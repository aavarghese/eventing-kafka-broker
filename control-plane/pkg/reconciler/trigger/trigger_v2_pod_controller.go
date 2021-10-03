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

package trigger

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	eventingduck "knative.dev/eventing/pkg/apis/duck/v1"
	eventing "knative.dev/eventing/pkg/apis/eventing/v1"
	brokerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/broker"
	triggerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/trigger"
	eventinglisters "knative.dev/eventing/pkg/client/listers/eventing/v1"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"

	podreconciler "knative.dev/eventing-kafka-broker/control-plane/pkg/client/injection/kube/reconciler/core/v1/pod"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/contract"
	coreconfig "knative.dev/eventing-kafka-broker/control-plane/pkg/core/config"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/pod"
)

func NewPodController(ctx context.Context) *controller.Impl {

	systemNamespace := system.Namespace()

	triggerInformer := triggerinformer.Get(ctx)

	pr := &pod.Reconciler{
		ScheduledResourceLister: &triggerScheduledResourceLister{triggerLister: triggerInformer.Lister()},
		ToContract:              contractTransformer(brokerinformer.Get(ctx).Lister()),
		Kube:                    kubeclient.Get(ctx).CoreV1(),
	}

	impl := podreconciler.NewImpl(ctx, pr)

	triggerInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { onTriggerChange(systemNamespace, impl.EnqueueKey)(obj.(*eventing.Trigger)) },
		UpdateFunc: func(oldObj, newObj interface{}) {
			// Handle both versions since the previous and the new version could have different pods assigned.
			onTriggerChange(systemNamespace, impl.EnqueueKey)(oldObj.(*eventing.Trigger))
			onTriggerChange(systemNamespace, impl.EnqueueKey)(newObj.(*eventing.Trigger))
		},
		DeleteFunc: func(obj interface{}) { onTriggerChange(systemNamespace, impl.EnqueueKey)(obj.(*eventing.Trigger)) },
	})

	podInformer := newPodInformer(ctx)
	podInformer.AddEventHandler(controller.HandleAll(impl.Enqueue))

	return impl
}

func onTriggerChange(systemNamespace string, enqueue func(key types.NamespacedName)) func(obj *eventing.Trigger) {
	return func(t *eventing.Trigger) {
		for k, v := range t.Status.Annotations {
			if strings.HasSuffix(k, podNameField) {
				enqueue(types.NamespacedName{Namespace: systemNamespace, Name: v})
			}
		}
	}
}

func contractTransformer(bl eventinglisters.BrokerLister) pod.ToContract {
	return func(ctx context.Context, schedulables []pod.ScheduledResource) (proto.Message, error) {
		ct := &contract.Contract{}
		for _, sc := range schedulables {
			vPod := sc.(*vPod)
			broker, err := bl.Brokers(vPod.GetNamespace()).Get(vPod.Spec.Broker)
			if err != nil {
				// We don't want to fail here for a single badly configured resource since this might affect other
				// scheduled resources.
				logging.FromContext(ctx).Desugar().Warn("Failed to get broker resource",
					zap.String("namespace", vPod.GetNamespace()),
					zap.String("name", vPod.GetName()),
					zap.String("broker", vPod.Spec.Broker),
				)
				continue
			}
			r, err := resourceFor(vPod, broker)
			if err != nil {
				// We don't want to fail here for a single badly configured resource since this might affect other
				// scheduled resources.
				logging.FromContext(ctx).Desugar().Warn("Failed to create resource",
					zap.String("namespace", vPod.GetNamespace()),
					zap.String("name", vPod.GetName()),
				)
				continue
			}
			ct.Resources = append(ct.Resources, r)
		}
		return ct, nil
	}
}

func resourceFor(vPod *vPod, broker *eventing.Broker) (*contract.Resource, error) {
	egress, err := egressFor(vPod, broker)
	if err != nil {
		return nil, err
	}
	r := &contract.Resource{
		Uid:              string(vPod.GetUID()),
		Topics:           vPod.GetTopics(),
		BootstrapServers: vPod.GetBootstrapServers(),
		Egresses:         []*contract.Egress{egress},
		Auth:             nil,
	}
	return r, nil
}

func egressFor(vPod *vPod, broker *eventing.Broker) (*contract.Egress, error) {
	dOrder, err := deliveryOrder(vPod.Trigger)
	if err != nil {
		return nil, err
	}
	tEgressConfig, err := egressConfigFor(vPod.Spec.Delivery, vPod.Status.DeadLetterSinkURI.String())
	if err != nil {
		return nil, err
	}
	bEgressConfig, err := egressConfigFor(broker.Spec.Delivery, broker.Status.DeadLetterSinkURI.String())
	if err != nil {
		return nil, err
	}
	egressConfig := coreconfig.MergeEgressConfig(tEgressConfig, bEgressConfig)
	egress := &contract.Egress{
		ConsumerGroup: string(vPod.GetUID()),
		Destination:   vPod.Status.SubscriberURI.String(),
		Filter:        &contract.Filter{Attributes: vPod.Spec.Filter.Attributes},
		Uid:           string(vPod.GetUID()),
		EgressConfig:  egressConfig,
		DeliveryOrder: dOrder,
	}
	return egress, nil
}

func egressConfigFor(delivery *eventingduck.DeliverySpec, deadLetter string) (*contract.EgressConfig, error) {
	if delivery == nil {
		return nil, nil
	}
	// TODO hardcoded default duration
	backoffDelay, err := coreconfig.DurationMillisFromISO8601String(delivery.BackoffDelay, 5000)
	if err != nil {
		return nil, fmt.Errorf("failed to get spec.delivery.backoffDelay: %w", err)
	}
	// TODO hardcoded default timeout
	timeout, err := coreconfig.DurationMillisFromISO8601String(delivery.Timeout, 300)
	if err != nil {
		return nil, fmt.Errorf("failed to get spec.delivery.timeout: %w", err)
	}
	retry := uint32(0)
	if delivery.Retry != nil {
		retry = uint32(*delivery.Retry)
	}
	ec := &contract.EgressConfig{
		DeadLetter:    deadLetter,
		Retry:         retry,
		BackoffPolicy: coreconfig.BackoffPolicyFromString(delivery.BackoffPolicy),
		BackoffDelay:  backoffDelay,
		Timeout:       timeout,
	}
	return ec, nil
}

type triggerScheduledResourceLister struct {
	triggerLister eventinglisters.TriggerLister
	brokerLister  eventinglisters.BrokerLister
}

func (tsrl triggerScheduledResourceLister) ListAll() ([]pod.ScheduledResource, error) {
	triggers, err := tsrl.triggerLister.List(labels.NewSelector())
	if err != nil {
		return nil, err
	}
	vPods := make([]pod.ScheduledResource, 0, len(triggers))
	for _, t := range triggers {
		// Take only triggers that are ready or the scheduling is in progress.
		depCond := t.Status.GetCondition(eventing.TriggerConditionDependency)
		if t.Status.IsReady() || (depCond.IsUnknown() && depCond.Reason == SchedulingProgressReason) {
			b, err := tsrl.brokerLister.Brokers(t.GetNamespace()).Get(t.Spec.Broker)
			if err != nil {
				// TODO do not fail here
				return nil, err
			}
			vPod := newVPod(t, b)
			vPods = append(vPods, vPod)
		}
	}
	return vPods, nil
}
