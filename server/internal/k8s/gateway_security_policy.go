package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// securityPolicyGVR is the Envoy Gateway SecurityPolicy custom resource. IP
// allow listing on the Gateway API path is implemented through this CRD because
// core Gateway API has no native CIDR filtering. The whole mechanism lives in
// this file so it can be replaced wholesale if the gateway implementation
// changes (e.g. Istio AuthorizationPolicy).
var securityPolicyGVR = schema.GroupVersionResource{
	Group:    "gateway.envoyproxy.io",
	Version:  "v1alpha1",
	Resource: "securitypolicies",
}

// reconcileSecurityPolicy creates/updates the SecurityPolicy that restricts the
// HTTPRoute to the given IPv4 sources. An empty allowlist deletes any existing
// policy so the route becomes open to all.
func (p *GatewayProvisioner) reconcileSecurityPolicy(ctx context.Context, routeName string, ipAllowlist []string) error {
	if !p.securityPolicyEnabled {
		if len(ipAllowlist) > 0 {
			p.logger.WarnContext(ctx, "gateway ip allowlist requested but SecurityPolicy reconcile is gated off; skipping", attr.SlogResourceName(routeName))
		}
		return nil
	}

	if len(ipAllowlist) == 0 {
		return p.deleteSecurityPolicy(ctx, routeName)
	}

	desired := buildSecurityPolicy(p.namespace, routeName, ipAllowlist)
	policies := p.dynamic.Resource(securityPolicyGVR).Namespace(p.namespace)

	existing, err := policies.Get(ctx, routeName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get securitypolicy %s: %w", routeName, err)
		}
		p.logger.InfoContext(ctx, "securitypolicy not found, creating", attr.SlogResourceName(routeName))
		if _, createErr := policies.Create(ctx, desired, metav1.CreateOptions{}); createErr != nil {
			return fmt.Errorf("create securitypolicy %s: %w", routeName, createErr)
		}
		return nil
	}

	p.logger.InfoContext(ctx, "securitypolicy found, updating", attr.SlogResourceName(routeName))
	desired.SetResourceVersion(existing.GetResourceVersion())
	if _, updateErr := policies.Update(ctx, desired, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("update securitypolicy %s: %w", routeName, updateErr)
	}
	return nil
}

// deleteSecurityPolicy removes the SecurityPolicy for the route. Missing policy
// is not an error — Delete and the open-allowlist path both rely on this.
func (p *GatewayProvisioner) deleteSecurityPolicy(ctx context.Context, routeName string) error {
	if !p.securityPolicyEnabled {
		return nil
	}

	err := p.dynamic.Resource(securityPolicyGVR).Namespace(p.namespace).Delete(ctx, routeName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete securitypolicy %s: %w", routeName, err)
	}
	if err == nil {
		p.logger.InfoContext(ctx, "securitypolicy deleted", attr.SlogResourceName(routeName))
	}
	return nil
}

// buildSecurityPolicy builds an Envoy Gateway SecurityPolicy that denies by
// default and allows only the supplied client CIDRs, targeting the HTTPRoute of
// the same name.
func buildSecurityPolicy(namespace, routeName string, ipAllowlist []string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": securityPolicyGVR.Group + "/" + securityPolicyGVR.Version,
			"kind":       "SecurityPolicy",
			"metadata": map[string]any{
				"name":      routeName,
				"namespace": namespace,
				"labels": map[string]any{
					managedByLabelKey: managedByLabelValue,
				},
			},
			"spec": map[string]any{
				"targetRefs": []any{
					map[string]any{
						"group": "gateway.networking.k8s.io",
						"kind":  "HTTPRoute",
						"name":  routeName,
					},
				},
				"authorization": map[string]any{
					"defaultAction": "Deny",
					"rules": []any{
						map[string]any{
							"action": "Allow",
							"principal": map[string]any{
								"clientCIDRs": toClientCIDRs(ipAllowlist),
							},
						},
					},
				},
			},
		},
	}
}

// toClientCIDRs normalizes allowlist entries into CIDR notation as required by
// the SecurityPolicy clientCIDRs field. Bare IPv4 addresses become /32.
func toClientCIDRs(entries []string) []any {
	cidrs := make([]any, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			cidrs = append(cidrs, entry)
			continue
		}
		cidrs = append(cidrs, entry+"/32")
	}
	return cidrs
}
