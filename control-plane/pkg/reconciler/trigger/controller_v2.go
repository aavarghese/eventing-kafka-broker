/*
 * Copyright 2020 The Knative Authors
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

	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"

	eventingclient "knative.dev/eventing/pkg/client/injection/client"
	brokerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/broker"
	triggerinformer "knative.dev/eventing/pkg/client/injection/informers/eventing/v1/trigger"
	triggerreconciler "knative.dev/eventing/pkg/client/injection/reconciler/eventing/v1/trigger"

	"knative.dev/eventing-kafka-broker/control-plane/pkg/config"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/base"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/kafka"
)

func NewControllerV2(ctx context.Context, _ configmap.Watcher, configs *config.Env) *controller.Impl {

	logger := logging.FromContext(ctx).Desugar()

	brokerInformer := brokerinformer.Get(ctx)
	triggerInformer := triggerinformer.Get(ctx)
	triggerLister := triggerInformer.Lister()

	r := &ReconcilerV2{
		BrokerLister:   brokerInformer.Lister(),
		EventingClient: eventingclient.Get(ctx),
	}

	impl := triggerreconciler.NewImpl(ctx, r, func(impl *controller.Impl) controller.Options {
		return controller.Options{
			FinalizerName:     FinalizerName,
			AgentName:         ControllerAgentName,
			SkipStatusUpdates: false,
		}
	})

	r.Resolver = resolver.NewURIResolverFromTracker(ctx, impl.Tracker)

	triggerInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterTriggers(r.BrokerLister),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	// Filter Brokers and enqueue associated Triggers
	brokerInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: kafka.BrokerClassFilter(),
		Handler:    enqueueTriggers(logger, triggerLister, impl.Enqueue),
	})

	globalResync := func(_ interface{}) {
		impl.GlobalResync(triggerInformer.Informer())
	}

	podFilterFunc := reconciler.ChainFilterFuncs(
		reconciler.NamespaceFilterFunc(configs.SystemNamespace),
		reconciler.LabelFilterFunc("app", base.BrokerDispatcherLabel, false),
	)

	podinformer := coreinformers.NewPodInformer(
		kubeclient.Get(ctx),
		configs.SystemNamespace,
		controller.DefaultResyncPeriod,
		nil,
	)

	podinformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: podFilterFunc,
		Handler:    controller.HandleAll(globalResync),
	})

	return impl
}
