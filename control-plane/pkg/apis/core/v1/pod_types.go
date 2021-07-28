package v1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook/resourcesemantics"
)

var (
	volumeName = "kafka-broker-brokers-triggers"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Pod struct {
	corev1.Pod
}

func (p *Pod) DeepCopyObject() runtime.Object {
	cp := p.Pod.DeepCopyObject().(*corev1.Pod)
	return &Pod{Pod: *cp}
}

func (p *Pod) SetDefaults(ctx context.Context) {
	logging.FromContext(ctx).Info("SetDefaults Pod")
	if p.Namespace != system.Namespace() {
		return
	}
	if v, ok := p.Labels["app"]; !ok || v != "kafka-broker-dispatcher" {
		return
	}
	found := false
	for i := range p.Spec.Volumes {
		if p.Spec.Volumes[i].Name == volumeName {
			found = true
			p.Spec.Volumes[i].ConfigMap.Name = p.GetName()
			p.Spec.Volumes[i].ConfigMap.Optional = pointer.BoolPtr(true)
		}
	}
	if !found {
		found = false
		for i := range p.Spec.Containers {
			for j := range p.Spec.Containers[i].VolumeMounts {
				if p.Spec.Containers[i].VolumeMounts[j].Name == volumeName {
					found = true
				}
			}
			if !found {
				p.Spec.Containers[i].VolumeMounts = append(p.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					ReadOnly:  true,
					MountPath: "/etc/brokers-triggers",
				})
			}
		}
		p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: p.GetName(),
					},
					Optional: pointer.BoolPtr(true),
				},
			},
		})
	}
}

func (p *Pod) Validate(ctx context.Context) *apis.FieldError { return nil }

var (
	_ resourcesemantics.GenericCRD = &Pod{}
)
