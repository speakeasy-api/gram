package networkingress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetIngressNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetIngress(readCtx(t, ctx), &gen.GetIngressPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetIngressReturnsCreatedIngress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created := createAuthKeyIngress(t, ctx, ti)

	got, err := ti.service.GetIngress(readCtx(t, ctx), &gen.GetIngressPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, created.Hostname, got.Hostname)
	require.Equal(t, created.CredentialKind, got.CredentialKind)
	require.True(t, got.AuthKeyConfigured)
}
