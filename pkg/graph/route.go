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
	"context"

	v1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RouteV1Graph is used to graph all routing resources.
type RouteV1Graph struct {
	graph *Graph
}

// NewRouteV1Graph creates a new RouteV1Graph.
func NewRouteV1Graph(g *Graph) *RouteV1Graph {
	return &RouteV1Graph{
		graph: g,
	}
}

// RouteV1 retrieves the RouteV1Graph.
func (g *Graph) RouteV1() *RouteV1Graph {
	return g.routeV1
}

// Unstructured adds an unstructured node to the Graph.
func (g *RouteV1Graph) Unstructured(unstr *unstructured.Unstructured) (err error) {
	switch unstr.GetKind() {
	case "Route":
		obj := &v1.Route{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Route(obj)
	}

	return err
}

// Route adds a v1.Route resource to the Graph.
func (g *RouteV1Graph) Route(obj *v1.Route) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	options := metav1.GetOptions{}
	service, err := g.graph.clientset.CoreV1().Services(obj.GetNamespace()).Get(context.TODO(), obj.Spec.To.Name, options)
	if err != nil {
		return nil, err
	}

	s, err := g.graph.CoreV1().Service(service)
	if err != nil {
		return nil, err
	}
	g.graph.Relationship(n, "Route", s)

	return n, nil
}
