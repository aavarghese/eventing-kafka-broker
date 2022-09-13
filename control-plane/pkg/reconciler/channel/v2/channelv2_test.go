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
	"fmt"
	"testing"

	"github.com/Shopify/sarama"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	kubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/network"
	. "knative.dev/pkg/reconciler/testing"

	"knative.dev/eventing-kafka-broker/control-plane/pkg/apis/eventing/v1alpha1"
	internals "knative.dev/eventing-kafka-broker/control-plane/pkg/apis/internals/kafka/eventing"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/config"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/contract"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/kafka"
	kafkatesting "knative.dev/eventing-kafka-broker/control-plane/pkg/kafka/testing"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/prober"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/prober/probertesting"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/receiver"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/base"
	. "knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/testing"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/security"

	messagingv1beta "knative.dev/eventing-kafka/pkg/apis/messaging/v1beta1"
	fakeeventingkafkaclient "knative.dev/eventing-kafka/pkg/client/injection/client/fake"
	messagingv1beta1kafkachannelreconciler "knative.dev/eventing-kafka/pkg/client/injection/reconciler/messaging/v1beta1/kafkachannel"

	kedaclient "knative.dev/eventing-autoscaler-keda/third_party/pkg/client/injection/client/fake"
	internalscg "knative.dev/eventing-kafka-broker/control-plane/pkg/apis/internals/kafka/eventing/v1alpha1"
	fakeconsumergroupinformer "knative.dev/eventing-kafka-broker/control-plane/pkg/client/internals/kafka/injection/client/fake"
)

const (
	testProber = "testProber"

	finalizerName                 = "kafkachannels.messaging.knative.dev"
	TestExpectedDataNumPartitions = "TestExpectedDataNumPartitions"
	TestExpectedReplicationFactor = "TestExpectedReplicationFactor"
)

var finalizerUpdatedEvent = Eventf(
	corev1.EventTypeNormal,
	"FinalizerUpdate",
	fmt.Sprintf(`Updated %q finalizers`, ChannelName),
)

var DefaultEnv = &config.Env{
	DataPlaneConfigMapNamespace: "knative-eventing",
	ContractConfigMapName:       "kafka-channel-channels-subscriptions",
	GeneralConfigMapName:        "kafka-channel-config",
	IngressName:                 "kafka-channel-ingress",
	SystemNamespace:             "knative-eventing",
	ContractConfigMapFormat:     base.Json,
}

