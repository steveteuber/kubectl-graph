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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CoreV1Graph is used to graph all core resources.
type CoreV1Graph struct {
	graph *Graph
}

// NewCoreV1Graph creates a new CoreV1Graph.
func NewCoreV1Graph(g *Graph) *CoreV1Graph {
	return &CoreV1Graph{
		graph: g,
	}
}

// CoreV1 retrieves the CoreV1Graph.
func (g *Graph) CoreV1() *CoreV1Graph {
	return g.coreV1
}

// Unstructured adds an unstructured node to the Graph.
func (g *CoreV1Graph) Unstructured(unstr *unstructured.Unstructured) (err error) {
	return err
}
