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
	"fmt"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	case "Ingress":
		obj := &v1.Ingress{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.Ingress(obj)
	case "NetworkPolicy":
		obj := &v1.NetworkPolicy{}
		if err = FromUnstructured(unstr, obj); err != nil {
			return err
		}
		_, err = g.NetworkPolicy(obj)
	}

	return err
}

// Relationship creates a new relationship between two nodes based on v1.PolicyType.
func (g *NetworkingV1Graph) Relationship(from *Node, policyType v1.PolicyType, to *Node) (r *Relationship) {
	switch policyType {
	case v1.PolicyTypeIngress:
		r = g.graph.Relationship(to, string(policyType), from)
		r.Attribute("color", "#34A853")
	case v1.PolicyTypeEgress:
		r = g.graph.Relationship(from, string(policyType), to)
		r.Attribute("color", "#EA4335")
	}

	return r.Attribute("style", "dashed")
}

// Ingress adds a v1.Ingress resource to the Graph.
func (g *NetworkingV1Graph) Ingress(obj *v1.Ingress) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	for _, rule := range obj.Spec.Rules {
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				b, err := g.IngressBackend(obj, path.Backend)
				if err != nil {
					return nil, err
				}
				g.Relationship(b, v1.PolicyTypeIngress, n)
			}
		}

		h, err := g.Host(rule.Host)
		if err != nil {
			return nil, err
		}
		g.Relationship(n, v1.PolicyTypeIngress, h)
	}

	return n, nil
}

// IngressBackend adds a v1.IngressBackend resource to the Graph.
func (g *NetworkingV1Graph) IngressBackend(obj *v1.Ingress, backend v1.IngressBackend) (*Node, error) {
	switch {
	case backend.Service != nil:
		options := metav1.GetOptions{}
		service, err := g.graph.clientset.CoreV1().Services(obj.GetNamespace()).Get(context.TODO(), backend.Service.Name, options)
		if err != nil {
			return nil, err
		}

		return g.graph.CoreV1().Service(service)
	case backend.Resource != nil:
		return g.graph.CoreV1().TypedLocalObjectReference(backend.Resource, obj.GetNamespace())
	}

	return nil, fmt.Errorf("%v: backend is not supported yet", obj.GroupVersionKind())
}

// Host adds a v1.Host resource to the Graph.
func (g *NetworkingV1Graph) Host(name string) (*Node, error) {
	n := g.graph.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "Host"),
		&metav1.ObjectMeta{
			ClusterName: "External",
			UID:         ToUID(name),
			Name:        name,
		},
	)

	return n, nil
}

// NetworkPolicy adds a v1.NetworkPolicy resource to the Graph.
func (g *NetworkingV1Graph) NetworkPolicy(obj *v1.NetworkPolicy) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	selector, err := metav1.LabelSelectorAsSelector(&obj.Spec.PodSelector)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{LabelSelector: selector.String(), FieldSelector: "status.phase=Running"}
	pods, err := g.graph.clientset.CoreV1().Pods(obj.GetNamespace()).List(context.TODO(), options)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		p, err := g.graph.CoreV1().Pod(&pod)
		if err != nil {
			return nil, err
		}
		if len(obj.Spec.Ingress) != 0 {
			g.Relationship(p, v1.PolicyTypeIngress, n)
		}
		if len(obj.Spec.Egress) != 0 {
			g.Relationship(p, v1.PolicyTypeEgress, n)
		}
	}

	for _, rule := range obj.Spec.Ingress {
		if len(rule.From) == 0 {
			rule.From = append(rule.From, v1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{}})
		}
		for _, peer := range rule.From {
			_, err := g.NetworkPolicyPeer(obj, v1.PolicyTypeIngress, peer)
			if err != nil {
				return nil, err
			}
		}
	}

	for _, rule := range obj.Spec.Egress {
		if len(rule.To) == 0 {
			rule.To = append(rule.To, v1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{}})
		}
		for _, peer := range rule.To {
			_, err := g.NetworkPolicyPeer(obj, v1.PolicyTypeEgress, peer)
			if err != nil {
				return nil, err
			}
		}
	}

	return n, nil
}

