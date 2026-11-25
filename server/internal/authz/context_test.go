package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPrepareContext_loadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.True(t, ok)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_123"}))
}

func TestPrepareContext_rejectsInvalidUserPrincipal(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.UserID = urn.AllUsersPrincipalID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := engine.PrepareContext(ctx)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	require.ErrorIs(t, err, ErrPrincipalInvalid)
}

func TestPrepareContext_skipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.WithLegacyAPIKeyAuthorization(ctx, authCtx)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.False(t, ok)
}

func TestPrepareContext_sessionWithoutActiveOrganizationGetsNoGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.Empty(t, grants)
}

func TestPrepareContext_loadsAssistantPrincipalGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "owner@example.com", "Owner", "user_workos_owner", "membership_owner")
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.True(t, ok)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_assistant"}))
}

func TestShouldEnforce_assistantPrincipalOnEnterpriseOrgEnforces(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestShouldEnforce_assistantPrincipalOnNonEnterpriseOrgEnforces(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestPrepareContext_loadsGrantsForNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID)
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.Len(t, grants, 1)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_non_enterprise"}))
}

func TestWithoutCredentialCeilingKeepsLivePolicies(t *testing.T) {
	t.Parallel()
	ceiling := []Grant{{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceID: "gateway"}}}
	agent := []Grant{{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceID: "member"}}}
	owner := []Grant{{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceID: "member"}}}
	ctx := admittedPoliciesToContext(t.Context(), RepresentedUserPolicy{}, true, ceiling, agent, owner)

	ctx = WithoutCredentialCeiling(ctx)

	policies, ok := currentGrantAuthorizationFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, [][]Grant{agent, owner}, policies.policies)
	require.False(t, policies.legacyAuthCeiling)
	got, ok := CredentialCeilingFromContext(ctx)
	require.True(t, ok)
	require.Empty(t, got)
}

// Removing the credential policy twice must leave the agent policy bounding
// the call. Here the agent may read project-one only, while the credential and
// the owner may read every project.
func TestWithoutCredentialCeilingTwiceKeepsAgentPolicy(t *testing.T) {
	t.Parallel()
	everyProject := []Grant{NewGrant(ScopeProjectRead, WildcardResource)}
	agent := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	ctx := principalPolicyTestContext(t, everyProject, agent, everyProject)

	ctx = WithoutCredentialCeiling(WithoutCredentialCeiling(ctx))

	policies, ok := currentGrantAuthorizationFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, [][]Grant{agent, everyProject}, policies.policies)
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	requireForbidden(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-two"}))
}

// A refresh admits the workload again with its credential policy, removes it
// once, and a later removal leaves the agent policy in place.
func TestWithoutCredentialCeilingAfterRefreshKeepsAgentPolicy(t *testing.T) {
	t.Parallel()
	everyProject := []Grant{NewGrant(ScopeProjectRead, WildcardResource)}
	agent := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
			return WorkloadSessionAdmission{
				AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001",
				OwnerUserID:    "user_123",
				Ceiling:        everyProject,
				Agent:          agent,
				Owner:          everyProject,
			}, nil
		},
	})
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), authCtx, urn.NewWorkloadPrincipal(uuid.New(), "subject"))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	ctx = WithoutCredentialCeiling(ctx)

	ctx, err = engine.RefreshContext(ctx)
	require.NoError(t, err)
	ctx = WithoutCredentialCeiling(ctx)

	policies, ok := currentGrantAuthorizationFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, [][]Grant{agent, everyProject}, policies.policies)
}

func TestCredentialCeilingFromContextIsImmutable(t *testing.T) {
	t.Parallel()
	source := []Grant{{
		Scope:    ScopeMCPConnect,
		Selector: Selector{SelectorKeyProjectID: "project-original"},
	}}
	ctx := admittedPoliciesToContext(t.Context(), RepresentedUserPolicy{}, false, source)

	source[0].Selector[SelectorKeyProjectID] = "project-mutated-source"
	first, ok := CredentialCeilingFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "project-original", first[0].Selector[SelectorKeyProjectID])

	first[0].Selector[SelectorKeyProjectID] = "project-mutated-return"
	second, ok := CredentialCeilingFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "project-original", second[0].Selector[SelectorKeyProjectID])
}

func TestPrepareContext_demoOrgGetsEveryUserVisibleScope(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, grants)

	// Demo sessions hold the same set access.ListGrants reports to the
	// dashboard, so no page is offered that the server then refuses.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeChatRead, ResourceID: "chat_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Environment(EnvironmentIDFrom("env_demo"), ProjectIDFrom(testProjectID)).Read()))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceID: "project_demo"}))
	require.NoError(t, engine.Require(ctx, MCPServer(MCPServerIDFrom("mcp_demo"), ProjectIDFrom(testProjectID)).Write()))

	// Internal scopes stay internal: the demo set is the user-visible
	// catalogue, not every scope the server declares.
	for _, grant := range grants {
		visibility, ok := ScopeVisibilityFor(grant.Scope)
		require.True(t, ok, "scope %q has no declared visibility", grant.Scope)
		require.Equal(t, ScopeVisibilityUserVisible, visibility, "scope %q is not user-visible", grant.Scope)
	}
}

func TestPrepareContext_demoOrgIgnoresScopeOverrides(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	// The demo branch runs before scope overrides, so an override neither
	// widens nor narrows a demo session.
	ctx = contextvalues.SetRBACScopeOverride(ctx, "project:write")

	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	// org:admin is outside the override, so passing it proves the demo branch
	// produced these grants rather than GrantsFromOverrides.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
}

func challengeLoggingAlwaysEnabled(context.Context, string) (bool, error) { return true, nil }

func demoTestCtx(t *testing.T) context.Context {
	t.Helper()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = constants.DemoOrganizationID
	return contextvalues.SetAuthContext(ctx, authCtx)
}
