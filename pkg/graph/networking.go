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
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NetworkingV1Graph is used to graph all networking resources.
type NetworkingV1Graph struct {
	graph *Graph
}

// NewNetworkingV1Graph creates a new NetworkingV1Graph.
func NewNetworkingV1Graph(g *Graph) *NetworkingV1Graph {
	return &NetworkingV1Graph{
		graph: g,
	}
}

// NetworkingV1 retrieves the NetworkingV1Graph.
func (g *Graph) NetworkingV1() *NetworkingV1Graph {
	return g.networkingV1
}

// Unstructured adds an unstructured node to the Graph.
func (g *NetworkingV1Graph) Unstructured(unstr *unstructured.Unstructured) (err error) {
	switch unstr.GetKind() {
	case "NetworkPolicy":
		obj := &v1.NetworkPolicy{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.NetworkPolicy(obj)
	}

	return err
}

// NetworkPolicy adds a v1.NetworkPolicy resource to the Graph.
func (g *NetworkingV1Graph) NetworkPolicy(obj *v1.NetworkPolicy) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	selector, err := metav1.LabelSelectorAsSelector(&obj.Spec.PodSelector)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{LabelSelector: selector.String(), FieldSelector: "status.phase=Running"}
	pods, err := g.graph.clientset.CoreV1().Pods(obj.GetNamespace()).List(options)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		p, err := g.graph.CoreV1().Pod(&pod)
		if err != nil {
			return nil, err
		}
		g.graph.Relationship(n, "Pod", p)
	}

	return n, nil
}
