package remotesessions_test

import (
	"testing"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAdminRemoteSessions_GetGlobalIssuerEMABindingCount(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "global-ema-count")
	user := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "global-ema-count-human")
	adminCtx := withAdmin(t, ctx)
	payload := &adminrsgen.GetGlobalIssuerPayload{ID: issuer.String()}
	got, err := ti.service.GetGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, got.EmaBindingCount)
	require.Zero(t, *got.EmaBindingCount)
	q := repo.New(ti.conn)
	key := repo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, Resource: "https://resource.example.com/"}
	require.NoError(t, q.EnsureEMABinding(ctx, key))
	got, err = ti.service.GetGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, got.EmaBindingCount)
	require.Equal(t, 1, *got.EmaBindingCount)
	binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	got, err = ti.service.GetGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, got.EmaBindingCount)
	require.Zero(t, *got.EmaBindingCount)
}
