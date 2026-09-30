package authz

import (
	"context"
	"testing"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRefreshContextObservesRevocationWithoutChangingOriginalRequest(t *testing.T) {
	t.Parallel()
	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedOrganization(t, ctx, conn, auth.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, auth.ActiveOrganizationID, auth.UserID, "fixture@example.com", "Fixture User", "fixture-workos-user", "fixture-membership")
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID)
	seedGrant(t, ctx, conn, auth.ActiveOrganizationID, principal, ScopeProjectRead, WildcardResource)
	admitted, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(admitted, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, err = accessrepo.New(conn).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{OrganizationID: auth.ActiveOrganizationID, PrincipalUrn: principal})
	require.NoError(t, err)
	refreshed, err := engine.RefreshContext(admitted)
	require.NoError(t, err)
	require.Error(t, engine.Require(refreshed, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	require.NoError(t, engine.Require(admitted, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
}

func TestAdmissionBoundarySurvivesGrantReplacement(t *testing.T) {
	t.Parallel()
	ctx := enterpriseTestCtx(t.Context())
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	initial := GrantsToContext(ctx, []Grant{NewGrant(ScopeProjectRead, "initial")})
	current := GrantsToContext(ctx, []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	boundary, err := engine.CaptureAdmissionBoundary(initial)
	require.NoError(t, err)
	bounded := boundary.Apply(current)
	require.NoError(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "initial"}))
	require.Error(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	// Principal re-admission replaces live policy sets; the job boundary persists.
	bounded = admittedPoliciesToContext(bounded, []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	require.Error(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	bounded = admittedPoliciesToContext(bounded, []Grant{})
	require.Error(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "initial"}))
}

func TestAdmissionBoundaryRequiresPreparedAuthorization(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	_, err := engine.CaptureAdmissionBoundary(enterpriseTestCtx(t.Context()))
	require.ErrorContains(t, err, "prepared authorization")
}
