/*
 * Copyright 2022 The Knative Authors
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

	"k8s.io/client-go/tools/cache"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	configmapinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/configmap"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/resolver"

	eventingclient "knative.dev/eventing/pkg/client/injection/client"
	brokerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/broker"
	triggerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/trigger"
	triggerreconciler "knative.dev/eventing/pkg/client/injection/reconciler/eventing/v1/trigger"

	consumergroupclient "knative.dev/eventing-kafka-broker/control-plane/pkg/client/internals/kafka/injection/client"
	consumergroupinformer "knative.dev/eventing-kafka-broker/control-plane/pkg/client/internals/kafka/injection/informers/eventing/v1alpha1/consumergroup"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/config"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/kafka"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/base"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/consumergroup"
)

const (
	NamespacedControllerAgentName = "kafka-namespaced-trigger-controller"

	NamespacedFinalizerName = "kafka.namespaced.triggers.eventing.knative.dev"
)

func NewNamespacedController(ctx context.Context, watcher configmap.Watcher, configs *config.Env) *controller.Impl {

	logger := logging.FromContext(ctx).Desugar()

	configmapInformer := configmapinformer.Get(ctx)
	consumerGroupInformer := consumergroupinformer.Get(ctx)
	brokerInformer := brokerinformer.Get(ctx)
	triggerInformer := triggerinformer.Get(ctx)
	triggerLister := triggerInformer.Lister()

	reconciler := &NamespacedReconciler{
		Reconciler: &base.Reconciler{
			KubeClient:                   kubeclient.Get(ctx),
			PodLister:                    podinformer.Get(ctx).Lister(),
			SecretLister:                 secretinformer.Get(ctx).Lister(),
			DataPlaneConfigMapNamespace:  configs.DataPlaneConfigMapNamespace,
			DataPlaneConfigConfigMapName: configs.DataPlaneConfigConfigMapName,
			ContractConfigMapName:        configs.ContractConfigMapName,
			ContractConfigMapFormat:      configs.ContractConfigMapFormat,
			DataPlaneNamespace:           configs.SystemNamespace,
			DispatcherLabel:              base.BrokerDispatcherLabel,
			ReceiverLabel:                base.BrokerReceiverLabel,
		},
		BrokerLister:        brokerInformer.Lister(),
		ConfigMapLister:     configmapInformer.Lister(),
		EventingClient:      eventingclient.Get(ctx),
		Env:                 configs,
		ConsumerGroupLister: consumergroupinformer.Get(ctx).Lister(),
		InternalsClient:     consumergroupclient.Get(ctx),
		SecretLister:        secretinformer.Get(ctx).Lister(),
		KubeClient:          kubeclient.Get(ctx),
	}

	impl := triggerreconciler.NewImpl(ctx, reconciler, func(impl *controller.Impl) controller.Options {
		return controller.Options{
			FinalizerName:     NamespacedFinalizerName,
			AgentName:         NamespacedControllerAgentName,
			SkipStatusUpdates: false,
			PromoteFilterFunc: filterTriggers(reconciler.BrokerLister, kafka.NamespacedBrokerClass, NamespacedFinalizerName),
		}
	})

	reconciler.Resolver = resolver.NewURIResolverFromTracker(ctx, impl.Tracker)

	triggerInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterTriggers(reconciler.BrokerLister, kafka.NamespacedBrokerClass, NamespacedFinalizerName),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	// Filter Brokers and enqueue associated Triggers
	brokerInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: kafka.NamespacedBrokerClassFilter(),
		Handler:    enqueueTriggers(logger, triggerLister, impl.Enqueue),
	})

	globalResync := func(_ interface{}) {
		impl.GlobalResync(brokerInformer.Informer())
	}

	configmapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: kafka.FilterWithLabel(kafka.NamespacedBrokerDataplaneLabelKey, kafka.NamespacedBrokerDataplaneLabelValue),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				globalResync(obj)
			},
			DeleteFunc: func(obj interface{}) {
				globalResync(obj)
			},
		},
	})

	// ConsumerGroup changes and enqueue associated Trigger
	consumerGroupInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: consumergroup.FilterByBrokerClass("trigger", triggerLister, reconciler.BrokerLister, kafka.NamespacedBrokerClass),
		//FilterFunc: kafka.FilterWithLabel(kafka.NamespacedBrokerDataplaneLabelKey, kafka.NamespacedBrokerDataplaneLabelValue),
		Handler: controller.HandleAll(consumergroup.Enqueue("trigger", impl.EnqueueKey)),
	})

	reconciler.SecretTracker = impl.Tracker
	secretinformer.Get(ctx).Informer().AddEventHandler(controller.HandleAll(reconciler.SecretTracker.OnChanged))

	return impl
}
