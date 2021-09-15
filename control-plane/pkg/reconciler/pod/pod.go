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

package pod

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	kafkaduck "knative.dev/eventing-kafka/pkg/apis/duck/v1alpha1"
	"knative.dev/pkg/reconciler"

	"knative.dev/eventing-kafka-broker/control-plane/pkg/configmap"
)

// ScheduledResource is a resource that has been scheduled in multiple pods.
type ScheduledResource interface {
	// GetPlacements returns the current list of placements.
	// Do not mutate!
	GetPlacements() []kafkaduck.Placement
}

// ScheduledResourceLister list schedulables.
type ScheduledResourceLister interface {
	// ListAll lists all schedulables.
	ListAll() ([]ScheduledResource, error)
}

// ToContract transform scheduled resources to our internal contract.
type ToContract func(schedulables []ScheduledResource) (proto.Message, error)

type Reconciler struct {
	ScheduledResourceLister ScheduledResourceLister
	ToContract              ToContract

	ConfigMapLister corelisters.ConfigMapNamespaceLister
	Kube            kubernetes.Interface
}

func (r *Reconciler) ReconcileKind(ctx context.Context, pod *corev1.Pod) reconciler.Event {
	schedulables, err := r.getScheduledResourcesIn(pod)
	if err != nil {
		return fmt.Errorf("failed to get scheduled resources: %w", err)
	}
	contract, err := r.ToContract(schedulables)
	if err != nil {
		return fmt.Errorf("failed to transform scheduled resources to contract: %w", err)
	}
	if err := r.saveContract(ctx, contract, pod); err != nil {
		return fmt.Errorf("failed to save contract: %w", err)
	}
	return nil
}

func (r *Reconciler) getScheduledResourcesIn(pod *corev1.Pod) ([]ScheduledResource, error) {
	schedulables, err := r.ScheduledResourceLister.ListAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}
	return filterSchedulablesOf(schedulables, pod), nil
}

func (r *Reconciler) saveContract(ctx context.Context, contract proto.Message, pod *corev1.Pod) error {
	cmName, err := configMapNameOf(pod)
	if err != nil {
		return err
	}
	cm, err := configmap.GetOrCreate(ctx, r.Kube, pod.GetNamespace(), cmName)
	if err != nil {
		return fmt.Errorf("failed to get or create ConfigMap %s/%s: %w", pod.GetNamespace(), cmName, err)
	}
	if err := configmap.Update(ctx, r.Kube, cm, contract); err != nil {
		return fmt.Errorf("failed to update ConfigMap %s/%s: %w", cm.GetNamespace(), cm.GetName(), err)
	}
	return nil
}

func filterSchedulablesOf(schedulables []ScheduledResource, pod *corev1.Pod) []ScheduledResource {
	currentPodSchedulables := make([]ScheduledResource, 0, len(schedulables))
	for _, schedulable := range schedulables {
		for _, placement := range schedulable.GetPlacements() {
			if placement.PodName == pod.Name {
				currentPodSchedulables = append(currentPodSchedulables, schedulable)
				break // There cannot be placements with the same pod name. TODO check invariance
			}
		}
	}
	return currentPodSchedulables
}

func configMapNameOf(pod *corev1.Pod) (string, error) {
	const volumeName = "kafka-broker-brokers-triggers"
	for _, v := range pod.Spec.Volumes {
		if v.Name == volumeName {
			return v.ConfigMap.Name, nil
		}
	}
	return "", fmt.Errorf("failed to find volume %s in pod %s/%s", volumeName, pod.GetNamespace(), pod.GetName())
}
