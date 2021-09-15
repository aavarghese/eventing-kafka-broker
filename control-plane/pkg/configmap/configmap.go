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

package configmap

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DataKey is the key of the data field to save the contract to.
	DataKey = "data"
)

func GetOrCreate(ctx context.Context, kube kubernetes.Interface, namespace, name string) (*corev1.ConfigMap, error) {
	cm, err := kube.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return create(ctx, kube, namespace, name)
		}
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, name, err)
	}
	return cm, nil
}

func Update(ctx context.Context, kube kubernetes.Interface, cm *corev1.ConfigMap, contract proto.Message) error {
	bytes, err := protojson.Marshal(contract)
	if err != nil {
		return fmt.Errorf("failed to marshal contract: %w", err)
	}
	cm.BinaryData[DataKey] = bytes
	_, err = kube.CoreV1().ConfigMaps(cm.GetNamespace()).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func create(ctx context.Context, kube kubernetes.Interface, namespace string, name string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		BinaryData: map[string][]byte{DataKey: []byte("")},
	}
	return kube.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
}
