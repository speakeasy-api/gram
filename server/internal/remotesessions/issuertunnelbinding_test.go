package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func seedTunneledMcpServer(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        uuid.New(),
		ProjectID: *authCtx.ProjectID,
		Name:      "tunnel-" + uuid.NewString()[:8],
		KeyHash:   uuid.NewString(),
		KeyPrefix: "tunnel_test",
	})
	require.NoError(t, err)
	return created.ID
}

func TestCreateRemoteSessionIssuer_TunnelBindingRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	tunnelID := seedTunneledMcpServer(t, ctx, ti)
	payload := newIssuerPayload("idp-tunnel-forbidden")
	payload.TunneledMcpServerID = conv.PtrEmpty(tunnelID.String())

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRemoteSessionIssuer_TunnelBindingPersisted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	tunnelID := seedTunneledMcpServer(t, ctx, ti)
	ctx = withAdmin(t, ctx)
	payload := newIssuerPayload("idp-tunnel-bound")
	payload.TunneledMcpServerID = conv.PtrEmpty(tunnelID.String())

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.TunneledMcpServerID)
	require.Equal(t, tunnelID.String(), *created.TunneledMcpServerID)
}

func TestCreateRemoteSessionIssuer_TunnelBindingUnknownTunnel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)
	payload := newIssuerPayload("idp-tunnel-unknown")
	payload.TunneledMcpServerID = conv.PtrEmpty(uuid.NewString())

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateRemoteSessionIssuer_TunnelBindingSetAndClear(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-tunnel-update"))
	require.NoError(t, err)
	require.Nil(t, created.TunneledMcpServerID)
	tunnelID := seedTunneledMcpServer(t, ctx, ti)

	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  created.ID,
		TunneledMcpServerID: conv.PtrEmpty(tunnelID.String()),
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	adminCtx := withAdmin(t, ctx)
	updated, err := ti.service.UpdateRemoteSessionIssuer(adminCtx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  created.ID,
		TunneledMcpServerID: conv.PtrEmpty(tunnelID.String()),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TunneledMcpServerID)
	require.Equal(t, tunnelID.String(), *updated.TunneledMcpServerID)

	name := "renamed"
	updated, err = ti.service.UpdateRemoteSessionIssuer(adminCtx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &name,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TunneledMcpServerID)

	empty := ""
	updated, err = ti.service.UpdateRemoteSessionIssuer(adminCtx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                  created.ID,
		TunneledMcpServerID: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.TunneledMcpServerID)
}
