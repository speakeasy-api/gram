package k8s

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

const gatewayTestNamespace = "gram-test"

func newGatewayProvisioner(t *testing.T) (*GatewayProvisioner, *gatewayfake.Clientset) {
	t.Helper()
	cs := gatewayfake.NewSimpleClientset()
	return &GatewayProvisioner{
		client:    cs,
		namespace: gatewayTestNamespace,
		logger:    testenv.NewLogger(t),
	}, cs
}

func TestGatewayProvisioner_Kind(t *testing.T) {
	t.Parallel()
	p, _ := newGatewayProvisioner(t)
	require.Equal(t, ProvisionerKindGateway, p.Kind())
}

func TestGatewayProvisioner_Setup_CreateNew(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newGatewayProvisioner(t)
	domain := "test.example.com"

	result, err := p.Setup(ctx, domain)
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
	p, cs := newGatewayProvisioner(t)
	domain := "update.example.com"

	_, err := p.Setup(ctx, domain)
	require.NoError(t, err)

	_, err = p.Setup(ctx, domain)
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)

	routes, err := cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, routes.Items, 1)
	require.Equal(t, expectedName, routes.Items[0].Name)
}

func TestGatewayProvisioner_Get_Found(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newGatewayProvisioner(t)
	domain := "get.example.com"

	result, err := p.Setup(ctx, domain)
	require.NoError(t, err)

	require.NoError(t, p.Get(ctx, result.ResourceName))
}

func TestGatewayProvisioner_Get_NotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newGatewayProvisioner(t)

	err := p.Get(ctx, "nonexistent-httproute")
	require.Error(t, err)
}

func TestGatewayProvisioner_Delete_DoesNotTouchSecret(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newGatewayProvisioner(t)
	domain := "delete.example.com"

	result, err := p.Setup(ctx, domain)
	require.NoError(t, err)

	require.NoError(t, p.Delete(ctx, result.ResourceName, ""))

	_, err = cs.GatewayV1().HTTPRoutes(gatewayTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}

func TestGatewayProvisioner_Setup_SecretNameEmpty(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newGatewayProvisioner(t)

	result, err := p.Setup(ctx, "secret.example.com")
	require.NoError(t, err)
	require.Empty(t, result.SecretName)
}
