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
		{{- range $namespace, $node := .Nodes }}
			{{- if $namespace }}
		MERGE (node:Namespace {UID: "namespace_{{ $namespace }}"}) ON CREATE SET node.Name = "{{ $namespace }}";
			{{- end }}
			{{- range . }}
		MERGE (node:{{ .Kind }} {UID: "{{ .UID }}"}) ON CREATE SET node.Namespace = "{{ .Namespace }}", node.Name = "{{ .Name }}";
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
		{{- range $namespace, $node := .Nodes }}
			{{- if $namespace }}
		MATCH (ns:Namespace) WHERE ns.Name = "{{ $namespace }}" MATCH (node) WHERE node.Namespace = ns.Name AND NOT(()-[]->(node)) MERGE (node)-[:Namespace]->(ns);
			{{- end }}
		{{- end }}
		:commit
		`, "\t\t", "", -1)

	graphvizTemplate = strings.Replace(
		`digraph {
		    graph [rankdir="LR" bgcolor="#F6F6F6"];
		    node [shape="record" color="lightgray"];
		    edge [color="#4284F3"];

		{{- range $namespace, $node := .Nodes }}
		  {{ if $namespace }}
		    // create subgraph
		    subgraph "cluster_{{ $namespace }}" {
		      label="{{ $namespace }}";
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

		    // create relationships
		{{- range .Relationships }}
		  {{- range . }}
		    "{{ .From.UID }}" -> "{{ .To.UID }}";
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
	Nodes         map[string]map[types.UID]*Node
	Relationships map[types.UID][]*Relationship

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
		Nodes:         make(map[string]map[types.UID]*Node),
		Relationships: make(map[types.UID][]*Relationship),
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
func (g *Graph) Unstructured(unstr *unstructured.Unstructured) (err error) {
	g.Node(unstr.GroupVersionKind(), unstr)

	switch unstr.GetKind() {
	case "Pod":
		obj := &v1.Pod{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Pod(obj)
	case "Endpoints":
		obj := &v1.Endpoints{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Endpoints(obj)
	case "Service":
		obj := &v1.Service{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Service(obj)
	}

	return err
}

// Node adds a node and the owner references to the Graph.
func (g *Graph) Node(gvk schema.GroupVersionKind, obj metav1.Object) *Node {
	if g.Nodes[obj.GetNamespace()] == nil {
		g.Nodes[obj.GetNamespace()] = make(map[types.UID]*Node)
	}

	apiVersion, kind := gvk.ToAPIVersionAndKind()
	node := &Node{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		UID:        obj.GetUID(),
	}
	g.Nodes[obj.GetNamespace()][obj.GetUID()] = node

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
	}
	g.Relationships[from.UID] = append(g.Relationships[from.UID], relationship)

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

// Pod adds a v1.Pod resource to the Graph.
func (g *Graph) Pod(pod *v1.Pod) (*Node, error) {
	n := g.Node(pod.GroupVersionKind(), pod)

	for _, container := range pod.Spec.Containers {
		c, err := g.Container(pod, container)
		if err != nil {
			return nil, err
		}
		g.Relationship(n, "Container", c)
	}

	return n, nil
}

// Container adds a v1.Container resource to the Graph.
func (g *Graph) Container(pod *v1.Pod, container v1.Container) (*Node, error) {
	n := g.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "Container"),
		&metav1.ObjectMeta{
			UID:       ToUID(string(pod.GetUID()), container.Name),
			Namespace: pod.GetNamespace(),
			Name:      container.Name,
		},
	)
	i, err := g.ContainerImage(v1.ContainerImage{Names: []string{container.Image}})
	if err != nil {
		return nil, err
	}
	g.Relationship(n, "ContainerImage", i)

	return n, nil
}

// ContainerImage adds a v1.ContainerImage resource to the Graph.
func (g *Graph) ContainerImage(image v1.ContainerImage) (*Node, error) {
	n := g.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "ContainerImage"),
		&metav1.ObjectMeta{
			UID:  ToUID(strings.Join(image.Names, "-")),
			Name: strings.Join(image.Names, ","),
		},
	)

	return n, nil
}

// Endpoints adds a v1.Endpoints resource to the Graph.
func (g *Graph) Endpoints(obj *v1.Endpoints) (*Node, error) {
	n := g.Node(schema.FromAPIVersionAndKind(v1.GroupName, "Endpoints"), obj)

	for _, subset := range obj.Subsets {
		for _, address := range subset.Addresses {
			t := g.Node(
				address.TargetRef.GroupVersionKind(),
				&metav1.ObjectMeta{
					UID:       address.TargetRef.UID,
					Name:      address.TargetRef.Name,
					Namespace: address.TargetRef.Namespace,
				},
			)
			g.Relationship(n, address.TargetRef.Kind, t)
		}
	}

	return n, nil
}

// Service adds a v1.Service resource to the Graph.
func (g *Graph) Service(obj *v1.Service) (*Node, error) {
	switch obj.Spec.Type {
	case v1.ServiceTypeClusterIP:
		return g.ServiceTypeClusterIP(obj)
		// case v1.ServiceTypeNodePort:
		// case v1.ServiceTypeLoadBalancer:
	case v1.ServiceTypeExternalName:
		return g.ServiceTypeExternalName(obj)
	}

	return nil, nil
}

// ServiceTypeClusterIP adds a v1.Service of type ClusterIP to the Graph.
func (g *Graph) ServiceTypeClusterIP(obj *v1.Service) (*Node, error) {
	n := g.Node(obj.GroupVersionKind(), obj)

	options := metav1.GetOptions{}
	endpoints, err := g.clientset.CoreV1().Endpoints(obj.GetNamespace()).Get(obj.GetName(), options)
	if err != nil {
		return nil, err
	}

	e, err := g.Endpoints(endpoints)
	if err != nil {
		return nil, err
	}
	g.Relationship(n, "Endpoints", e)

	return n, nil
}

// ServiceTypeExternalName adds a v1.Service of type ExternalName to the Graph.
func (g *Graph) ServiceTypeExternalName(obj *v1.Service) (*Node, error) {
	n := g.Node(obj.GroupVersionKind(), obj)

	e := g.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "ExternalName"),
		&metav1.ObjectMeta{
			UID:  ToUID(obj.Spec.ExternalName),
			Name: obj.Spec.ExternalName,
		},
	)
	g.Relationship(n, "ExternalName", e)

	return n, nil
}
