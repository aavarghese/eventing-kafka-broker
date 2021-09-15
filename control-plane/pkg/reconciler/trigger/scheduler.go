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
	kafkaduck "knative.dev/eventing-kafka/pkg/apis/duck/v1alpha1"
	eventing "knative.dev/eventing/pkg/apis/eventing/v1"

	"knative.dev/eventing-kafka/pkg/common/scheduler"
)

const (
	prefix           = "scheduler."
	placementsPrefix = prefix + "placements."
	podNameField     = "PodName"
	zoneNameField    = "ZoneName"
	vReplicasField   = "VReplicas"
	numVPodFields    = 3
)

var (
	_ scheduler.VPod = &vPod{}
)

type vPod struct {
	trigger   *eventing.Trigger
	vReplicas int32

	// placements is set by the GetPlacements() method.
	placements []kafkaduck.Placement
}

func (v *vPod) GetKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: v.trigger.GetNamespace(),
		Name:      v.trigger.GetName(),
	}
}

func (v *vPod) GetVReplicas() int32 {
	return v.vReplicas
}

func (v *vPod) GetPlacements() []kafkaduck.Placement {
	if v.placements == nil {
		v.placements = getPlacements(v.trigger.Status.Annotations)
	}
	return v.placements
}

func (v *vPod) SetPlacements(placements []kafkaduck.Placement) {
	v.removeSchedulerAnnotations()
	v.setSchedulerAnnotations(placements)
}

func (v *vPod) removeSchedulerAnnotations() {
	nonSchedulerAnnotations := make(map[string]string, len(v.trigger.Status.Annotations))
	for k, v := range v.trigger.Status.Annotations {
		if !strings.HasPrefix(k, placementsPrefix) {
			nonSchedulerAnnotations[k] = v
		}
	}
	v.trigger.Status.Annotations = nonSchedulerAnnotations
}

func (v *vPod) setSchedulerAnnotations(placements []kafkaduck.Placement) {
	for i, p := range placements {
		if v.trigger.Status.Annotations == nil {
			v.trigger.Status.Annotations = make(map[string]string, len(placements))
		}
		v.trigger.Status.Annotations[fmt.Sprintf("%s%d.%s", placementsPrefix, i, podNameField)] = p.PodName
		v.trigger.Status.Annotations[fmt.Sprintf("%s%d.%s", placementsPrefix, i, zoneNameField)] = p.ZoneName
		v.trigger.Status.Annotations[fmt.Sprintf("%s%d.%s", placementsPrefix, i, vReplicasField)] = strconv.Itoa(int(p.VReplicas))
	}
}

func getPlacements(annotations map[string]string) []kafkaduck.Placement {
	// scheduler.placements.0.PodName
	// scheduler.placements.0.ZoneName
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
				panic(err) // TODO don't panic
			}
			field := parts[3]
			switch field {
			case podNameField:
				placements[idx].PodName = v
			case zoneNameField:
				placements[idx].ZoneName = v
			case vReplicasField:
				r, err := strconv.Atoi(v)
				if err != nil {
					panic(err) // TODO don't panic
				}
				placements[idx].VReplicas = int32(r)
			}
		}
	}
	return placements
}
