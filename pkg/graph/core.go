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
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	case "Node":
		obj := &v1.Node{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Node(obj)
	}

	return err
}

// Pod adds a v1.Pod resource to the Graph.
func (g *CoreV1Graph) Pod(pod *v1.Pod) (*Node, error) {
	n := g.graph.Node(schema.FromAPIVersionAndKind(v1.GroupName, "Pod"), pod)

	for _, container := range pod.Spec.Containers {
		c, err := g.Container(pod, container)
		if err != nil {
			return nil, err
		}
		g.graph.Relationship(n, "Container", c)
	}

	return n, nil
}

// Container adds a v1.Container resource to the Graph.
func (g *CoreV1Graph) Container(pod *v1.Pod, container v1.Container) (*Node, error) {
	n := g.graph.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "Container"),
		&metav1.ObjectMeta{
			UID:       ToUID(string(pod.GetUID()), container.Name),
			Namespace: pod.GetNamespace(),
			Name:      container.Name,
		},
	)

	// i, err := g.Image(container.Image)
	// if err != nil {
	// 	return nil, err
	// }
	// g.graph.Relationship(n, "Image", i)

	return n, nil
}

// Image adds a v1.Image resource to the Graph.
func (g *CoreV1Graph) Image(name string) (*Node, error) {
	registry := "docker.io"
	image := name

	if strings.Count(image, "/") > 0 {
		s := strings.SplitN(image, "/", 2)
		if strings.Count(s[0], ".") > 0 {
			registry, image = s[0], s[1]
		}
	}

	n := g.graph.Node(
		schema.FromAPIVersionAndKind("kubectl-graph/v1", "Image"),
		&metav1.ObjectMeta{
			UID:  ToUID(registry, image),
			Name: image,
		},
	)

	r, err := g.Registry(registry)
	if err != nil {
		return nil, err
	}
	g.graph.Relationship(n, "Registry", r)

	return n, nil
}

// Registry adds a v1.Registry resource to the Graph.
func (g *CoreV1Graph) Registry(name string) (*Node, error) {
	n := g.graph.Node(
		schema.FromAPIVersionAndKind("kubectl-graph/v1", "Registry"),
		&metav1.ObjectMeta{
			UID:  ToUID(name),
			Name: name,
		},
	)

	return n, nil
}

// Endpoints adds a v1.Endpoints resource to the Graph.
func (g *CoreV1Graph) Endpoints(obj *v1.Endpoints) (*Node, error) {
	n := g.graph.Node(schema.FromAPIVersionAndKind(v1.GroupName, "Endpoints"), obj)

	for _, subset := range obj.Subsets {
		for _, address := range subset.Addresses {
			t := g.graph.Node(
				address.TargetRef.GroupVersionKind(),
				&metav1.ObjectMeta{
					UID:       address.TargetRef.UID,
					Name:      address.TargetRef.Name,
					Namespace: address.TargetRef.Namespace,
				},
			)
			g.graph.Relationship(n, address.TargetRef.Kind, t)
		}
	}

	return n, nil
}

// Service adds a v1.Service resource to the Graph.
func (g *CoreV1Graph) Service(obj *v1.Service) (*Node, error) {
	switch obj.Spec.Type {
	case v1.ServiceTypeClusterIP:
		return g.ServiceTypeClusterIP(obj)
		// case v1.ServiceTypeNodePort:
	case v1.ServiceTypeLoadBalancer:
		return g.ServiceTypeLoadBalancer(obj)
	case v1.ServiceTypeExternalName:
		return g.ServiceTypeExternalName(obj)
	}

	return nil, nil
}

// ServiceTypeClusterIP adds a v1.Service of type ClusterIP to the Graph.
func (g *CoreV1Graph) ServiceTypeClusterIP(obj *v1.Service) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	options := metav1.GetOptions{}
	endpoints, err := g.graph.clientset.CoreV1().Endpoints(obj.GetNamespace()).Get(context.TODO(), obj.GetName(), options)
	if err != nil {
		return nil, err
	}

	e, err := g.Endpoints(endpoints)
	if err != nil {
		return nil, err
	}
	g.graph.Relationship(n, "Endpoints", e)

	return n, nil
}

// ServiceTypeLoadBalancer adds a v1.Service of type LoadBalancer to the Graph.
func (g *CoreV1Graph) ServiceTypeLoadBalancer(obj *v1.Service) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	options := metav1.GetOptions{}
	endpoints, err := g.graph.clientset.CoreV1().Endpoints(obj.GetNamespace()).Get(context.TODO(), obj.GetName(), options)
	if err != nil {
		return nil, err
	}

	e, err := g.Endpoints(endpoints)
	if err != nil {
		return nil, err
	}
	g.graph.Relationship(n, "Endpoints", e)

	return n, nil
}

// ServiceTypeExternalName adds a v1.Service of type ExternalName to the Graph.
func (g *CoreV1Graph) ServiceTypeExternalName(obj *v1.Service) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	e := g.graph.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "ExternalName"),
		&metav1.ObjectMeta{
			UID:  ToUID(obj.Spec.ExternalName),
			Name: obj.Spec.ExternalName,
		},
	)
	g.graph.Relationship(n, "ExternalName", e)

	return n, nil
}

// Node adds a v1.Node resource to the Graph.
func (g *CoreV1Graph) Node(obj *v1.Node) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	infos := map[string]string{
		"Architecture": obj.Status.NodeInfo.Architecture,
		"Runtime":      obj.Status.NodeInfo.ContainerRuntimeVersion,
		"Kernel":       obj.Status.NodeInfo.KernelVersion,
		"OSImage":      obj.Status.NodeInfo.OSImage,
	}
	for kind, info := range infos {
		i := g.graph.Node(
			schema.FromAPIVersionAndKind("kubectl-graph/v1", kind),
			&metav1.ObjectMeta{
				UID:  ToUID(info),
				Name: info,
			},
		)
		g.graph.Relationship(n, kind, i)
	}

	return n, nil
}
