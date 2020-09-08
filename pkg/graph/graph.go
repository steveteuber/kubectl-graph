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
	"crypto/md5"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
)

var (
	cypherTemplate = strings.Replace(
		`// create nodes
		:begin
		{{- range $cluster, $namespaces := .Nodes }}
		MERGE (node:Cluster {UID: "cluster_{{ $cluster }}"}) ON CREATE SET node.Name = "{{ $cluster }}";
			{{- range $namespace, $node := $namespaces }}
				{{- if $namespace }}
		MERGE (node:Namespace {UID: "namespace_{{ $namespace }}"}) ON CREATE SET node.ClusterName = "{{ $cluster }}", node.Name = "{{ $namespace }}";
					{{- range . }}
		MERGE (node:{{ .Kind }} {UID: "{{ .UID }}"}) ON CREATE SET node.ClusterName = "{{ $cluster }}", node.Namespace = "{{ .Namespace }}", node.Name = "{{ .Name }}";
					{{- end }}
				{{- else }}
					{{- range . }}
		MERGE (node:{{ .Kind }} {UID: "{{ .UID }}"}) ON CREATE SET node.ClusterName = "{{ $cluster }}", node.Name = "{{ .Name }}";
					{{- end }}
				{{- end }}
			{{- end }}
		{{- end }}
		:commit

		// wait for index completion
		call db.awaitIndexes();

		// create relationships
		:begin
		{{- range .Relationships }}{{ range . }}
		MATCH (from:{{ .From.Kind }}),(to:{{ .To.Kind }}) WHERE from.UID = "{{ .From.UID }}" AND to.UID = "{{ .To.UID }}" MERGE (from)-[:{{ .Type }}]->(to);
		{{- end }}{{ end }}
		{{- range $cluster, $namespaces := .Nodes }}
		MATCH (cl:Cluster) WHERE cl.UID = "cluster_{{ $cluster }}" MATCH (node) WHERE node.ClusterName = cl.Name AND node.Namespace IS NULL AND NOT(()-[]->(node)) MERGE (node)-[:Cluster]->(cl);
			{{- range $namespace, $node := $namespaces }}
				{{- if $namespace }}
		MATCH (ns:Namespace) WHERE ns.UID = "namespace_{{ $namespace }}" MATCH (node) WHERE node.Namespace = ns.Name AND NOT(()-[]->(node)) MERGE (node)-[:Namespace]->(ns);
				{{- end }}
			{{- end }}
		{{- end }}
		:commit
		`, "\t\t", "", -1)

	graphvizTemplate = strings.Replace(
		`digraph {
		    graph [rankdir="LR" bgcolor="#F6F6F6"];
		    node [shape="record" color="#D3D3D3" style="filled" fillcolor="#FFFFFF"];
		    edge [color="#4284F3"];
		    compound=true;
		{{ range $cluster, $namespaces := .Nodes }}
		    // create cluster
		    subgraph "cluster_{{ $cluster }}" {
		        label="{{ $cluster }}";
		        bgcolor="#ECEFF1";

		    {{- range $namespace, $node := $namespaces }}
		      {{ if $namespace }}
		        // create namespace
		        subgraph "cluster_namespace_{{ $namespace }}" {
		          label="{{ $namespace }}";
		          bgcolor="#E3F2FD";

		          "{{ $namespace }}" [label="Namespace\l | { {{ $namespace }}\l }" shape="point" style="invis"]
		          {{- range . }}
		          "{{ .UID }}" [label="{{ .Kind }}\l | { {{ .Name }}\l }"];
		          {{- end }}
		        }
		      {{- else }}
		        // create nodes
		        {{- range . }}
		        "{{ .UID }}" [label="{{ .Kind }}\l | { {{ .Name }}\l }"];
		        {{- end }}
		      {{- end }}
		    {{- end }}
		    }
		{{- end }}

		    // create relationships
		{{- range .Relationships }}
		  {{- range . }}
		    "{{ .From.UID }}" -> "{{ .To.UID }}"{{ if .Attr }} [{{ .Attr }}]{{ end }};
		  {{- end }}
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
	Nodes         map[string]map[string]map[types.UID]*Node
	Relationships map[types.UID][]*Relationship

	clientset *kubernetes.Clientset

	coreV1       *CoreV1Graph
	networkingV1 *NetworkingV1Graph
}

// Node represents a node in the graph.
type Node v1.ObjectReference

// Relationship represents a relationship between nodes in the graph.
type Relationship struct {
	From v1.ObjectReference
	Type string
	To   v1.ObjectReference
	Attr Attributes
}

// Attributes is a map of key:value.
type Attributes map[string]string

// String returns all fields listed as a Graphviz attr_list string.
func (attr Attributes) String() string {
	selector := make([]string, 0, len(attr))
	for key, value := range attr {
		selector = append(selector, fmt.Sprintf("%s=\"%s\"", key, value))
	}

	sort.StringSlice(selector).Sort()
	return strings.Join(selector, " ")
}

// ToUID converts strings to MD5 and returns this as types.UID.
func ToUID(input ...string) types.UID {
	bytes := []byte(strings.Join(input, "-"))
	md5sum := fmt.Sprintf("%x", md5.Sum(bytes))

	slice := []string{
		md5sum[:8],
		md5sum[8:12],
		md5sum[12:16],
		md5sum[16:20],
		md5sum[20:],
	}

	return types.UID(strings.Join(slice, "-"))
}

// FromUnstructured converts an unstructured object into a concrete type.
func FromUnstructured(unstr *unstructured.Unstructured, obj interface{}) error {
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.UnstructuredContent(), obj)
	if err != nil {
		return fmt.Errorf("Failed to convert %T to %T: %v", unstr, obj, err)
	}

	return nil
}

// NewGraph returns a new initialized a Graph.
func NewGraph(clientset *kubernetes.Clientset, objs []*unstructured.Unstructured) (*Graph, error) {
	g := &Graph{
		clientset:     clientset,
		Nodes:         make(map[string]map[string]map[types.UID]*Node),
		Relationships: make(map[types.UID][]*Relationship),
	}

	g.coreV1 = NewCoreV1Graph(g)
	g.networkingV1 = NewNetworkingV1Graph(g)

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
func (g *Graph) Unstructured(unstr *unstructured.Unstructured) (err error) {
	g.Node(unstr.GroupVersionKind(), unstr)

	switch unstr.GetAPIVersion() {
	case "v1":
		return g.CoreV1().Unstructured(unstr)
	case "networking.k8s.io/v1":
		return g.NetworkingV1().Unstructured(unstr)
	}

	return err
}

// Node adds a node and the owner references to the Graph.
func (g *Graph) Node(gvk schema.GroupVersionKind, obj metav1.Object) *Node {
	if obj.GetClusterName() == "" {
		obj.SetClusterName(g.clientset.RESTClient().Get().URL().Hostname())
	}
	if g.Nodes[obj.GetClusterName()] == nil {
		g.Nodes[obj.GetClusterName()] = make(map[string]map[types.UID]*Node)
	}
	if g.Nodes[obj.GetClusterName()][obj.GetNamespace()] == nil {
		g.Nodes[obj.GetClusterName()][obj.GetNamespace()] = make(map[types.UID]*Node)
	}

	apiVersion, kind := gvk.ToAPIVersionAndKind()
	node := &Node{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		UID:        obj.GetUID(),
	}
	if gvk.GroupKind().String() == "Namespace" {
		return node
	}

	g.Nodes[obj.GetClusterName()][obj.GetNamespace()][obj.GetUID()] = node

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
	if rs, ok := g.Relationships[from.UID]; ok {
		for _, r := range rs {
			if r.To.UID == to.UID {
				return r
			}
		}
	}

	relationship := &Relationship{
		From: v1.ObjectReference{UID: from.UID, Kind: from.Kind},
		Type: label,
		To:   v1.ObjectReference{UID: to.UID, Kind: to.Kind},
		Attr: make(Attributes),
	}
	g.Relationships[from.UID] = append(g.Relationships[from.UID], relationship)

	return relationship
}

// Attribute adds an attribute which is rendered in the Graphviz output format.
func (r *Relationship) Attribute(key string, value string) {
	r.Attr[key] = value
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
