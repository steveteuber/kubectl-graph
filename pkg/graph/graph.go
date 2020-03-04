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
	"strings"
	"text/template"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
)

var (
	cypherTemplate = strings.Replace(
		`// create nodes
		:begin
		{{- range .Nodes }}
		MERGE (node:{{ .Kind }} {UID: "{{ .UID }}"}) ON CREATE SET node.Namespace = "{{ .Namespace }}", node.Name = "{{ .Name }}";
		{{- end }}
		:commit

		// wait for index completion
		call db.awaitIndexes();

		// create relationships
		:begin
		{{- range .Relationships }}
		MATCH (from:{{ .From.Kind }}),(to:{{ .To.Kind }}) WHERE from.UID = "{{ .From.UID }}" AND to.UID = "{{ .To.UID }}" MERGE (from)-[:{{ .Type }}]->(to);
		{{- end }}
		:commit
		`, "\t\t", "", -1)

	graphvizTemplate = strings.Replace(
		`digraph {
		    bgcolor="#F6F6F6";

		    // create nodes
		    {{- range .Nodes }}
		    "{{ .UID }}" [label="{{ .Name }}"];
		    {{- end }}

		    // create relationships
		    {{- range .Relationships }}
		    "{{ .From.UID }}" -> "{{ .To.UID }}" [label="&nbsp;{{ .Type }}"];
		    {{- end }}
		}
		`, "\t\t", "", -1)

	templates = template.New("output")
)

func init() {
	template.Must(templates.New("cypher").Parse(cypherTemplate))
	template.Must(templates.New("graphviz").Parse(graphvizTemplate))
}

// Graph stores nodes and relationships between them.
type Graph struct {
	Nodes         map[types.UID]*Node
	Relationships []*Relationship

	clientset *kubernetes.Clientset
}

// Node represents a node in the graph.
type Node v1.ObjectReference

// Relationship represents a relationship between nodes in the graph.
type Relationship struct {
	From v1.ObjectReference
	Type string
	To   v1.ObjectReference
}

// NewGraph returns a new initialized a Graph.
func NewGraph(clientset *kubernetes.Clientset, objs []*unstructured.Unstructured) (*Graph, error) {
	g := &Graph{
		clientset: clientset,
		Nodes:     make(map[types.UID]*Node),
	}

	errs := []error{}

	for _, obj := range objs {
		err := g.Unstructured(obj)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return g, errors.NewAggregate(errs)
}

// Unstructured adds an unstructured node to the Graph.
func (g *Graph) Unstructured(unstr *unstructured.Unstructured) error {
	gvk := unstr.GroupVersionKind()
	g.Node(gvk, unstr)

	return nil
}

// Node adds a node and the owner references to the Graph.
func (g *Graph) Node(gvk schema.GroupVersionKind, obj metav1.Object) *Node {
	if node, ok := g.Nodes[obj.GetUID()]; ok {
		return node
	}

	apiVersion, kind := gvk.ToAPIVersionAndKind()
	node := &Node{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		UID:        obj.GetUID(),
	}
	g.Nodes[obj.GetUID()] = node

	if len(obj.GetOwnerReferences()) == 0 {
		ns := g.Node(
			schema.FromAPIVersionAndKind("v1", "Namespace"),
			&metav1.ObjectMeta{
				UID:       types.UID(obj.GetNamespace()), // TODO: use real Namespace.UID
				Name:      obj.GetNamespace(),
				Namespace: obj.GetNamespace(),
			},
		)
		if node != ns {
			g.Relationship(ns, kind, node)
		}
	}

	for _, ownerRef := range obj.GetOwnerReferences() {
		owner := g.Node(
			schema.FromAPIVersionAndKind(ownerRef.APIVersion, ownerRef.Kind),
			&metav1.ObjectMeta{
				UID:       ownerRef.UID,
				Name:      ownerRef.Name,
				Namespace: obj.GetNamespace(),
			},
		)
		g.Relationship(owner, kind, node)
	}

	return node
}

// Relationship creates a new relationship between two nodes.
func (g *Graph) Relationship(from *Node, label string, to *Node) *Relationship {
	relationship := &Relationship{
		From: v1.ObjectReference{UID: from.UID, Kind: from.Kind},
		Type: label,
		To:   v1.ObjectReference{UID: to.UID, Kind: to.Kind},
	}
	g.Relationships = append(g.Relationships, relationship)

	return relationship
}

// String returns the graph in requested format.
func (g Graph) String(format string) string {
	b := &bytes.Buffer{}
	g.Write(b, format)

	return b.String()
}

// Write formats according to the requested format and writes to w.
func (g Graph) Write(w io.Writer, format string) error {
	err := templates.ExecuteTemplate(w, format, g)
	if err != nil {
		return err
	}

	return nil
}
