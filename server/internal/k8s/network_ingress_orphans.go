package k8s

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NetworkIngressOrphan identifies a labelled resource without persisted ownership.
// Names, manifests and Secret data are intentionally excluded from the result.
type NetworkIngressOrphan struct {
	// OwnerID is the validated ingress UUID on the resource.
	OwnerID uuid.UUID

	// Kind is one of the bounded resource kinds managed by this provider.
	Kind string
}

// FindOrphans inventories provider-owned resources without modifying them.
func (r *NetworkIngressProvisionerRegistry) FindOrphans(ctx context.Context, known map[string][]NetworkIngressResourceNames) ([]NetworkIngressOrphan, error) {
	var result []NetworkIngressOrphan
	for provider, wrapped := range r.providers {
		observed, ok := wrapped.(*observedNetworkIngressProvisioner)
		if !ok {
			continue
		}
		finder, ok := observed.provisioner.(interface {
			FindOrphans(context.Context, []NetworkIngressResourceNames) ([]NetworkIngressOrphan, error)
		})
		if !ok {
			continue
		}
		orphans, err := finder.FindOrphans(ctx, known[provider])
		if err != nil {
			return nil, fmt.Errorf("inventory network ingress resources: %w", err)
		}
		result = append(result, orphans...)
	}
	return result, nil
}

func (p *TailscaleNetworkIngressProvisioner) FindOrphans(ctx context.Context, known []NetworkIngressResourceNames) ([]NetworkIngressOrphan, error) {
	type resourceKey struct{ kind, namespace, name string }
	expected := make(map[resourceKey]uuid.UUID)
	for _, names := range known {
		if err := names.Validate(); err != nil {
			return nil, fmt.Errorf("invalid persisted identity during orphan scan")
		}
		for key, name := range map[resourceKey]string{
			{kind: "namespaces", namespace: "", name: ""}:                              names.Namespace,
			{kind: "tailnets", namespace: "", name: ""}:                                names.Tailnet,
			{kind: "proxygroups", namespace: "", name: ""}:                             names.ProxyGroup,
			{kind: "proxygrouppolicies", namespace: names.Namespace, name: ""}:         names.ProxyGroupPolicy,
			{kind: "deployments", namespace: names.Namespace, name: ""}:                names.AttestorDeployment,
			{kind: "services", namespace: names.Namespace, name: ""}:                   names.AttestorService,
			{kind: "serviceaccounts", namespace: names.Namespace, name: ""}:            names.AttestorServiceAccount,
			{kind: "ingresses", namespace: names.Namespace, name: ""}:                  names.Ingress,
			{kind: "secrets", namespace: names.Namespace, name: ""}:                    names.AttestorCASecret,
			{kind: "secrets", namespace: p.config.OperatorNamespace, name: ""}:         names.CredentialsSecret,
			{kind: "networkpolicies", namespace: names.Namespace, name: ""}:            names.AttestorNetworkPolicy,
			{kind: "networkpolicies", namespace: p.config.OperatorNamespace, name: ""}: names.ProxyNetworkPolicy,
		} {
			key.name = name
			expected[key] = names.OwnerID
		}
	}
	resources := []struct {
		gvr        schema.GroupVersionResource
		namespaced bool
	}{
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, false},
		{tailnetGVR, false}, {proxyGroupGVR, false}, {proxyGroupPolicyGVR, true},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true},
		{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true},
		{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, true},
	}
	var orphans []NetworkIngressOrphan
	selector := labels.Set{managedByLabelKey: networkIngressManagedBy}.String()
	for _, resource := range resources {
		var continuation string
		for range 10 {
			client := p.dynamic.Resource(resource.gvr)
			options := metav1.ListOptions{LabelSelector: selector, Limit: 100, Continue: continuation}
			// List remains inside the provider boundary. Only owner IDs and kinds
			// escape it, never Secret payloads or untrusted Kubernetes errors.
			list, err := client.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("network ingress orphan inventory unavailable")
			}
			for _, object := range list.Items {
				owner, err := uuid.Parse(object.GetLabels()[networkIngressIDLabel])
				if err != nil {
					owner = uuid.Nil
				}
				key := resourceKey{kind: resource.gvr.Resource, namespace: object.GetNamespace(), name: object.GetName()}
				if resource.namespaced && key.namespace == "" {
					continue
				}
				if expectedOwner, ok := expected[key]; !ok || owner != expectedOwner {
					orphans = append(orphans, NetworkIngressOrphan{OwnerID: owner, Kind: resource.gvr.Resource})
				}
			}
			continuation = list.GetContinue()
			if continuation == "" {
				break
			}
		}
		if continuation != "" {
			return nil, fmt.Errorf("network ingress orphan inventory limit exceeded")
		}
	}
	return orphans, nil
}
