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
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/eventing-kafka/pkg/common/scheduler"
	eventing "knative.dev/eventing/pkg/apis/eventing/v1"

	kafkaduck "knative.dev/eventing-kafka/pkg/apis/duck/v1alpha1"

	"knative.dev/eventing-kafka-broker/control-plane/pkg/reconciler/broker"
)

const (
	prefix           = "knative.kafka.scheduler."
	placementsPrefix = prefix + "placements."
	podNameField     = "PodName"
	vReplicasField   = "VReplicas"
	numVPodFields    = 3
)

var (
	_ scheduler.VPod = &vPod{}
)

type vPod struct {
	*eventing.Trigger
	broker    *eventing.Broker
	vReplicas int32

	// placements is set by the GetPlacements() method.
	placements []kafkaduck.Placement
}

func newVPod(t *eventing.Trigger, b *eventing.Broker) *vPod {
	vp := &vPod{
		Trigger:   t,
		broker:    b,
		vReplicas: countVReplicas(t),
		// placements is set by the GetPlacements() method.
	}
	return vp
}

func (v *vPod) GetKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: v.GetNamespace(),
		Name:      v.GetName(),
	}
}

func countVReplicas(t *eventing.Trigger) int32 {
	replicasStr, ok := t.Annotations[ReplicasAnnotationKey]
	if !ok {
		return 1
	}
	replicas, err := strconv.ParseInt(replicasStr, 10, 32)
	if err != nil {
		return 1
	}
	return int32(replicas)
}

func (v *vPod) GetVReplicas() int32 {
	return v.vReplicas
}

func (v *vPod) GetPlacements() []kafkaduck.Placement {
	if v.placements == nil {
		v.placements = getPlacements(v.Status.Annotations)
	}
	return v.placements
}

func (v *vPod) SetPlacements(placements []kafkaduck.Placement) {
	v.removeSchedulerAnnotations()
	v.setSchedulerAnnotations(placements)
}

func (v *vPod) removeSchedulerAnnotations() {
	nonSchedulerAnnotations := make(map[string]string, len(v.Status.Annotations))
	for k, v := range v.Status.Annotations {
		if !strings.HasPrefix(k, placementsPrefix) {
			nonSchedulerAnnotations[k] = v
		}
	}
	v.Status.Annotations = nonSchedulerAnnotations
}

func (v *vPod) setSchedulerAnnotations(placements []kafkaduck.Placement) {
	for i, p := range placements {
		if v.Status.Annotations == nil {
			v.Status.Annotations = make(map[string]string, len(placements))
		}
		v.Status.Annotations[fmt.Sprintf("%s%d.%s", placementsPrefix, i, podNameField)] = p.PodName
		v.Status.Annotations[fmt.Sprintf("%s%d.%s", placementsPrefix, i, vReplicasField)] = strconv.Itoa(int(p.VReplicas))
	}
}

func getPlacements(annotations map[string]string) []kafkaduck.Placement {
	// scheduler.placements.0.PodName
	// scheduler.placements.0.VReplicas

	count := 0
	for k := range annotations {
		if strings.HasPrefix(k, placementsPrefix) {
			count++
		}
	}
	if count <= 0 {
		return nil
	}
	placements := make([]kafkaduck.Placement, count/numVPodFields)
	for k, v := range annotations {
		if strings.HasPrefix(k, placementsPrefix) {
			parts := strings.Split(k, ".")
			idx, err := strconv.Atoi(parts[2])
			if err != nil {
				panic(err) // Bug in SetPlacements
			}
			field := parts[3]
			switch field {
			case podNameField:
				placements[idx].PodName = v
			case vReplicasField:
				r, err := strconv.ParseInt(v, 10, 32)
				if err != nil {
					panic(err) // TODO don't panic
				}
				placements[idx].VReplicas = int32(r)
			}
		}
	}
	return placements
}

func (v *vPod) GetTopics() []string {
	return []string{v.Status.Annotations[broker.TopicNameStatusAnnotationKey]}
}

func (v *vPod) GetBootstrapServers() string {
	return v.Status.Annotations[broker.BootstrapServersStatusAnnotationKey]
}
