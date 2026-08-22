package networkingress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteIngress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressDelete)
	require.NoError(t, err)

	err = ti.service.DeleteIngress(adminCtx(t, ctx), &gen.DeleteIngressPayload{SessionToken: nil})
	require.NoError(t, err)

	_, err = ti.service.GetIngress(readCtx(t, ctx), &gen.GetIngressPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestDeleteIngressNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteIngress(adminCtx(t, ctx), &gen.DeleteIngressPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteIngressAllowsRecreation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	first := createAuthKeyIngress(t, ctx, ti)

	err := ti.service.DeleteIngress(adminCtx(t, ctx), &gen.DeleteIngressPayload{SessionToken: nil})
	require.NoError(t, err)

	// The one-per-org unique index is partial on deleted rows, so a fresh
	// ingress can be created after a soft delete.
	second := createAuthKeyIngress(t, ctx, ti)
	require.NotEqual(t, first.ID, second.ID)
}
