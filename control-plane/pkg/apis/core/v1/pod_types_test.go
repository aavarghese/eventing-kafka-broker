package v1

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	reconcilertesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/webhook/resourcesemantics"
)

func TestSetDefaults(t *testing.T) {
	ctx, _ := reconcilertesting.SetupFakeContext(t)
	p := &Pod{}
	cp := p.DeepCopyObject()
	gcrd := cp.(resourcesemantics.GenericCRD)
	_ = gcrd

	_ = os.Setenv("SYSTEM_NAMESPACE", "knative-eventing")

	tm := metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	}
	om := metav1.ObjectMeta{
		Name:      "kafka-broker-disptacher-abc",
		Namespace: "knative-eventing",
	}

	before := &Pod{
		Pod: corev1.Pod{
			TypeMeta:   tm,
			ObjectMeta: om,
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "aaa",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "aaa",
								},
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  containerName,
						Image: "knative.dev/app",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "aaa",
								ReadOnly:  true,
								MountPath: "/etc/config",
							},
						},
					},
				},
			},
		},
	}

	after := &Pod{
		Pod: corev1.Pod{
			TypeMeta:   tm,
			ObjectMeta: om,
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "aaa",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "aaa",
								},
							},
						},
					},
					{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: om.GetName(),
								},
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  containerName,
						Image: "knative.dev/app",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "aaa",
								ReadOnly:  true,
								MountPath: "/etc/config",
							},
							{
								Name:      volumeName,
								ReadOnly:  true,
								MountPath: mountPath,
							},
						},
					},
				},
			},
		},
	}

	before.SetDefaults(ctx)

	if diff := cmp.Diff(before, after); diff != "" {
		t.Errorf(diff)
	}
}
