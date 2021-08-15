package v1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage/names"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook/resourcesemantics"
)

var (
	volumeName    = "kafka-broker-brokers-triggers"
	containerName = "kafka-broker-dispatcher"
	mountPath     = "/etc/brokers-triggers"
)

var namespace = system.Namespace()

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Pod struct {
	corev1.Pod
}

func (p *Pod) DeepCopyObject() runtime.Object {
	cp := p.Pod.DeepCopyObject().(*corev1.Pod)
	return &Pod{Pod: *cp}
}

func (p *Pod) SetDefaults(ctx context.Context) {

	cmName := names.SimpleNameGenerator.GenerateName(p.GetGenerateName())

	logging.FromContext(ctx).Infof("SetDefaults generated name %s", cmName)

	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cmName,
				},
			},
		},
	})

	for i := range p.Spec.Containers {
		if p.Spec.Containers[i].Name == containerName {
			p.Spec.Containers[i].VolumeMounts = append(p.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				ReadOnly:  true,
				MountPath: mountPath,
			})
		}
	}
}

func (p *Pod) Validate(context.Context) *apis.FieldError { return nil }

var (
	_ resourcesemantics.GenericCRD = &Pod{}
)
