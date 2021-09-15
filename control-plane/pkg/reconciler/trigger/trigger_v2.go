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
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/eventing-kafka/pkg/common/scheduler"
	eventing "knative.dev/eventing/pkg/apis/eventing/v1"
	eventingclientset "knative.dev/eventing/pkg/client/clientset/versioned"
	eventinglisters "knative.dev/eventing/pkg/client/listers/eventing/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"

	kafkalogging "knative.dev/eventing-kafka-broker/control-plane/pkg/logging"
)

type ReconcilerV2 struct {
	Scheduler      scheduler.Scheduler
	BrokerLister   eventinglisters.BrokerLister
	EventingClient eventingclientset.Interface
	Resolver       *resolver.URIResolver
}

func (r *ReconcilerV2) ReconcileKind(ctx context.Context, trigger *eventing.Trigger) reconciler.Event {
	logger := kafkalogging.CreateReconcileMethodLogger(ctx, trigger)

	broker, err := r.BrokerLister.Brokers(trigger.Namespace).Get(trigger.Spec.Broker)
	if err != nil && !apierrors.IsNotFound(err) {
		trigger.Status.MarkBrokerFailed("Failed to get broker", "%v", err)
		return fmt.Errorf("failed to get broker: %w", err)
	}

	if apierrors.IsNotFound(err) {
		// Actually check if the broker doesn't exist.
		// Note: do not introduce another `broker` variable with `:`
		broker, err = r.EventingClient.EventingV1().Brokers(trigger.Namespace).Get(ctx, trigger.Spec.Broker, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			logger.Debug("broker not found", zap.String("finalizeDuringReconcile", "notFound"))
			// The associated broker doesn't exist anymore, so clean up Trigger resources.
			return r.FinalizeKind(ctx, trigger)
		}
	}

	// Ignore Triggers that are associated with a Broker we don't own.
	if isOur, brokerClass := isOurBroker(broker); !isOur {
		logger.Debug("Ignoring Trigger", zap.String(eventing.BrokerClassAnnotationKey, brokerClass))
		return nil
	}

	if !broker.GetDeletionTimestamp().IsZero() {
		logger.Debug("broker deleted", zap.String("finalizeDuringReconcile", "deleted"))
		// The associated broker doesn't exist anymore, so clean up Trigger resources.
		return r.FinalizeKind(ctx, trigger)
	}
	trigger.Status.PropagateBrokerCondition(broker.Status.GetTopLevelCondition())

	if !broker.IsReady() {
		return nil // Trigger will get re-queued once this broker is ready.
	}

	// TODO set consumer group offsets and set condition accordingly, so that we don't need to care about being
	//  ready too early (use eventing-kafka util)
	trigger.Status.PropagateSubscriptionCondition(&apis.Condition{Status: corev1.ConditionTrue})

	if err := r.schedule(trigger); err != nil {
		return err
	}
	if err := r.reconcileSubscriber(ctx, trigger); err != nil {
		return err
	}
	if err := r.reconcileDeadLetterSink(ctx, trigger); err != nil {
		return err
	}
	trigger.Status.MarkDependencySucceeded()

	return nil
}

func (r *ReconcilerV2) schedule(trigger *eventing.Trigger) error {
	vPod := &vPod{trigger: trigger, vReplicas: 1}
	placements, err := r.Scheduler.Schedule(vPod)
	if err != nil {
		return fmt.Errorf("failed to schedule trigger: %w", err)
	}
	vPod.SetPlacements(placements)
	return nil
}

func (r *ReconcilerV2) reconcileSubscriber(ctx context.Context, trigger *eventing.Trigger) error {
	uri, err := r.Resolver.URIFromDestinationV1(ctx, trigger.Spec.Subscriber, trigger)
	if err != nil {
		trigger.Status.MarkSubscriberResolvedFailed("Failed to resolve spec.subscriber", err.Error())
		return fmt.Errorf("failed to resolve spec.subscriber: %w", err)
	}

	trigger.Status.SubscriberURI = uri
	trigger.Status.MarkSubscriberResolvedSucceeded()
	return nil
}

func (r *ReconcilerV2) reconcileDeadLetterSink(ctx context.Context, trigger *eventing.Trigger) error {
	if trigger.Spec.Delivery == nil || trigger.Spec.Delivery.DeadLetterSink == nil {
		trigger.Status.MarkDeadLetterSinkNotConfigured()
		return nil
	}

	uri, err := r.Resolver.URIFromDestinationV1(ctx, *trigger.Spec.Delivery.DeadLetterSink, trigger)
	if err != nil {
		trigger.Status.MarkDeadLetterSinkResolvedFailed("Failed to resolve spec.delivery.deadLetterSink", err.Error())
		return fmt.Errorf("failed to resolve spec.delivery.deadLetterSink: %w", err)
	}

	trigger.Status.DeadLetterSinkURI = uri
	trigger.Status.MarkDeadLetterSinkResolvedSucceeded()
	return nil
}

func (r *ReconcilerV2) FinalizeKind(_ context.Context, trigger *eventing.Trigger) reconciler.Event {
	vPod := &vPod{trigger: trigger, vReplicas: 0}
	vPod.SetPlacements(nil)
	return nil
}
