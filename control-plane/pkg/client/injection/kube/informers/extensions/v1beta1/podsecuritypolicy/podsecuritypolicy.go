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

// Code generated by injection-gen. DO NOT EDIT.

package podsecuritypolicy

import (
	context "context"

	apiextensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	v1beta1 "k8s.io/client-go/informers/extensions/v1beta1"
	kubernetes "k8s.io/client-go/kubernetes"
	extensionsv1beta1 "k8s.io/client-go/listers/extensions/v1beta1"
	cache "k8s.io/client-go/tools/cache"
	client "knative.dev/eventing-kafka-broker/control-plane/pkg/client/injection/kube/client"
	factory "knative.dev/eventing-kafka-broker/control-plane/pkg/client/injection/kube/informers/factory"
	controller "knative.dev/pkg/controller"
	injection "knative.dev/pkg/injection"
	logging "knative.dev/pkg/logging"
)

func init() {
	injection.Default.RegisterInformer(withInformer)
	injection.Dynamic.RegisterDynamicInformer(withDynamicInformer)
}

// Key is used for associating the Informer inside the context.Context.
type Key struct{}

func withInformer(ctx context.Context) (context.Context, controller.Informer) {
	f := factory.Get(ctx)
	inf := f.Extensions().V1beta1().PodSecurityPolicies()
	return context.WithValue(ctx, Key{}, inf), inf.Informer()
}

func withDynamicInformer(ctx context.Context) context.Context {
	inf := &wrapper{client: client.Get(ctx)}
	return context.WithValue(ctx, Key{}, inf)
}

// Get extracts the typed informer from the context.
func Get(ctx context.Context) v1beta1.PodSecurityPolicyInformer {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		logging.FromContext(ctx).Panic(
			"Unable to fetch k8s.io/client-go/informers/extensions/v1beta1.PodSecurityPolicyInformer from context.")
	}
	return untyped.(v1beta1.PodSecurityPolicyInformer)
}

type wrapper struct {
	client kubernetes.Interface
}

var _ v1beta1.PodSecurityPolicyInformer = (*wrapper)(nil)
var _ extensionsv1beta1.PodSecurityPolicyLister = (*wrapper)(nil)

func (w *wrapper) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(nil, &apiextensionsv1beta1.PodSecurityPolicy{}, 0, nil)
}

func (w *wrapper) Lister() extensionsv1beta1.PodSecurityPolicyLister {
	return w
}

func (w *wrapper) List(selector labels.Selector) (ret []*apiextensionsv1beta1.PodSecurityPolicy, err error) {
	lo, err := w.client.ExtensionsV1beta1().PodSecurityPolicies().List(context.TODO(), v1.ListOptions{
		LabelSelector: selector.String(),
		// TODO(mattmoor): Incorporate resourceVersion bounds based on staleness criteria.
	})
	if err != nil {
		return nil, err
	}
	for idx := range lo.Items {
		ret = append(ret, &lo.Items[idx])
	}
	return ret, nil
}

func (w *wrapper) Get(name string) (*apiextensionsv1beta1.PodSecurityPolicy, error) {
	return w.client.ExtensionsV1beta1().PodSecurityPolicies().Get(context.TODO(), name, v1.GetOptions{
		// TODO(mattmoor): Incorporate resourceVersion bounds based on staleness criteria.
	})
}
