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
	"bytes"
	"io"
	"text/template"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

var (
	dotTemplate = `digraph {}
`

	cypherTemplate = `// create nodes
:begin
{{- range $uid, $node := .Nodes }}
MERGE (node:{{ $node.Kind }} {UID: "{{ $uid }}"}) ON CREATE SET node.Namespace = "{{ $node.Namespace }}", node.Name = "{{ $node.Name }}";
{{- end }}
:commit

// wait for index completion
call db.awaitIndexes();

// create relationships
:begin
{{- range $i, $relation := .Relationships }}
MATCH (from:{{ $relation.From.Kind }}),(to:{{ $relation.To.Kind }}) WHERE from.UID = "{{ $relation.From.UID }}" AND to.UID = "{{ $relation.To.UID }}" MERGE (from)-[:{{ $relation.Type }}]->(to);
{{- end }}
:commit
`
)

// Graph stores nodes and relationships between them.
type Graph struct {
	Nodes         map[types.UID]v1.ObjectReference
	Relationships []Relationship
}

// Relationship represents a relationship between nodes in the graph.
type Relationship struct {
	From v1.ObjectReference
	Type string
	To   v1.ObjectReference
}

// NewGraph returns a new initialized a Graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[types.UID]v1.ObjectReference),
	}
}

// AddNode adds a node to the Graph and detects the relationships.
func (g *Graph) AddNode(obj *unstructured.Unstructured) error {
	if len(obj.GetOwnerReferences()) == 0 {
		references := make([]metav1.OwnerReference, 1)
		references[0] = metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Namespace",
			Name:       obj.GetNamespace(),
			UID:        types.UID(obj.GetNamespace()), // TODO: use real Namespace.UID
		}

		obj.SetOwnerReferences(references)
	}

	g.Nodes[obj.GetUID()] = v1.ObjectReference{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		UID:        obj.GetUID(),
	}

	for _, owner := range obj.GetOwnerReferences() {
		// Check if OwnerReference exists as a Node in the Graph
		if _, exists := g.Nodes[owner.UID]; !exists {
			g.Nodes[owner.UID] = v1.ObjectReference{
				APIVersion: owner.APIVersion,
				Kind:       owner.Kind,
				Name:       owner.Name,
				Namespace:  obj.GetNamespace(),
				UID:        owner.UID,
			}
		}

		relationship := Relationship{
			From: v1.ObjectReference{
				Kind: owner.Kind,
				UID:  owner.UID,
			},
			Type: obj.GetKind(),
			To: v1.ObjectReference{
				Kind: obj.GetKind(),
				UID:  obj.GetUID(),
			},
		}

		g.Relationships = append(g.Relationships, relationship)
	}

	return nil
}

// String returns the graph in requested format.
func (g Graph) String(format string) string {
	b := &bytes.Buffer{}
	g.Write(b, format)

	return b.String()
}

// Write formats according to the requested format and writes to w.
func (g Graph) Write(w io.Writer, format string) error {
	tpl, err := template.New(format).Parse(cypherTemplate)
	if err != nil {
		return err
	}

	return tpl.Execute(w, g)
}