// NetworkPolicyPeer adds a v1.NetworkPolicyPeer resource to the Graph.
func (g *NetworkingV1Graph) NetworkPolicyPeer(obj *v1.NetworkPolicy, policyType v1.PolicyType, peer v1.NetworkPolicyPeer) (*Node, error) {
	switch {
	case peer.NamespaceSelector != nil && peer.PodSelector != nil:
		return g.NetworkPolicyPeerNamespaceAndPodSelector(obj, policyType, peer)
	case peer.NamespaceSelector != nil:
		return g.NetworkPolicyPeerNamespaceSelector(obj, policyType, peer)
	case peer.PodSelector != nil:
		return g.NetworkPolicyPeerPodSelector(obj, policyType, peer)
	case peer.IPBlock != nil:
		return g.NetworkPolicyPeerIPBlock(obj, policyType, peer)
	}

	return nil, nil
}

// NetworkPolicyPeerNamespaceAndPodSelector adds a v1.NetworkPolicyPeer of type NamespaceAndPodSelector to the Graph.
func (g *NetworkingV1Graph) NetworkPolicyPeerNamespaceAndPodSelector(obj *v1.NetworkPolicy, policyType v1.PolicyType, peer v1.NetworkPolicyPeer) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	selector, err := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{LabelSelector: selector.String()}
	namespaces, err := g.graph.clientset.CoreV1().Namespaces().List(context.TODO(), options)
	if err != nil {
		return nil, err
	}

	for _, namespace := range namespaces.Items {
		selector, err := metav1.LabelSelectorAsSelector(peer.PodSelector)
		if err != nil {
			return nil, err
		}

		options := metav1.ListOptions{LabelSelector: selector.String(), FieldSelector: "status.phase=Running"}
		pods, err := g.graph.clientset.CoreV1().Pods(namespace.GetName()).List(context.TODO(), options)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods.Items {
			p, err := g.graph.CoreV1().Pod(&pod)
			if err != nil {
				return nil, err
			}
			g.Relationship(n, policyType, p)
		}
	}

	return n, nil
}

// NetworkPolicyPeerNamespaceSelector adds a v1.NetworkPolicyPeer of type NamespaceSelector to the Graph.
func (g *NetworkingV1Graph) NetworkPolicyPeerNamespaceSelector(obj *v1.NetworkPolicy, policyType v1.PolicyType, peer v1.NetworkPolicyPeer) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	selector, err := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{LabelSelector: selector.String()}
	namespaces, err := g.graph.clientset.CoreV1().Namespaces().List(context.TODO(), options)
	if err != nil {
		return nil, err
	}

	for _, namespace := range namespaces.Items {
		ns, err := g.graph.CoreV1().Namespace(&namespace)
		if err != nil {
			return nil, err
		}
		g.Relationship(n, policyType, ns)
	}

	return n, nil
}

// NetworkPolicyPeerPodSelector adds a v1.NetworkPolicyPeer of type PodSelector to the Graph.
func (g *NetworkingV1Graph) NetworkPolicyPeerPodSelector(obj *v1.NetworkPolicy, policyType v1.PolicyType, peer v1.NetworkPolicyPeer) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	selector, err := metav1.LabelSelectorAsSelector(peer.PodSelector)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{LabelSelector: selector.String(), FieldSelector: "status.phase=Running"}
	pods, err := g.graph.clientset.CoreV1().Pods(obj.GetNamespace()).List(context.TODO(), options)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		p, err := g.graph.CoreV1().Pod(&pod)
		if err != nil {
			return nil, err
		}
		g.Relationship(n, policyType, p)
	}

	return n, nil
}

// NetworkPolicyPeerIPBlock adds a v1.NetworkPolicyPeer of type IPBlock to the Graph.
func (g *NetworkingV1Graph) NetworkPolicyPeerIPBlock(obj *v1.NetworkPolicy, policyType v1.PolicyType, peer v1.NetworkPolicyPeer) (*Node, error) {
	n := g.graph.Node(obj.GroupVersionKind(), obj)

	i, err := g.IPBlock(peer.IPBlock.CIDR)
	if err != nil {
		return nil, err
	}
	g.Relationship(n, policyType, i)

	return n, nil
}

// IPBlock adds a v1.IPBlock resource to the Graph.
func (g *NetworkingV1Graph) IPBlock(cidr string) (*Node, error) {
	n := g.graph.Node(
		schema.FromAPIVersionAndKind(v1.GroupName, "IPBlock"),
		&metav1.ObjectMeta{
			ClusterName: "External",
			UID:         ToUID(cidr),
			Name:        cidr,
		},
	)

	return n, nil
}
