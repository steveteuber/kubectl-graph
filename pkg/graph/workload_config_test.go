// Copyright 2020 Steve Teuber
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package graph

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestWorkloadVolumeConfigReferences(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		configKind string
		configName string
		podSpec    v1.PodSpec
	}{
		{
			name:       "deployment configmap volume",
			kind:       "Deployment",
			configKind: "ConfigMap",
			configName: "deployment-config",
			podSpec: v1.PodSpec{Volumes: []v1.Volume{{VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: "deployment-config"}},
			}}}},
		},
		{
			name:       "statefulset secret volume",
			kind:       "StatefulSet",
			configKind: "Secret",
			configName: "statefulset-secret",
			podSpec: v1.PodSpec{Volumes: []v1.Volume{{VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{SecretName: "statefulset-secret"},
			}}}},
		},
		{
			name:       "job configmap reference",
			kind:       "Job",
			configKind: "ConfigMap",
			configName: "job-config",
			podSpec: v1.PodSpec{Containers: []v1.Container{{EnvFrom: []v1.EnvFromSource{{
				ConfigMapRef: &v1.ConfigMapEnvSource{LocalObjectReference: v1.LocalObjectReference{Name: "job-config"}},
			}}}}},
		},
		{
			name:       "cronjob nested template reference",
			kind:       "CronJob",
			configKind: "Secret",
			configName: "cronjob-secret",
			podSpec: v1.PodSpec{Containers: []v1.Container{{EnvFrom: []v1.EnvFromSource{{
				SecretRef: &v1.SecretEnvSource{LocalObjectReference: v1.LocalObjectReference{Name: "cronjob-secret"}},
			}}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newTestGraph()
			target := addConfigNode(g, tt.configKind, "default", tt.configName, "config-uid")
			workload := newWorkload(t, tt.kind, "default", "workload", "workload-uid", tt.podSpec)

			from, err := g.Unstructured(workload)
			if err != nil {
				t.Fatal(err)
			}

			assertRelationshipCount(t, g, from, tt.configKind, target, 1)
		})
	}
}

func TestWorkloadEnvironmentReferences(t *testing.T) {
	g := newTestGraph()
	targets := []*Node{
		addConfigNode(g, "ConfigMap", "default", "regular-config", "regular-config-uid"),
		addConfigNode(g, "Secret", "default", "regular-secret", "regular-secret-uid"),
		addConfigNode(g, "ConfigMap", "default", "init-config", "init-config-uid"),
		addConfigNode(g, "Secret", "default", "init-secret", "init-secret-uid"),
	}
	podSpec := v1.PodSpec{
		Containers: []v1.Container{{
			EnvFrom: []v1.EnvFromSource{{ConfigMapRef: &v1.ConfigMapEnvSource{
				LocalObjectReference: v1.LocalObjectReference{Name: "regular-config"},
			}}},
			Env: []v1.EnvVar{{ValueFrom: &v1.EnvVarSource{SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: "regular-secret"},
			}}}},
		}},
		InitContainers: []v1.Container{{
			EnvFrom: []v1.EnvFromSource{{SecretRef: &v1.SecretEnvSource{
				LocalObjectReference: v1.LocalObjectReference{Name: "init-secret"},
			}}},
			Env: []v1.EnvVar{{ValueFrom: &v1.EnvVarSource{ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: "init-config"},
			}}}},
		}},
	}

	from, err := g.Unstructured(newWorkload(t, "DaemonSet", "default", "workload", "workload-uid", podSpec))
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range targets {
		assertRelationshipCount(t, g, from, target.Kind, target, 1)
	}
}

func TestWorkloadProjectedVolumeReferences(t *testing.T) {
	g := newTestGraph()
	configMap := addConfigNode(g, "ConfigMap", "default", "projected-config", "config-uid")
	secret := addConfigNode(g, "Secret", "default", "projected-secret", "secret-uid")
	podSpec := v1.PodSpec{Volumes: []v1.Volume{{VolumeSource: v1.VolumeSource{Projected: &v1.ProjectedVolumeSource{
		Sources: []v1.VolumeProjection{
			{ConfigMap: &v1.ConfigMapProjection{LocalObjectReference: v1.LocalObjectReference{Name: "projected-config"}}},
			{Secret: &v1.SecretProjection{LocalObjectReference: v1.LocalObjectReference{Name: "projected-secret"}}},
		},
	}}}}}

	from, err := g.Unstructured(newWorkload(t, "Deployment", "default", "workload", "workload-uid", podSpec))
	if err != nil {
		t.Fatal(err)
	}

	assertRelationshipCount(t, g, from, "ConfigMap", configMap, 1)
	assertRelationshipCount(t, g, from, "Secret", secret, 1)
}

