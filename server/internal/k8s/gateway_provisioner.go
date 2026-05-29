package k8s

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

type GatewayProvisioner struct {
	client  gatewayclient.Interface
	dynamic dynamic.Interface
	// securityPolicyEnabled gates the Envoy Gateway SecurityPolicy reconcile. It
	// must stay off until Envoy Gateway and its SecurityPolicy CRD are installed
	// in the target namespaces; otherwise create/delete calls hit a missing CRD
	// and break provisioning. See InitializeK8sClient.
	securityPolicyEnabled bool
	namespace             string
	logger                *slog.Logger
}

func (p *GatewayProvisioner) Kind() ProvisionerKind { return ProvisionerKindGateway }

func (p *GatewayProvisioner) Setup(ctx context.Context, domain string, ipAllowlist []string) (SetupResult, error) {
	name, err := SanitizeDomainForK8sName(domain)
	if err != nil {
		return SetupResult{}, fmt.Errorf("sanitize domain: %w", err)
	}

	route := p.buildHTTPRoute(name, domain)
	routeInterface := p.client.GatewayV1().HTTPRoutes(p.namespace)

	existing, err := routeInterface.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return SetupResult{}, fmt.Errorf("get httproute %s: %w", name, err)
		}
		p.logger.InfoContext(ctx, "httproute not found, creating", attr.SlogResourceName(name))
		if _, createErr := routeInterface.Create(ctx, route, metav1.CreateOptions{}); createErr != nil {
			return SetupResult{}, fmt.Errorf("create httproute %s: %w", name, createErr)
		}
	} else {
		p.logger.InfoContext(ctx, "httproute found, updating", attr.SlogResourceName(name))
		route.ResourceVersion = existing.ResourceVersion
		if _, updateErr := routeInterface.Update(ctx, route, metav1.UpdateOptions{}); updateErr != nil {
			return SetupResult{}, fmt.Errorf("update httproute %s: %w", name, updateErr)
		}
	}

	// Gateway API has no native CIDR filtering; IP allow listing is delegated to
	// an Envoy Gateway SecurityPolicy targeting the HTTPRoute. Isolated in
	// gateway_security_policy.go so the mechanism can be swapped if the gateway
	// implementation changes.
	if err := p.reconcileSecurityPolicy(ctx, name, ipAllowlist); err != nil {
		return SetupResult{}, fmt.Errorf("reconcile security policy %s: %w", name, err)
	}

	return SetupResult{ResourceName: name, SecretName: ""}, nil
}

func (p *GatewayProvisioner) Get(ctx context.Context, resourceName string) error {
	_, err := p.client.GatewayV1().HTTPRoutes(p.namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get httproute: %w", err)
	}
	return nil
}

// Delete removes the HTTPRoute and any SecurityPolicy it owns. The parent
// Gateway's TLS Secret is shared and must not be touched.
//
// The HTTPRoute is removed first. If that fails the SecurityPolicy stays in
// place, so the still-live route remains IP-restricted rather than briefly
// open. Once the route is gone the SecurityPolicy guards nothing, so deleting
// it second is safe.
func (p *GatewayProvisioner) Delete(ctx context.Context, resourceName, _ string) error {
	if err := p.client.GatewayV1().HTTPRoutes(p.namespace).Delete(ctx, resourceName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete httproute %s: %w", resourceName, err)
	}
	p.logger.InfoContext(ctx, "httproute deleted", attr.SlogResourceName(resourceName))

	if err := p.deleteSecurityPolicy(ctx, resourceName); err != nil {
		return fmt.Errorf("delete security policy %s: %w", resourceName, err)
	}

	return nil
}

func (p *GatewayProvisioner) buildHTTPRoute(name, domain string) *gatewayv1.HTTPRoute {
	svcName := gatewayv1.ObjectName("gram-server")
	port := gatewayv1.PortNumber(80)
	pathPrefix := gatewayv1.PathMatchPathPrefix
	pathRegex := gatewayv1.PathMatchRegularExpression

	mcpPath := "/mcp"
	oauthPath := "/oauth"
	authServerPath := `/.well-known/oauth-authorization-server/mcp(/.*)?`
	protectedResourcePath := `/.well-known/oauth-protected-resource/mcp(/.*)?`

	backendRefs := []gatewayv1.HTTPBackendRef{
		{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: svcName,
					Port: &port,
				},
			},
		},
	}

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "custom-domain-chart",
				"custom-domain":                domain,
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "gram-gateway"},
				},
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(domain)},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{Path: &gatewayv1.HTTPPathMatch{Type: &pathPrefix, Value: &mcpPath}},
						{Path: &gatewayv1.HTTPPathMatch{Type: &pathPrefix, Value: &oauthPath}},
						{Path: &gatewayv1.HTTPPathMatch{Type: &pathRegex, Value: &authServerPath}},
						{Path: &gatewayv1.HTTPPathMatch{Type: &pathRegex, Value: &protectedResourcePath}},
					},
					BackendRefs: backendRefs,
				},
			},
		},
	}
}
