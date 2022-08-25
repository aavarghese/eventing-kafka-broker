/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	v1alpha1 "knative.dev/eventing-autoscaler-keda/third_party/pkg/apis/keda/v1alpha1"
	scheme "knative.dev/eventing-autoscaler-keda/third_party/pkg/client/clientset/versioned/scheme"
)

// ScaledJobsGetter has a method to return a ScaledJobInterface.
// A group's client should implement this interface.
type ScaledJobsGetter interface {
	ScaledJobs(namespace string) ScaledJobInterface
}

// ScaledJobInterface has methods to work with ScaledJob resources.
type ScaledJobInterface interface {
	Create(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.CreateOptions) (*v1alpha1.ScaledJob, error)
	Update(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.UpdateOptions) (*v1alpha1.ScaledJob, error)
	UpdateStatus(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.UpdateOptions) (*v1alpha1.ScaledJob, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.ScaledJob, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.ScaledJobList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ScaledJob, err error)
	ScaledJobExpansion
}

// scaledJobs implements ScaledJobInterface
type scaledJobs struct {
	client rest.Interface
	ns     string
}

// newScaledJobs returns a ScaledJobs
func newScaledJobs(c *KedaV1alpha1Client, namespace string) *scaledJobs {
	return &scaledJobs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the scaledJob, and returns the corresponding scaledJob object, and an error if there is any.
func (c *scaledJobs) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ScaledJob, err error) {
	result = &v1alpha1.ScaledJob{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("scaledjobs").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ScaledJobs that match those selectors.
func (c *scaledJobs) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ScaledJobList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.ScaledJobList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("scaledjobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested scaledJobs.
func (c *scaledJobs) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("scaledjobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a scaledJob and creates it.  Returns the server's representation of the scaledJob, and an error, if there is any.
func (c *scaledJobs) Create(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.CreateOptions) (result *v1alpha1.ScaledJob, err error) {
	result = &v1alpha1.ScaledJob{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("scaledjobs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(scaledJob).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a scaledJob and updates it. Returns the server's representation of the scaledJob, and an error, if there is any.
func (c *scaledJobs) Update(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.UpdateOptions) (result *v1alpha1.ScaledJob, err error) {
	result = &v1alpha1.ScaledJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("scaledjobs").
		Name(scaledJob.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(scaledJob).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *scaledJobs) UpdateStatus(ctx context.Context, scaledJob *v1alpha1.ScaledJob, opts v1.UpdateOptions) (result *v1alpha1.ScaledJob, err error) {
	result = &v1alpha1.ScaledJob{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("scaledjobs").
		Name(scaledJob.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(scaledJob).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the scaledJob and deletes it. Returns an error if one occurs.
func (c *scaledJobs) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("scaledjobs").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *scaledJobs) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("scaledjobs").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched scaledJob.
func (c *scaledJobs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ScaledJob, err error) {
	result = &v1alpha1.ScaledJob{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("scaledjobs").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