func TestWorkloadConfigReferencesResolveWithinNamespace(t *testing.T) {
	g := newTestGraph()
	configA := addConfigNode(g, "ConfigMap", "namespace-a", "shared", "config-a-uid")
	configB := addConfigNode(g, "ConfigMap", "namespace-b", "shared", "config-b-uid")
	podSpec := v1.PodSpec{Volumes: []v1.Volume{{VolumeSource: v1.VolumeSource{
		ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: "shared"}},
	}}}}

	workloadA, err := g.Unstructured(newWorkload(t, "Deployment", "namespace-a", "workload-a", "workload-a-uid", podSpec))
	if err != nil {
		t.Fatal(err)
	}
	workloadB, err := g.Unstructured(newWorkload(t, "Deployment", "namespace-b", "workload-b", "workload-b-uid", podSpec))
	if err != nil {
		t.Fatal(err)
	}

	assertRelationshipCount(t, g, workloadA, "ConfigMap", configA, 1)
	assertRelationshipCount(t, g, workloadA, "ConfigMap", configB, 0)
	assertRelationshipCount(t, g, workloadB, "ConfigMap", configA, 0)
	assertRelationshipCount(t, g, workloadB, "ConfigMap", configB, 1)
}

func TestMissingWorkloadConfigReferenceIsSkipped(t *testing.T) {
	g := newTestGraph()
	podSpec := v1.PodSpec{Volumes: []v1.Volume{{VolumeSource: v1.VolumeSource{
		ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: "missing"}},
	}}}}

	if _, err := g.Unstructured(newWorkload(t, "Deployment", "default", "workload", "workload-uid", podSpec)); err != nil {
		t.Fatal(err)
	}
	if target := g.Nodes[ToUID("ConfigMap", "default", "missing")]; target != nil {
		t.Errorf("unexpected synthetic ConfigMap node: %v", target)
	}
	if got := len(g.RelationshipList()); got != 0 {
		t.Errorf("relationships = %d, want 0", got)
	}
}

func newTestGraph() *Graph {
	g := &Graph{
		Nodes:         make(map[types.UID]*Node),
		Relationships: make(map[types.UID][]*Relationship),
		Options:       &Options{NodeNameLimit: DefaultNodeNameLimit},
	}
	g.coreV1 = NewCoreV1Graph(g)
	return g
}

func addConfigNode(g *Graph, kind, namespace, name string, uid types.UID) *Node {
	return g.Node(
		schema.FromAPIVersionAndKind(v1.SchemeGroupVersion.String(), kind),
		&metav1.ObjectMeta{Namespace: namespace, Name: name, UID: uid},
	)
}

func newWorkload(t *testing.T, kind, namespace, name string, uid types.UID, podSpec v1.PodSpec) *unstructured.Unstructured {
	t.Helper()
	podSpecMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&podSpec)
	if err != nil {
		t.Fatal(err)
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       kind,
		"metadata": map[string]interface{}{
			"namespace": namespace,
			"name":      name,
			"uid":       string(uid),
		},
	}}
	path := []string{"spec", "template", "spec"}
	if kind == "CronJob" {
		path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
	}
	if kind == "Job" || kind == "CronJob" {
		obj.SetAPIVersion("batch/v1")
	}
	if err := unstructured.SetNestedMap(obj.Object, podSpecMap, path...); err != nil {
		t.Fatal(err)
	}

	return obj
}

func assertRelationshipCount(t *testing.T, g *Graph, from *Node, label string, to *Node, want int) {
	t.Helper()
	got := 0
	for _, relationship := range g.Relationships[to.GetUID()] {
		if relationship.From == from.GetUID() && relationship.Label == label {
			got++
		}
	}
	if got != want {
		t.Errorf("%s -[%s]-> %s relationships = %d, want %d", from.GetName(), label, to.GetName(), got, want)
	}
}
