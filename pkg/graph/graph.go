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
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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
	"sigs.k8s.io/yaml"
)

var (
	//go:embed templates/*.tmpl
	templateFiles embed.FS
	templates     *template.Template
)

func init() {
	templates = template.New("output").Funcs(template.FuncMap{
		"json": func(i interface{}) string {
			b, err := json.Marshal(i)
			if err != nil {
				return err.Error()
			}
			return string(b)
		},
		"yaml": func(i interface{}) string {
			b, err := yaml.Marshal(i)
			if err != nil {
				return err.Error()
			}
			return strings.Trim(string(b), "\n")
		},
		"underscore": func(s string) string {
			re := regexp.MustCompile(`[^A-Za-z0-9]+`)
			return re.ReplaceAllString(strings.ToLower(s), "_")
		},
		"color": func(s string) string {
			hash := md5.Sum([]byte(s))
			return fmt.Sprintf("#%x", hash[:3])
		},
	})

	template.Must(templates.ParseFS(templateFiles, "templates/*.tmpl"))
}

// Graph stores nodes and relationships between them.
type Graph struct {
	Nodes         map[types.UID]*Node
	Relationships map[types.UID][]*Relationship

	clientset *kubernetes.Clientset

	coreV1       *CoreV1Graph
	networkingV1 *NetworkingV1Graph
	routeV1      *RouteV1Graph
}

// Node represents a node in the graph.
type Node struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

// Relationship represents a relationship between nodes in the graph.
type Relationship struct {
	From  types.UID
	Label string
	To    types.UID
	Attr  map[string]string
}

// ToUID converts all params to MD5 and returns this as types.UID.
func ToUID(params ...interface{}) types.UID {
	input := make([]string, len(params))
	for _, param := range params {
		input = append(input, fmt.Sprint(param))
	}

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

// FilterByValue filters a key value map by value using a function.
func FilterByValue(kv map[string]string, f func(string) bool) map[string]string {
	filtered := make(map[string]string, 0)
	for key, value := range kv {
		if f(value) {
			filtered[key] = value
		}
	}

	return filtered
}

// FromUnstructured converts an unstructured object into a concrete type.
func FromUnstructured(unstr *unstructured.Unstructured, obj runtime.Object) error {
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.UnstructuredContent(), obj)
	if err != nil {
		return fmt.Errorf("failed to convert %T to %T: %v", unstr, obj, err)
	}

	return nil
}

// NewGraph returns a new initialized a Graph.
func NewGraph(clientset *kubernetes.Clientset, objs []*unstructured.Unstructured, processed func()) (*Graph, error) {
	g := &Graph{
		clientset:     clientset,
		Nodes:         make(map[types.UID]*Node),
		Relationships: make(map[types.UID][]*Relationship),
	}

	g.coreV1 = NewCoreV1Graph(g)
	g.networkingV1 = NewNetworkingV1Graph(g)
	g.routeV1 = NewRouteV1Graph(g)

	errs := []error{}

	for _, obj := range objs {
		_, err := g.Unstructured(obj)
		if err != nil {
			errs = append(errs, err)
		}
		processed()
	}

	err := g.Finalize()
	if err != nil {
		errs = append(errs, err)
	}

	return g, errors.NewAggregate(errs)
}

// Unstructured adds an unstructured node to the Graph.
func (g *Graph) Unstructured(unstr *unstructured.Unstructured) (*Node, error) {
	switch unstr.GetAPIVersion() {
	case "v1":
		return g.CoreV1().Unstructured(unstr)
	case "networking.k8s.io/v1":
		return g.NetworkingV1().Unstructured(unstr)
	case "route.openshift.io/v1":
		return g.RouteV1().Unstructured(unstr)
	default:
		return g.Node(unstr.GroupVersionKind(), unstr), nil
	}
}

// Node adds a node and the owner references to the Graph.
func (g *Graph) Node(gvk schema.GroupVersionKind, obj metav1.Object) *Node {
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	node := &Node{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			UID:         obj.GetUID(),
			Namespace:   obj.GetNamespace(),
			Name:        obj.GetName(),
			Annotations: FilterByValue(obj.GetAnnotations(), func(v string) bool {
				return !strings.HasPrefix(v, "{") && !strings.HasPrefix(v, "[")
			}),
			Labels:      obj.GetLabels(),
		},
	}

	if n, ok := g.Nodes[obj.GetUID()]; ok {
		if len(n.GetAnnotations()) != 0 {
			node.SetAnnotations(n.GetAnnotations())
		}
		if len(n.GetLabels()) != 0 {
			node.SetLabels(n.GetLabels())
		}
	}

	g.Nodes[obj.GetUID()] = node

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

// Finalize adds missing relationships to the Graph.
func (g *Graph) Finalize() error {
	for _, node := range g.Nodes {
		if node.Kind == "Cluster" || node.Kind == "Namespace" {
			continue
		}

		if _, ok := g.Relationships[node.UID]; ok {
			continue
		}

		if len(node.GetNamespace()) == 0 {
			cluster, err := g.CoreV1().Cluster()
			if err != nil {
				return err
			}

			g.Relationship(cluster, node.Kind, node)
			continue
		}

		metadata := metav1.ObjectMeta{Name: node.GetNamespace()}
		namespace, err := g.CoreV1().Namespace(&v1.Namespace{ObjectMeta: metadata})
		if err != nil {
			return err
		}
		g.Relationship(namespace, node.Kind, node)
	}

	return nil
}

// NodeList returns a list of all nodes.
func (g *Graph) NodeList() []*Node {
	nodes := []*Node{}

	for _, node := range g.Nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// Relationship creates a new relationship between two nodes.
func (g *Graph) Relationship(from *Node, label string, to *Node) *Relationship {
	if rs, ok := g.Relationships[to.GetUID()]; ok {
		for _, r := range rs {
			if r.From == from.GetUID() {
				return r
			}
		}
	}

	relationship := &Relationship{
		From:  from.GetUID(),
		Label: label,
		To:    to.GetUID(),
		Attr:  make(map[string]string),
	}
	g.Relationships[to.GetUID()] = append(g.Relationships[to.GetUID()], relationship)

	return relationship
}

// RelationshipList returns a list of all relationships.
func (g *Graph) RelationshipList() []*Relationship {
	relationships := []*Relationship{}

	for _, relationship := range g.Relationships {
		relationships = append(relationships, relationship...)
	}

	return relationships
}

// Attribute adds an attribute to a relationship.
func (r *Relationship) Attribute(key string, value string) *Relationship {
	r.Attr[key] = value
	return r
}

// String returns the graph in requested format.
func (g *Graph) String(format string) string {
	b := &bytes.Buffer{}
	g.Write(b, format)

	return b.String()
}

// Write formats according to the requested format and writes to w.
func (g *Graph) Write(w io.Writer, format string) error {
	return templates.ExecuteTemplate(w, format+".tmpl", g)
}
