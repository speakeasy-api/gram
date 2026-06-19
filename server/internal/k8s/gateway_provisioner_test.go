package k8s

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

const gatewayTestNamespace = "gram-test"

func newGatewayProvisioner(t *testing.T) (*GatewayProvisioner, *gatewayfake.Clientset, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	cs := gatewayfake.NewSimpleClientset()
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{securityPolicyGVR: "SecurityPolicyList"},
	)
	return &GatewayProvisioner{
		client:                cs,
		dynamic:               dc,
		securityPolicyEnabled: true,
		namespace:             gatewayTestNamespace,
		logger:                testenv.NewLogger(t),
	}, cs, dc
}

func TestGatewayProvisioner_Kind(t *testing.T) {
	t.Parallel()
	p, _, _ := newGatewayProvisioner(t)
	require.Equal(t, ProvisionerKindGateway, p.Kind())
}

func TestGatewayProvisioner_Setup_CreateNew(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs, _ := newGatewayProvisioner(t)
	domain := "test.example.com"

	result, err := p.Setup(ctx, domain, nil)
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)
	require.Equal(t, expectedName, result.ResourceName)

	route, err := cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).Get(ctx, expectedName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, expectedName, route.Name)
	require.Equal(t, gatewayTestNamespace, route.Namespace)
	require.Len(t, route.Spec.Hostnames, 1)
	require.Equal(t, domain, string(route.Spec.Hostnames[0]))
	require.Len(t, route.Spec.ParentRefs, 1)
	require.Equal(t, "gram-gateway", string(route.Spec.ParentRefs[0].Name))
}

func TestGatewayProvisioner_Setup_UpdateExisting(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs, _ := newGatewayProvisioner(t)
	domain := "update.example.com"

	_, err := p.Setup(ctx, domain, nil)
	require.NoError(t, err)

	_, err = p.Setup(ctx, domain, nil)
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)

	routes, err := cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, routes.Items, 1)
	require.Equal(t, expectedName, routes.Items[0].Name)
}

func TestGatewayProvisioner_Setup_WithAllowlist_CreatesSecurityPolicy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _, dc := newGatewayProvisioner(t)
	domain := "secure.example.com"

	result, err := p.Setup(ctx, domain, []string{"1.2.3.4", "10.0.0.0/8"})
	require.NoError(t, err)

	policy, err := dc.Resource(securityPolicyGVR).Namespace(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)

	defaultAction, _, err := unstructured.NestedString(policy.Object, "spec", "authorization", "defaultAction")
	require.NoError(t, err)
	require.Equal(t, "Deny", defaultAction)

	rules, found, err := unstructured.NestedSlice(policy.Object, "spec", "authorization", "rules")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, rules, 1)
	rule, ok := rules[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Allow", rule["action"])
	principal, ok := rule["principal"].(map[string]any)
	require.True(t, ok)
	rawCIDRs, ok := principal["clientCIDRs"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"1.2.3.4/32", "10.0.0.0/8"}, rawCIDRs)
}

func TestGatewayProvisioner_Setup_EmptyAllowlist_DeletesSecurityPolicy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _, dc := newGatewayProvisioner(t)
	domain := "open.example.com"

	// Restrict, then re-apply with an empty allowlist to lift the restriction.
	result, err := p.Setup(ctx, domain, []string{"1.2.3.4"})
	require.NoError(t, err)

	_, err = dc.Resource(securityPolicyGVR).Namespace(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)

	_, err = p.Setup(ctx, domain, nil)
	require.NoError(t, err)

	_, err = dc.Resource(securityPolicyGVR).Namespace(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}

func TestGatewayProvisioner_Setup_GatedOff_SkipsSecurityPolicy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs, dc := newGatewayProvisioner(t)
	p.securityPolicyEnabled = false
	domain := "gated.example.com"

	result, err := p.Setup(ctx, domain, []string{"1.2.3.4"})
	require.NoError(t, err)

	// HTTPRoute is still provisioned; SecurityPolicy is not touched.
	_, err = cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)

	_, err = dc.Resource(securityPolicyGVR).Namespace(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	// Delete must also skip the policy and not error.
	require.NoError(t, p.Delete(ctx, result.ResourceName, ""))
}

func TestGatewayProvisioner_Get_Found(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _, _ := newGatewayProvisioner(t)
	domain := "get.example.com"

	result, err := p.Setup(ctx, domain, nil)
	require.NoError(t, err)

	require.NoError(t, p.Get(ctx, result.ResourceName))
}

func TestGatewayProvisioner_Get_NotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _, _ := newGatewayProvisioner(t)

	err := p.Get(ctx, "nonexistent-httproute")
	require.Error(t, err)
}

func TestGatewayProvisioner_Delete_DoesNotTouchSecret(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs, dc := newGatewayProvisioner(t)
	domain := "delete.example.com"

	result, err := p.Setup(ctx, domain, []string{"1.2.3.4"})
	require.NoError(t, err)

	require.NoError(t, p.Delete(ctx, result.ResourceName, ""))

	_, err = cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	// The SecurityPolicy is owned by the route and must be removed alongside it.
	_, err = dc.Resource(securityPolicyGVR).Namespace(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}

func TestGatewayProvisioner_Setup_SecretNameEmpty(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _, _ := newGatewayProvisioner(t)

	result, err := p.Setup(ctx, "secret.example.com", nil)
	require.NoError(t, err)
	require.Empty(t, result.SecretName)
}
