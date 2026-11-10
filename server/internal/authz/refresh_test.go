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
	requireForbidden(t, engine.Require(refreshed, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
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
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	// Principal re-admission replaces live policy sets; the job boundary persists.
	bounded = admittedPoliciesToContext(bounded, RepresentedUserPolicy{UserID: "", Policy: nil}, false, []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	bounded = admittedPoliciesToContext(bounded, RepresentedUserPolicy{UserID: "", Policy: nil}, false, []Grant{})
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "initial"}))
}

func TestAdmissionBoundaryRetainsRepresentedUserPolicy(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	agentPolicy := []Grant{markerGrant}
	admitted := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{}}, false, agentPolicy)
	boundary, err := engine.CaptureAdmissionBoundary(admitted)
	require.NoError(t, err)
	requireForbidden(t, engine.Require(admitted, personalClientCheck("user_u")))
	// The represented user is granted mcp:auth after admission; the running
	// job must not gain it.
	refreshed := admittedPoliciesToContext(boundary.Apply(admitted), RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{markerGrant}}, false, agentPolicy)
	requireForbidden(t, engine.Require(refreshed, personalClientCheck("user_u")))
	require.NoError(t, engine.Require(admittedPoliciesToContext(admitted, RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{markerGrant}}, false, agentPolicy), personalClientCheck("user_u")))
}

func TestAdmissionBoundaryRequiresPreparedAuthorization(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	_, err := engine.CaptureAdmissionBoundary(enterpriseTestCtx(t.Context()))
	require.ErrorContains(t, err, "prepared authorization")
}
