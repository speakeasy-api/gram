package k8s

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
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

//nolint:exhaustruct // Internal identity keys intentionally build up namespace/name fields in stages.
func (p *TailscaleNetworkIngressProvisioner) FindOrphans(ctx context.Context, known []NetworkIngressResourceNames) ([]NetworkIngressOrphan, error) {
	type resourceKey struct{ kind, namespace, name string }
	expected := make(map[resourceKey]uuid.UUID)
	for _, names := range known {
		if err := names.Validate(); err != nil {
			return nil, fmt.Errorf("invalid persisted identity during orphan scan")
		}
		for key, name := range map[resourceKey]string{
			{kind: "namespaces"}:  names.Namespace,
			{kind: "tailnets"}:    names.Tailnet,
			{kind: "proxygroups"}: names.ProxyGroup,
			{kind: "proxygrouppolicies", namespace: names.Namespace}:         names.ProxyGroupPolicy,
			{kind: "deployments", namespace: names.Namespace}:                names.AttestorDeployment,
			{kind: "services", namespace: names.Namespace}:                   names.AttestorService,
			{kind: "serviceaccounts", namespace: names.Namespace}:            names.AttestorServiceAccount,
			{kind: "ingresses", namespace: names.Namespace}:                  names.Ingress,
			{kind: "secrets", namespace: names.Namespace}:                    names.AttestorCASecret,
			{kind: "networkpolicies", namespace: names.Namespace}:            names.AttestorNetworkPolicy,
			{kind: "networkpolicies", namespace: p.config.OperatorNamespace}: names.ProxyNetworkPolicy,
			{kind: "rolebindings", namespace: names.Namespace}:               networkIngressAttestorManagerBinding,
		} {
			key.name = name
			expected[key] = names.OwnerID
		}
	}

	selector := labels.Set{managedByLabelKey: networkIngressManagedBy}.String()
	list := func(gvr schema.GroupVersionResource, namespace, ownerSelector, name string, visit func(metav1.Object)) error {
		var client dynamic.ResourceInterface = p.dynamic.Resource(gvr)
		if namespace != "" {
			client = p.dynamic.Resource(gvr).Namespace(namespace)
		}
		options := metav1.ListOptions{LabelSelector: ownerSelector, Limit: 100}
		if name != "" {
			options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
		}
		for range 10 {
			objects, err := client.List(ctx, options)
			if err != nil {
				return fmt.Errorf("network ingress orphan inventory unavailable")
			}
			for i := range objects.Items {
				visit(&objects.Items[i])
			}
			options.Continue = objects.GetContinue()
			if options.Continue == "" {
				return nil
			}
		}
		return fmt.Errorf("network ingress orphan inventory limit exceeded")
	}
	identity := func(object metav1.Object) (NetworkIngressResourceNames, bool) {
		objectLabels := object.GetLabels()
		if objectLabels[managedByLabelKey] != networkIngressManagedBy {
			return NetworkIngressResourceNames{}, false
		}
		owner, err := uuid.Parse(objectLabels[networkIngressIDLabel])
		if err != nil || owner == uuid.Nil {
			return NetworkIngressResourceNames{}, false
		}
		names, err := NewNetworkIngressResourceNames(owner)
		return names, err == nil
	}
	var orphans []NetworkIngressOrphan
	report := func(key resourceKey, owner uuid.UUID) {
		if expectedOwner, ok := expected[key]; !ok || expectedOwner != owner {
			orphans = append(orphans, NetworkIngressOrphan{OwnerID: owner, Kind: key.kind})
		}
	}

	namespaces := make(map[string]NetworkIngressResourceNames)
	namespaceGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	if err := list(namespaceGVR, "", selector, "", func(object metav1.Object) {
		names, ok := identity(object)
		if !ok || object.GetName() != names.Namespace {
			return
		}
		namespaces[names.Namespace] = names
		report(resourceKey{kind: "namespaces", name: names.Namespace}, names.OwnerID)
	}); err != nil {
		return nil, err
	}

	for _, resource := range []struct {
		gvr  schema.GroupVersionResource
		name func(NetworkIngressResourceNames) string
	}{
		{tailnetGVR, func(names NetworkIngressResourceNames) string { return names.Tailnet }},
		{proxyGroupGVR, func(names NetworkIngressResourceNames) string { return names.ProxyGroup }},
	} {
		if err := list(resource.gvr, "", selector, "", func(object metav1.Object) {
			names, ok := identity(object)
			if !ok || object.GetName() != resource.name(names) {
				return
			}
			report(resourceKey{kind: resource.gvr.Resource, name: object.GetName()}, names.OwnerID)
		}); err != nil {
			return nil, err
		}
	}

	orderedNamespaces := make([]string, 0, len(namespaces))
	for namespace := range namespaces {
		orderedNamespaces = append(orderedNamespaces, namespace)
	}
	sort.Strings(orderedNamespaces)
	namespacedResources := []struct {
		gvr  schema.GroupVersionResource
		name func(NetworkIngressResourceNames) string
	}{
		{proxyGroupPolicyGVR, func(names NetworkIngressResourceNames) string { return names.ProxyGroupPolicy }},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, func(names NetworkIngressResourceNames) string { return names.AttestorDeployment }},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, func(names NetworkIngressResourceNames) string { return names.AttestorService }},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, func(names NetworkIngressResourceNames) string { return names.AttestorServiceAccount }},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, func(names NetworkIngressResourceNames) string { return names.AttestorCASecret }},
		{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, func(names NetworkIngressResourceNames) string { return names.Ingress }},
		{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, func(names NetworkIngressResourceNames) string { return names.AttestorNetworkPolicy }},
	}
	for _, namespace := range orderedNamespaces {
		names := namespaces[namespace]
		ownerSelector := labels.Set{
			managedByLabelKey:     networkIngressManagedBy,
			networkIngressIDLabel: names.OwnerID.String(),
		}.String()
		for _, resource := range namespacedResources {
			name := resource.name(names)
			if err := list(resource.gvr, namespace, ownerSelector, name, func(object metav1.Object) {
				objectNames, ok := identity(object)
				if !ok || objectNames.OwnerID != names.OwnerID || object.GetNamespace() != namespace || object.GetName() != name {
					return
				}
				report(resourceKey{kind: resource.gvr.Resource, namespace: namespace, name: name}, names.OwnerID)
			}); err != nil {
				return nil, err
			}
		}
		binding, err := p.clientset.RbacV1().RoleBindings(namespace).Get(ctx, networkIngressAttestorManagerBinding, metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("network ingress orphan inventory unavailable")
		}
		if err == nil {
			bindingNames, ok := identity(binding)
			if ok && bindingNames.OwnerID == names.OwnerID {
				report(resourceKey{kind: "rolebindings", namespace: namespace, name: networkIngressAttestorManagerBinding}, names.OwnerID)
			}
		}
	}

	networkPolicyGVR := schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}
	if err := list(networkPolicyGVR, p.config.OperatorNamespace, selector, "", func(object metav1.Object) {
		names, ok := identity(object)
		if !ok || object.GetNamespace() != p.config.OperatorNamespace || object.GetName() != names.ProxyNetworkPolicy {
			return
		}
		report(resourceKey{kind: "networkpolicies", namespace: p.config.OperatorNamespace, name: names.ProxyNetworkPolicy}, names.OwnerID)
	}); err != nil {
		return nil, err
	}

	return orphans, nil
}