func TestReconcileKind(t *testing.T) {

	messagingv1beta.RegisterAlternateKafkaChannelConditionSet(conditionSet)

	env := *DefaultEnv
	testKey := fmt.Sprintf("%s/%s", ChannelNamespace, ChannelName)

	table := TableTest{
		{
			Name: "Reconciled normal - with single fresh subscriber - with auth - SASL",
			Objects: []runtime.Object{
				NewChannel(WithSubscribers(Subscriber1(WithFreshSubscriber))),
				NewConfigMapWithTextData(env.SystemNamespace, DefaultEnv.GeneralConfigMapName, map[string]string{
					kafka.BootstrapServersConfigMapKey: ChannelBootstrapServers,
					security.AuthSecretNameKey:         "secret-1",
					security.AuthSecretNamespaceKey:    "ns-1",
				}),
				NewConfigMapWithBinaryData(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, nil),
				NewLegacySASLSecret("ns-1", "secret-1"),
				NewConsumerGroup(
					WithConsumerGroupName(Subscription1UUID),
					WithConsumerGroupNamespace(ChannelNamespace),
					WithConsumerGroupOwnerRef(kmeta.NewControllerRef(NewChannel())),
					WithConsumerGroupMetaLabels(OwnerAsChannelLabel),
					WithConsumerGroupLabels(ConsumerSubscription1Label),
					ConsumerGroupConsumerSpec(NewConsumerSpec(
						ConsumerTopics(ChannelTopic()),
						ConsumerConfigs(
							ConsumerGroupIdConfig(consumerGroup(NewChannel(), GetSubscriberSpec(Subscriber1(WithFreshSubscriber)))),
							ConsumerBootstrapServersConfig(ChannelBootstrapServers),
						),
						ConsumerDelivery(NewConsumerSpecDelivery(internals.Ordered)),
						ConsumerSubscriber(NewConsumerSpecSubscriber(Subscription1URI)),
						ConsumerAuth(&internalscg.Auth{
							AuthSpec: &v1alpha1.Auth{
								Secret: &v1alpha1.Secret{
									Ref: &v1alpha1.SecretReference{
										Name: "secret-1",
									},
								},
							},
						}),
					)),
					ConsumerGroupReplicas(1),
					ConsumerGroupReady,
				),
			},
			Key: testKey,
			WantCreates: []runtime.Object{
				NewPerChannelService(&env),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{
				ConfigMapUpdate(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, env.ContractConfigMapFormat, &contract.Contract{
					Generation: 1,
					Resources: []*contract.Resource{
						{
							Uid:              ChannelUUID,
							Topics:           []string{ChannelTopic()},
							BootstrapServers: ChannelBootstrapServers,
							Reference:        ChannelReference(),
							Auth: &contract.Resource_MultiAuthSecret{
								MultiAuthSecret: &contract.MultiSecretReference{
									Protocol: contract.Protocol_SASL_PLAINTEXT,
									References: []*contract.SecretReference{{
										Reference: &contract.Reference{
											Uuid:      SecretUUID,
											Namespace: "ns-1",
											Name:      "secret-1",
											Version:   SecretResourceVersion,
										},
										KeyFieldReferences: []*contract.KeyFieldReference{
											{SecretKey: "password", Field: contract.SecretField_PASSWORD},
											{SecretKey: "username", Field: contract.SecretField_USER},
											{SecretKey: "saslType", Field: contract.SecretField_SASL_MECHANISM},
										},
									}},
								},
							},
							Ingress: &contract.Ingress{
								Host: receiver.Host(ChannelNamespace, ChannelName),
							},
						},
					},
				}),
			},
			SkipNamespaceValidation: true, // WantCreates compare the channel namespace with configmap namespace, so skip it
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{
				{
					Object: NewChannel(
						WithInitKafkaChannelConditions,
						StatusConfigParsed,
						StatusConfigMapUpdatedReady(&env),
						StatusTopicReadyWithName(ChannelTopic()),
						ChannelAddressable(&env),
						WithSubscribers(Subscriber1(WithFreshSubscriber)),
						StatusProbeSucceeded,
						StatusChannelSubscribers(),
					),
				},
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchFinalizers(),
			},
			WantEvents: []string{
				finalizerUpdatedEvent,
			},
		},
		{
			Name: "Reconciled normal - with single fresh subscriber - with autoscaling annotations",
			Objects: []runtime.Object{
				NewChannel(
					WithSubscribers(Subscriber1(WithFreshSubscriber)),
					WithAutoscalingAnnotationsChannel(),
				),
				NewConfigMapWithTextData(env.SystemNamespace, DefaultEnv.GeneralConfigMapName, map[string]string{
					kafka.BootstrapServersConfigMapKey: ChannelBootstrapServers,
				}),
				NewConfigMapWithBinaryData(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, nil),
			},
			Key: testKey,
			WantCreates: []runtime.Object{
				NewPerChannelService(&env),
				NewConsumerGroup(
					WithConsumerGroupName(Subscription1UUID),
					WithConsumerGroupNamespace(ChannelNamespace),
					WithConsumerGroupOwnerRef(kmeta.NewControllerRef(NewChannel())),
					WithConsumerGroupMetaLabels(OwnerAsChannelLabel),
					WithConsumerGroupLabels(ConsumerSubscription1Label),
					WithConsumerGroupAnnotations(ConsumerGroupAnnotations),
					ConsumerGroupConsumerSpec(NewConsumerSpec(
						ConsumerTopics(ChannelTopic()),
						ConsumerConfigs(
							ConsumerGroupIdConfig(consumerGroup(NewChannel(), GetSubscriberSpec(Subscriber1(WithFreshSubscriber)))),
							ConsumerBootstrapServersConfig(ChannelBootstrapServers),
						),
						ConsumerDelivery(NewConsumerSpecDelivery(internals.Ordered)),
						ConsumerSubscriber(NewConsumerSpecSubscriber(Subscription1URI)),
					)),
					ConsumerGroupReplicas(1),
				),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{
				ConfigMapUpdate(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, env.ContractConfigMapFormat, &contract.Contract{
					Generation: 1,
					Resources: []*contract.Resource{
						{
							Uid:              ChannelUUID,
							Topics:           []string{ChannelTopic()},
							BootstrapServers: ChannelBootstrapServers,
							Reference:        ChannelReference(),
							Ingress: &contract.Ingress{
								Host: receiver.Host(ChannelNamespace, ChannelName),
							},
						},
					},
				}),
			},
			SkipNamespaceValidation: true, // WantCreates compare the channel namespace with configmap namespace, so skip it
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{
				{
					Object: NewChannel(
						WithInitKafkaChannelConditions,
						StatusConfigParsed,
						StatusConfigMapUpdatedReady(&env),
						StatusTopicReadyWithName(ChannelTopic()),
						ChannelAddressable(&env),
						StatusProbeSucceeded,
						WithSubscribers(Subscriber1(WithUnknownSubscriber)),
						StatusChannelSubscribersUnknown(),
						WithAutoscalingAnnotationsChannel(),
					),
				},
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchFinalizers(),
			},
			WantEvents: []string{
				finalizerUpdatedEvent,
			},
		},
		{
			Name: "Create contract configmap when it does not exist",
			Objects: []runtime.Object{
				NewChannel(),
				NewConfigMapWithTextData(env.SystemNamespace, DefaultEnv.GeneralConfigMapName, map[string]string{
					kafka.BootstrapServersConfigMapKey: ChannelBootstrapServers,
				}),
			},
			Key: testKey,
			WantUpdates: []clientgotesting.UpdateActionImpl{
				ConfigMapUpdate(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, env.ContractConfigMapFormat, &contract.Contract{
					Generation: 1,
					Resources: []*contract.Resource{
						{
							Uid:              ChannelUUID,
							Topics:           []string{ChannelTopic()},
							BootstrapServers: ChannelBootstrapServers,
							Reference:        ChannelReference(),
							Ingress: &contract.Ingress{
								Host: receiver.Host(ChannelNamespace, ChannelName),
							},
						},
					},
				}),
			},
			SkipNamespaceValidation: true, // WantCreates compare the channel namespace with configmap namespace, so skip it
			WantCreates: []runtime.Object{
				NewConfigMapWithBinaryData(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, nil),
				NewPerChannelService(&env),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{
				{
					Object: NewChannel(
						WithInitKafkaChannelConditions,
						StatusConfigParsed,
						StatusConfigMapUpdatedReady(&env),
						StatusTopicReadyWithName(ChannelTopic()),
						ChannelAddressable(&env),
						StatusProbeSucceeded,
						StatusChannelSubscribers(),
					),
				},
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchFinalizers(),
			},
			WantEvents: []string{
				finalizerUpdatedEvent,
			},
		},
		{
			Name: "Do not create contract configmap when it exists",
			Objects: []runtime.Object{
				NewChannel(),
				NewConfigMapWithTextData(env.SystemNamespace, DefaultEnv.GeneralConfigMapName, map[string]string{
					kafka.BootstrapServersConfigMapKey: ChannelBootstrapServers,
				}),
				NewConfigMapWithBinaryData(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, nil),
			},
			Key: testKey,
			WantCreates: []runtime.Object{
				NewPerChannelService(&env),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{
				ConfigMapUpdate(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, env.ContractConfigMapFormat, &contract.Contract{
					Generation: 1,
					Resources: []*contract.Resource{
						{
							Uid:              ChannelUUID,
							Topics:           []string{ChannelTopic()},
							BootstrapServers: ChannelBootstrapServers,
							Reference:        ChannelReference(),
							Ingress: &contract.Ingress{
								Host: receiver.Host(ChannelNamespace, ChannelName),
							},
						},
					},
				}),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{
				{
					Object: NewChannel(
						WithInitKafkaChannelConditions,
						StatusConfigParsed,
						StatusConfigMapUpdatedReady(&env),
						StatusTopicReadyWithName(ChannelTopic()),
						ChannelAddressable(&env),
						StatusProbeSucceeded,
						StatusChannelSubscribers(),
					),
				},
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchFinalizers(),
			},
			WantEvents: []string{
				finalizerUpdatedEvent,
			},
		},
		{
			Name: "Corrupt contract in configmap",
			Objects: []runtime.Object{
				NewChannel(),
				NewConfigMapWithTextData(env.SystemNamespace, DefaultEnv.GeneralConfigMapName, map[string]string{
					kafka.BootstrapServersConfigMapKey: ChannelBootstrapServers,
				}),
				NewConfigMapWithBinaryData(env.DataPlaneConfigMapNamespace, env.ContractConfigMapName, []byte("corrupt")),
			},
			Key:                     testKey,
			WantErr:                 true,
			SkipNamespaceValidation: true, // WantCreates compare the channel namespace with configmap namespace, so skip it
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{
				{
					Object: NewChannel(
						WithInitKafkaChannelConditions,
						StatusConfigParsed,
						StatusTopicReadyWithName(ChannelTopic()),
						StatusConfigMapNotUpdatedReady(
							"Failed to get contract data from ConfigMap: knative-eventing/kafka-channel-channels-subscriptions",
							"failed to unmarshal contract: 'corrupt'",
						),
					),
				},
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchFinalizers(),
			},
			WantEvents: []string{
				finalizerUpdatedEvent,
				Eventf(
					corev1.EventTypeWarning,
					"InternalError",
					"failed to get broker and triggers data from config map knative-eventing/kafka-channel-channels-subscriptions: failed to unmarshal contract: 'corrupt'",
				),
			},
		},
	}

	table.Test(t, NewFactory(&env, func(ctx context.Context, listers *Listers, env *config.Env, row *TableRow) controller.Reconciler {
		ctx, _ = kedaclient.With(ctx)

		proberMock := probertesting.MockProber(prober.StatusReady)
		if p, ok := row.OtherTestData[testProber]; ok {
			proberMock = p.(prober.Prober)
		}

		numPartitions := int32(1)
		if v, ok := row.OtherTestData[TestExpectedDataNumPartitions]; ok {
			numPartitions = v.(int32)
		}

		replicationFactor := int16(1)
		if v, ok := row.OtherTestData[TestExpectedReplicationFactor]; ok {
			replicationFactor = v.(int16)
		}

		reconciler := &Reconciler{
			Reconciler: &base.Reconciler{
				KubeClient:                  kubeclient.Get(ctx),
				PodLister:                   listers.GetPodLister(),
				SecretLister:                listers.GetSecretLister(),
				DataPlaneConfigMapNamespace: env.DataPlaneConfigMapNamespace,
				ContractConfigMapName:       env.ContractConfigMapName,
				ContractConfigMapFormat:     env.ContractConfigMapFormat,
				DataPlaneNamespace:          env.SystemNamespace,
				DispatcherLabel:             base.ChannelDispatcherLabel,
				ReceiverLabel:               base.ChannelReceiverLabel,
			},
			Env: env,
			NewKafkaClient: func(addrs []string, config *sarama.Config) (sarama.Client, error) {
				return &kafkatesting.MockKafkaClient{}, nil
			},
			NewKafkaClusterAdminClient: func(_ []string, _ *sarama.Config) (sarama.ClusterAdmin, error) {
				return &kafkatesting.MockKafkaClusterAdmin{
					ExpectedTopicName: ChannelTopic(),
					ExpectedTopicDetail: sarama.TopicDetail{
						NumPartitions:     numPartitions,
						ReplicationFactor: replicationFactor,
					},
					T: t,
				}, nil
			},
			ConfigMapLister:     listers.GetConfigMapLister(),
			ServiceLister:       listers.GetServiceLister(),
			ConsumerGroupLister: listers.GetConsumerGroupLister(),
			InternalsClient:     fakeconsumergroupinformer.Get(ctx),
			Prober:              proberMock,
			IngressHost:         network.GetServiceHostname(env.IngressName, env.SystemNamespace),
			KedaClient:          kedaclient.Get(ctx),
		}
		reconciler.ConfigMapTracker = &FakeTracker{}
		reconciler.SecretTracker = &FakeTracker{}

		r := messagingv1beta1kafkachannelreconciler.NewReconciler(
			ctx,
			logging.FromContext(ctx),
			fakeeventingkafkaclient.Get(ctx),
			listers.GetKafkaChannelLister(),
			controller.GetEventRecorder(ctx),
			reconciler,
		)
		return r
	}))
}

func StatusChannelSubscribers() KRShapedOption {
	return func(obj duckv1.KRShaped) {
		ch := obj.(*messagingv1beta.KafkaChannel)
		ch.GetConditionSet().Manage(ch.GetStatus()).MarkTrue(KafkaChannelConditionSubscribersReady)
	}
}

func StatusChannelSubscribersUnknown() KRShapedOption {
	return func(obj duckv1.KRShaped) {
		ch := obj.(*messagingv1beta.KafkaChannel)
		ch.GetConditionSet().Manage(ch.GetStatus()).MarkUnknown(KafkaChannelConditionSubscribersReady, "all subscribers not ready", "failed to reconcile consumer group")
	}
}

func StatusChannelSubscribersFailed(reason string, msg string) KRShapedOption {
	return func(obj duckv1.KRShaped) {
		ch := obj.(*messagingv1beta.KafkaChannel)
		ch.GetConditionSet().Manage(ch.GetStatus()).MarkFalse(KafkaChannelConditionSubscribersReady, reason, msg)
	}
}

func patchFinalizers() clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = ChannelName
	action.Namespace = ChannelNamespace
	patch := `{"metadata":{"finalizers":["` + finalizerName + `"],"resourceVersion":""}}`
	action.Patch = []byte(patch)
	return action
}
