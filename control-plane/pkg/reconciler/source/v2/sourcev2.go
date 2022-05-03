/*
 * Copyright 2021 The Knative Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Shopify/sarama"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	sources "knative.dev/eventing-kafka/pkg/apis/sources/v1beta1"
	eventingduck "knative.dev/eventing/pkg/apis/duck/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	corelisters "k8s.io/client-go/listers/core/v1"

	internals "knative.dev/eventing-kafka-broker/control-plane/pkg/apis/internals/kafka/eventing"
	internalscg "knative.dev/eventing-kafka-broker/control-plane/pkg/apis/internals/kafka/eventing/v1alpha1"
	internalv1alpha1 "knative.dev/eventing-kafka-broker/control-plane/pkg/client/internals/kafka/clientset/versioned/typed/eventing/v1alpha1"
	internalslst "knative.dev/eventing-kafka-broker/control-plane/pkg/client/internals/kafka/listers/eventing/v1alpha1"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/kafka"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/security"
)

const (
	DefaultDeliveryOrder = internals.Ordered

	KafkaConditionConsumerGroup apis.ConditionType = "ConsumerGroup" //condition is registered by controller
)

var (
	conditionSet = apis.NewLivingConditionSet(
		KafkaConditionConsumerGroup,
		sources.KafkaConditionSinkProvided,
	)
)

type Reconciler struct {
	SecretLister corelisters.SecretLister

	SecretTracker       tracker.Interface
	ConsumerGroupLister internalslst.ConsumerGroupLister
	InternalsClient     internalv1alpha1.InternalV1alpha1Interface
	// NewKafkaClusterAdminClient creates new sarama ClusterAdmin. It's convenient to add this as Reconciler field so that we can
	// mock the function used during the reconciliation loop.
	NewKafkaClusterAdminClient kafka.NewClusterAdminClientFunc
}

func (r *Reconciler) ReconcileKind(ctx context.Context, ks *sources.KafkaSource) reconciler.Event {

	cg, err := r.reconcileConsumerGroup(ctx, ks)
	if err != nil {
		ks.GetConditionSet().Manage(&ks.Status).MarkFalse(KafkaConditionConsumerGroup, "failed to reconcile consumer group", err.Error())
		return err
	}

	propagateConsumerGroupStatus(cg, ks)

	return nil
}

func (r *Reconciler) FinalizeKind(ctx context.Context, ks *sources.KafkaSource) reconciler.Event {

	if err := r.finalizeConsumerGroup(ctx, ks); err != nil {
		return fmt.Errorf("failed to finalize consumer group: %w", err)
	}

	authContext, err := security.ResolveAuthContextFromNetSpec(r.SecretLister, ks.GetNamespace(), ks.Spec.Net)
	if err != nil {
		return fmt.Errorf("failed to create auth context: %w", err)
	}
	secret, err := security.Secret(ctx, &SecretLocator{KafkaSource: ks}, security.NetSpecSecretProviderFunc(authContext))
	if err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	// get security option for Sarama with secret info in it
	securityOption := security.NewSaramaSecurityOptionFromSecret(secret)

	if err := security.TrackNetSpecSecrets(r.SecretTracker, ks.Spec.Net, ks); err != nil {
		return fmt.Errorf("failed to track secrets: %w", err)
	}

	saramaConfig, err := kafka.GetSaramaConfig(securityOption)
	if err != nil {
		return fmt.Errorf("error getting cluster admin sarama config: %w", err)
	}

	kafkaClusterAdminClient, err := r.NewKafkaClusterAdminClient(ks.Spec.BootstrapServers, saramaConfig)
	if err != nil {
		logging.FromContext(ctx).Errorw("unable to create a kafka client", zap.Error(err))
		return err
	}
	defer kafkaClusterAdminClient.Close()

	if err := kafkaClusterAdminClient.DeleteConsumerGroup(ks.Spec.ConsumerGroup); err != nil && !errors.Is(sarama.ErrGroupIDNotFound, err) {
		logging.FromContext(ctx).Errorw("unable to delete the consumer group", zap.String("id", ks.Spec.ConsumerGroup), zap.Error(err))
		return err
	}

	logging.FromContext(ctx).Infow("consumer group deleted", zap.String("id", ks.Spec.ConsumerGroup))
	return nil
}

func (r Reconciler) finalizeConsumerGroup(ctx context.Context, ks *sources.KafkaSource) error {

	err := r.InternalsClient.ConsumerGroups(ks.GetNamespace()).Delete(ctx, string(ks.UID), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to remove consumer group %s/%s: %w", ks.GetNamespace(), string(ks.UID), err)
	}
	return nil
}

func (r Reconciler) reconcileConsumerGroup(ctx context.Context, ks *sources.KafkaSource) (*internalscg.ConsumerGroup, error) {

	backoffPolicy := eventingduck.BackoffPolicyExponential

	expectedCg := &internalscg.ConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(ks.UID),
			Namespace: ks.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(ks),
			},
			Labels: map[string]string{
				internalscg.UserFacingResourceLabelSelector: strings.ToLower(ks.GetGroupVersionKind().Kind),
			},
		},
		Spec: internalscg.ConsumerGroupSpec{
			Template: internalscg.ConsumerTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						internalscg.ConsumerLabelSelector: string(ks.UID),
					},
				},
				Spec: internalscg.ConsumerSpec{
					Topics: ks.Spec.Topics,
					Configs: internalscg.ConsumerConfigs{Configs: map[string]string{
						"group.id":          ks.Spec.ConsumerGroup,
						"bootstrap.servers": strings.Join(ks.Spec.BootstrapServers, ","),
					}},
					Auth: &internalscg.Auth{
						NetSpec: &ks.Spec.KafkaAuthSpec.Net,
					},
					Delivery: &internalscg.DeliverySpec{
						DeliverySpec: &eventingduck.DeliverySpec{
							Retry:         pointer.Int32(10),
							BackoffPolicy: &backoffPolicy,
							BackoffDelay:  pointer.String("PT10S"),
							Timeout:       pointer.String("PT600S"),
						},
						Ordering: DefaultDeliveryOrder,
					},
					Subscriber: ks.Spec.Sink,
					Reply:      &internalscg.ReplyStrategy{NoReply: &internalscg.NoReply{Enabled: true}},
				},
			},
			Replicas: ks.Spec.Consumers,
		},
	}

	if ks.Spec.CloudEventOverrides != nil {
		expectedCg.Spec.Template.Spec.CloudEventOverrides = &duckv1.CloudEventOverrides{
			Extensions: ks.Spec.CloudEventOverrides.Extensions,
		}
	}

	cg, err := r.ConsumerGroupLister.ConsumerGroups(ks.GetNamespace()).Get(string(ks.UID)) //Get by consumer group id
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if apierrors.IsNotFound(err) {
		cg, err := r.InternalsClient.ConsumerGroups(expectedCg.GetNamespace()).Create(ctx, expectedCg, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create consumer group %s/%s: %w", expectedCg.GetNamespace(), expectedCg.GetName(), err)
		}
		return cg, nil
	}

	if equality.Semantic.DeepDerivative(expectedCg.Spec, cg.Spec) {
		return cg, nil
	}

	newCg := &internalscg.ConsumerGroup{
		TypeMeta:   cg.TypeMeta,
		ObjectMeta: cg.ObjectMeta,
		Spec:       expectedCg.Spec,
		Status:     cg.Status,
	}
	if cg, err = r.InternalsClient.ConsumerGroups(cg.GetNamespace()).Update(ctx, newCg, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to update consumer group %s/%s: %w", newCg.GetNamespace(), newCg.GetName(), err)
	}

	return cg, nil
}

func propagateConsumerGroupStatus(cg *internalscg.ConsumerGroup, ks *sources.KafkaSource) {
	if cg.IsReady() {
		ks.GetConditionSet().Manage(&ks.Status).MarkTrue(KafkaConditionConsumerGroup)
	} else {
		topLevelCondition := cg.GetConditionSet().Manage(cg.GetStatus()).GetTopLevelCondition()
		if topLevelCondition == nil {
			ks.GetConditionSet().Manage(&ks.Status).MarkUnknown(
				KafkaConditionConsumerGroup,
				"failed to reconcile consumer group",
				"consumer group is not ready",
			)
		} else {
			ks.GetConditionSet().Manage(&ks.Status).MarkFalse(
				KafkaConditionConsumerGroup,
				topLevelCondition.Reason,
				topLevelCondition.Message,
			)
		}
	}
	ks.Status.MarkSink(cg.Status.SubscriberURI)
	ks.Status.Placeable = cg.Status.Placeable
	if cg.Status.Replicas != nil {
		ks.Status.Consumers = *cg.Status.Replicas
	}
}
