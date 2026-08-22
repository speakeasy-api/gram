package networkingress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateIngressSettings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateIngress(adminCtx(t, ctx), &gen.UpdateIngressPayload{
		SessionToken:       nil,
		Hostname:           new("mcp-private"),
		Enabled:            new(false),
		PrivateNetworkOnly: new(true),
		IdentityRequired:   nil,
	})
	require.NoError(t, err)
	require.Equal(t, "mcp-private", updated.Hostname)
	require.False(t, updated.Enabled)
	require.True(t, updated.PrivateNetworkOnly)
	// Untouched settings keep their values.
	require.True(t, updated.IdentityRequired)
	require.True(t, updated.AuthKeyConfigured)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionNetworkIngressUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionNetworkIngressUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "gram-mcp", beforeSnapshot["Hostname"])
	require.Equal(t, "mcp-private", afterSnapshot["Hostname"])
}

func TestUpdateIngressRequiresASetting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createAuthKeyIngress(t, ctx, ti)

	_, err := ti.service.UpdateIngress(adminCtx(t, ctx), &gen.UpdateIngressPayload{
		SessionToken:       nil,
		Hostname:           nil,
		Enabled:            nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateIngressNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateIngress(adminCtx(t, ctx), &gen.UpdateIngressPayload{
		SessionToken:       nil,
		Hostname:           new("mcp-private"),
		Enabled:            nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
