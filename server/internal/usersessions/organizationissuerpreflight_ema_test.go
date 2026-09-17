package usersessions_test

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOrganizationIssuerPreflightActiveEMABindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	user := seedOrganizationTierIssuer(t, ctx, ti.conn, "ema-preflight-human")
	remote := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "ema-preflight-remote", uuid.NullUUID{}, pgtype.Text{String: auth.ActiveOrganizationID, Valid: true})
	q := remoterepo.New(ti.conn)
	require.NoError(t, q.EnsureEMABinding(ctx, remoterepo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"}))
	preflight, err := ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: user.String()})
	require.NoError(t, err)
	require.Equal(t, int64(1), preflight.EmaBindingCount)
	require.Empty(t, preflight.McpServers)
	require.Empty(t, preflight.Toolsets)
	require.False(t, preflight.CanDelete)
	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: user.String()})
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeConflict, shared.Code)
	binding, err := q.GetEMABinding(ctx, remoterepo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: remote, Resource: "https://resource.example.com/"})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{ID: binding.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}})
	require.NoError(t, err)
	preflight, err = ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: user.String()})
	require.NoError(t, err)
	require.Zero(t, preflight.EmaBindingCount)
	require.True(t, preflight.CanDelete)
	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: user.String()}))
	count, err := testrepo.New(ti.conn).CountPreparationFixtureBindingByID(ctx, testrepo.CountPreparationFixtureBindingByIDParams{ID: binding.ID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.Zero(t, count, "user issuer deletion explicitly removes unlinked claims")
}
