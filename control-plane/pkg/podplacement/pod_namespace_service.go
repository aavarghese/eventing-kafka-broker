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

package podplacement

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubecorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	injectionkubeclient "knative.dev/pkg/client/injection/kube/client"
	injectioncorelister "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

type PodNamespaceService struct {
	podNamespaceLister corelisters.PodNamespaceLister
	podNamespaceClient kubecorev1.PodInterface
	selector           labels.Selector
}

func NewPodNamespaceGetter(ctx context.Context, namespace string, selector labels.Selector) *PodNamespaceService {
	return &PodNamespaceService{
		// TODO this setup a global watch, we want a namespaced-scope watch.
		podNamespaceLister: injectioncorelister.Get(ctx).Lister().Pods(namespace),
		podNamespaceClient: injectionkubeclient.Get(ctx).CoreV1().Pods(namespace),
		selector:           selector,
	}
}

// Get gets a pod either from the lister cache or from the API server if not found in the cache.
func (pg *PodNamespaceService) Get(ctx context.Context, name string) (*corev1.Pod, error) {
	pod, err := pg.podNamespaceLister.Get(name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			// Generic error.
			return nil, fmt.Errorf("failed to get pod %s/%s: %w", pod.GetNamespace(), pod.GetName(), err)
		}
		// Pod not found in the cache.
		// Try to get the pod from the API Server.
		pod, err := pg.podNamespaceClient.Get(ctx, pod.GetName(), metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get pod %s/%s: %w", pod.GetNamespace(), pod.GetName(), err)
		}
		return pod, nil
	}
	return pod, nil
}

// RemoveAnnotationsFor removes annotations related to the given object from all pods.
func (pg *PodNamespaceService) RemoveAnnotationsFor(ctx context.Context, object duckv1.KRShaped) error {
	prefix := AnnotationKey(object)
	pods, err := pg.podNamespaceLister.List(pg.selector)
	if err != nil {
		return err
	}
	for _, p := range pods {
		annotations := make(map[string]string, len(p.GetAnnotations()))
		for k, v := range p.GetAnnotations() {
			if !strings.HasPrefix(k, prefix) {
				annotations[k] = v
			}
		}
		if len(annotations) == len(p.GetAnnotations()) {
			continue // No difference.
		}

		_, err = pg.podNamespaceClient.Update(ctx, p, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to update pod %s/%s: %w", p.GetNamespace(), p.GetName(), err)
		}
	}
	return nil
}

const (
	Prefix = "knative.kafka.scheduler."
)

func AnnotationKey(object duckv1.KRShaped) string {
	return fmt.Sprintf("%s%s/%s", Prefix, object.GetNamespace(), object.GetName())
}

func (pg *PodNamespaceService) GetGenerationFor(ctx context.Context, podName string, object duckv1.KRShaped) (int64, error) {
	// pod pointer can point to the informer cache, so it's read-only.
	pod, err := pg.Get(ctx, podName)
	if err != nil {
		return -1, nil
	}
	if pod == nil {
		return -1, nil
	}
	return getPodGenerationFor(pod, object), nil
}

func getPodGenerationFor(pod *corev1.Pod, object duckv1.KRShaped) int64 {
	if pod.GetAnnotations() == nil {
		return -1
	}
	annotation := AnnotationKey(object)
	vPodGenStr, ok := pod.GetAnnotations()[annotation]
	if !ok {
		return -1
	}
	vPodGen, err := strconv.ParseInt(vPodGenStr, 10, 64)
	if err != nil {
		panic(err)
	}
	return vPodGen
}
