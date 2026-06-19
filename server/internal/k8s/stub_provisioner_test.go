package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStubProvisioner_KindMatchesRequest(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	clients, err := k8s.InitializeK8sClient(ctx, testenv.NewLogger(t), "local", false)
	require.NoError(t, err)

	require.Equal(t, k8s.ProvisionerKindIngress, clients.Provisioner(k8s.ProvisionerKindIngress).Kind())
	require.Equal(t, k8s.ProvisionerKindGateway, clients.Provisioner(k8s.ProvisionerKindGateway).Kind())
}

func TestStubProvisioner_IsNoOp(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	p := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, testenv.NewLogger(t))

	result, err := p.Setup(ctx, "test.example.com", nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.ResourceName)
	require.Empty(t, result.SecretName, "stub never sets SecretName")

	require.NoError(t, p.Get(ctx, result.ResourceName))
	require.NoError(t, p.Delete(ctx, result.ResourceName, "ignored-secret"))
}

func TestStubProvisioner_RecordsCalls(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	p := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, testenv.NewLogger(t))

	result, err := p.Setup(ctx, "my.example.com", []string{"1.2.3.4"})
	require.NoError(t, err)
	require.NoError(t, p.Get(ctx, result.ResourceName))
	require.NoError(t, p.Delete(ctx, result.ResourceName, "cert-secret"))

	calls := p.Calls()
	require.Len(t, calls, 3)

	require.Equal(t, "Setup", calls[0].Method)
	require.Equal(t, "my.example.com", calls[0].Domain)
	require.Equal(t, []string{"1.2.3.4"}, calls[0].IPAllowlist)

	require.Equal(t, "Get", calls[1].Method)
	require.Equal(t, result.ResourceName, calls[1].ResourceName)

	require.Equal(t, "Delete", calls[2].Method)
	require.Equal(t, result.ResourceName, calls[2].ResourceName)
	require.Equal(t, "cert-secret", calls[2].SecretName)
}

func TestStubProvisioner_ResourceNameHasNoDots(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	p := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, testenv.NewLogger(t))

	result, err := p.Setup(ctx, "my-domain.example.com", nil)
	require.NoError(t, err)
	require.NotContains(t, result.ResourceName, ".", "k8s resource name must not contain dots")
}
