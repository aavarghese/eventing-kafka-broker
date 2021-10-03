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
	"strings"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubecorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	kafkaduck "knative.dev/eventing-kafka/pkg/apis/duck/v1alpha1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/reconciler"

	"knative.dev/eventing-kafka-broker/control-plane/pkg/configmap"
	"knative.dev/eventing-kafka-broker/control-plane/pkg/podplacement"
)

// ScheduledResource is a resource that has been scheduled in multiple pods.
type ScheduledResource interface {
	// KRShaped is implemeted by any Knative resource.
	duckv1.KRShaped
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
type ToContract func(ctx context.Context, schedulables []ScheduledResource) (proto.Message, error)

type Reconciler struct {
	ScheduledResourceLister ScheduledResourceLister
	ToContract              ToContract

	Kube kubecorev1.CoreV1Interface
}

func (r *Reconciler) ReconcileKind(ctx context.Context, pod *corev1.Pod) reconciler.Event {
	schedulables, err := r.getScheduledResourcesIn(pod)
	if err != nil {
		return fmt.Errorf("failed to get scheduled resources: %w", err)
	}
	if err := r.schedule(ctx, pod, schedulables); err != nil {
		return fmt.Errorf("failed to schedule scheduled resources: %w", err)
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

func (r *Reconciler) schedule(ctx context.Context, pod *corev1.Pod, schedulables []ScheduledResource) error {
	contract, err := r.ToContract(ctx, schedulables)
	if err != nil {
		return fmt.Errorf("failed to transform scheduled resources to contract: %w", err)
	}
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
	return r.updatePodAnnotations(ctx, pod, schedulables)
}

func (r *Reconciler) updatePodAnnotations(ctx context.Context, pod *corev1.Pod, schedulables []ScheduledResource) error {
	annotations := make(map[string]string, len(pod.GetAnnotations())+len(schedulables))
	preserveNonSchedulerAnnotations(pod, annotations)
	addSchedulablesAnnotations(schedulables, annotations)
	return r.saveAnnotations(ctx, pod, annotations)
}

func (r *Reconciler) saveAnnotations(ctx context.Context, pod *corev1.Pod, annotations map[string]string) error {
	newPod := &corev1.Pod{}
	pod.DeepCopyInto(newPod) // Do not change informer copy
	newPod.Annotations = annotations
	// This signal to the Trigger reconciler that we reconciled the scheduled resources.
	if _, err := r.Kube.Pods(newPod.GetNamespace()).Update(ctx, newPod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update pod %s/%s: %w", pod.GetNamespace(), pod.GetName(), err)
	}
	return nil
}

func addSchedulablesAnnotations(schedulables []ScheduledResource, annotations map[string]string) {
	for _, sc := range schedulables {
		if sc.GetStatus() == nil {
			continue
		}
		key := podplacement.AnnotationKey(sc)
		value := sc.GetStatus().ObservedGeneration
		annotations[key] = fmt.Sprintf("%d", value)
	}
}

func preserveNonSchedulerAnnotations(pod *corev1.Pod, annotations map[string]string) {
	for k, v := range pod.GetAnnotations() {
		if !strings.HasPrefix(k, podplacement.Prefix) {
			annotations[k] = v
		}
	}
}

func filterSchedulablesOf(schedulables []ScheduledResource, pod *corev1.Pod) []ScheduledResource {
	currentPodSchedulables := make([]ScheduledResource, 0, len(schedulables))
	for _, schedulable := range schedulables {
		for _, placement := range schedulable.GetPlacements() {
			if placement.PodName == pod.Name {
				currentPodSchedulables = append(currentPodSchedulables, schedulable)
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
