package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func staticRBAC(enabled bool) IsRBACEnabled {
	return func(context.Context, string) (bool, error) {
		return enabled, nil
	}
}

func staticChallengeLogging(enabled bool) ChallengeLoggingEnabled {
	return func(context.Context, string) (bool, error) {
		return enabled, nil
	}
}

func failingRBAC(err error) IsRBACEnabled {
	return func(context.Context, string) (bool, error) {
		return false, err
	}
}

func TestEngineRequire_requiresAuthContext(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	err = engine.Require(t.Context(), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestEngineRequire_skipsWhenRBACFeatureDisabled(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient())

	err = engine.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestEngineRequire_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), nil)

	err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequire_mapsMissingGrantsToUnexpected(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	err = engine.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestEngineRequire_returnsUnexpectedWhenFeatureCheckFails(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, failingRBAC(errors.New("boom")), staticChallengeLogging(true), workos.NewStubClient())

	err = engine.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestResolveRoleSlugs_readsLocalAssignmentsOnly(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")

	membership := &countingMembershipFetcher{}
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, staticRBAC(true), staticChallengeLogging(true), membership)

	rolePrincipals, err := engine.resolveRolePrincipals(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rolePrincipals)

	rolePrincipals, err = engine.resolveRolePrincipals(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rolePrincipals)
	require.Equal(t, 0, membership.calls)
}

func TestResolveRolePrincipals_returnsAllLocalAssignments(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")
	seedRole(t, ctx, conn, authCtx.ActiveOrganizationID, "custom-alpha")
	seedRole(t, ctx, conn, authCtx.ActiveOrganizationID, "custom-beta")
	seedRoleAssignment(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "user_workos_test", "membership_test", "custom-alpha")
	seedRoleAssignment(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "user_workos_test", "membership_test", "custom-beta")

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, staticRBAC(true), staticChallengeLogging(true), &countingMembershipFetcher{})

	rolePrincipals, err := engine.resolveRolePrincipals(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	roleSlugs := make([]string, 0, len(rolePrincipals))
	principalURNs := make([]string, 0, len(rolePrincipals))
	for _, role := range rolePrincipals {
		roleSlugs = append(roleSlugs, role.RoleSlug)
		principalURNs = append(principalURNs, role.PrincipalUrn)
	}
	require.ElementsMatch(t, []string{"custom-alpha", "custom-beta"}, roleSlugs)
	require.Len(t, principalURNs, 2)
	for _, principalURN := range principalURNs {
		require.Contains(t, principalURN, "role:organization:")
	}
}

func TestEngineRequireAny_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, "tool_a")})

	err = engine.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_b"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_c"},
	)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_returnsAllowedSubset(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, "proj_123")})

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
		{Scope: ScopeProjectRead, ResourceID: "proj_456"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123"}, resourceIDs)
}

func TestEngineFilter_logsSingleAggregateChallenge(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_allowed")})
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_allowed"},
		{Scope: ScopeProjectRead, ResourceID: "proj_denied"},
		{Scope: ScopeProjectRead, ResourceID: "proj_other"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_allowed"}, resourceIDs)

	require.Eventually(t, func() bool {
		rows, err := chConn.Query(t.Context(), `
			SELECT count(), any(outcome), any(reason),
			       any(filter_candidate_count), any(filter_allowed_count),
			       any(requested_checks.resource_id)
			FROM authz_challenges
			WHERE organization_id = ? AND operation = 'filter'
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var (
			count                    uint64
			outcome, reason          string
			candidateCnt, allowedCnt uint32
			reqResourceIDs           []string
		)
		if err := rows.Scan(&count, &outcome, &reason, &candidateCnt, &allowedCnt, &reqResourceIDs); err != nil {
			return false
		}
		return count == 1 &&
			outcome == string(authzrepo.OutcomeAllow) &&
			reason == string(authzrepo.ReasonGrantMatched) &&
			candidateCnt == 3 && allowedCnt == 1 &&
			len(reqResourceIDs) == 3
	}, 5*time.Second, 100*time.Millisecond)
}

func TestEngineFilter_logsDenyWhenNoMatches(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_other")})
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_a"},
		{Scope: ScopeProjectRead, ResourceID: "proj_b"},
	})
	require.NoError(t, err)
	require.Empty(t, resourceIDs)

	require.Eventually(t, func() bool {
		rows, err := chConn.Query(t.Context(), `
			SELECT count(), any(outcome), any(reason),
			       any(filter_candidate_count), any(filter_allowed_count)
			FROM authz_challenges
			WHERE organization_id = ? AND operation = 'filter'
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var count uint64
		var outcome, reason string
		var candidateCnt, allowedCnt uint32
		if err := rows.Scan(&count, &outcome, &reason, &candidateCnt, &allowedCnt); err != nil {
			return false
		}
		return count == 1 &&
			outcome == string(authzrepo.OutcomeDeny) &&
			reason == string(authzrepo.ReasonScopeUnsatisfied) &&
			candidateCnt == 2 && allowedCnt == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func TestEngineFilter_skipsLogWhenNoChecks(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, resourceIDs)

	// Give async insert a moment, then verify nothing landed.
	time.Sleep(500 * time.Millisecond)
	rows, err := chConn.Query(t.Context(), `
		SELECT count() FROM authz_challenges WHERE organization_id = ? AND operation = 'filter'
	`, orgID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var count uint64
	require.NoError(t, rows.Scan(&count))
	require.Equal(t, uint64(0), count)
}

func TestEngineFilter_withDimensions(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{Scope: ScopeMCPConnect, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "toolsetA",
			"tool":          "tool_1",
		}},
	})

	// Only tool_1 matches the grant — one resource ID returned.
	results, err := engine.Filter(ctx, []Check{
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "tool_1", Disposition: ""}),
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "tool_2", Disposition: ""}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"toolsetA"}, results)
}

func TestEngineFilter_withDisposition(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{Scope: ScopeMCPConnect, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "toolsetA",
			"disposition":   "read_only",
		}},
	})

	// read_only disposition matches, destructive does not.
	results, err := engine.Filter(ctx, []Check{
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "safe_tool", Disposition: "read_only"}),
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "risky_tool", Disposition: "destructive"}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"toolsetA"}, results)
}

func TestEngineFilter_serverLevelGrantAllowsAllDimensions(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeMCPConnect, "toolsetA"),
	})

	// Server-level grant (no tool/disposition keys) allows everything.
	results, err := engine.Filter(ctx, []Check{
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "any_tool", Disposition: "destructive"}),
		MCPToolCallCheck("toolsetA", MCPToolCallDimensions{Tool: "other_tool", Disposition: "read_only"}),
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestEngineFilter_projectScopedGrantMatchesServersInProject(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{Scope: ScopeMCPConnect, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "*",
			"project_id":    "proj_A",
		}},
	})

	// Server in proj_A matches; server in proj_B does not.
	results, err := engine.Filter(ctx, []Check{
		MCPCheck(ScopeMCPConnect, "serverX", "proj_A"),
		MCPCheck(ScopeMCPConnect, "serverY", "proj_B"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"serverX"}, results)
}

func TestEngineRequire_projectScopedGrantAllowsToolsInProject(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{Scope: ScopeMCPConnect, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "*",
			"project_id":    "proj_A",
		}},
	})

	// Tool call on server in proj_A should pass.
	err = engine.Require(ctx, MCPToolCallCheck("serverX", MCPToolCallDimensions{
		Tool:      "my_tool",
		ProjectID: "proj_A",
	}))
	require.NoError(t, err)

	// Tool call on server in proj_B should fail.
	err = engine.Require(ctx, MCPToolCallCheck("serverY", MCPToolCallDimensions{
		Tool:      "my_tool",
		ProjectID: "proj_B",
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequire_projectScopedMCPReadGrant(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{Scope: ScopeMCPRead, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "*",
			"project_id":    "proj_A",
		}},
	})

	// mcp:read check for server in proj_A passes.
	err = engine.Require(ctx, MCPCheck(ScopeMCPRead, "serverX", "proj_A"))
	require.NoError(t, err)

	// mcp:read check for server in proj_B fails.
	err = engine.Require(ctx, MCPCheck(ScopeMCPRead, "serverY", "proj_B"))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_projectAndServerGrantsCombine(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		// Project-scoped grant for proj_A
		{Scope: ScopeMCPConnect, Selector: Selector{
			"resource_kind": "mcp",
			"resource_id":   "*",
			"project_id":    "proj_A",
		}},
		// Server-specific grant for serverZ (in proj_B)
		NewGrant(ScopeMCPConnect, "serverZ"),
	})

	results, err := engine.Filter(ctx, []Check{
		MCPCheck(ScopeMCPConnect, "serverX", "proj_A"), // matches project grant
		MCPCheck(ScopeMCPConnect, "serverY", "proj_B"), // no match
		MCPCheck(ScopeMCPConnect, "serverZ", "proj_B"), // matches server grant
	})
	require.NoError(t, err)
	require.Equal(t, []string{"serverX", "serverZ"}, results)
}

func TestEngineRequire_rejectsInvalidCheck(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: ""})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestEngineRequire_requiresChecks(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	err = engine.Require(ctx)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrNoChecks)
}

func TestEngineRequire_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "key_123",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})

	err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestEngineFilter_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "pro",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
		{Scope: ScopeProjectRead, ResourceID: "proj_456"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}

type countingMembershipFetcher struct {
	calls int
}

func (c *countingMembershipFetcher) GetOrgMembership(context.Context, string, string) (*workos.Member, error) {
	c.calls++
	return nil, nil
}

func enterpriseSessionCtx(t *testing.T) context.Context {
	t.Helper()
	return enterpriseSessionCtxWithOrg(t, "org_123")
}

func enterpriseSessionCtxWithOrg(t *testing.T, orgID string) context.Context {
	t.Helper()

	sessionID := "session_123"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

func scopeOverrideCtx(t *testing.T, isAdmin bool, accountType string) context.Context {
	t.Helper()
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           accountType,
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               isAdmin,
	})
	return contextvalues.SetRBACScopeOverride(ctx, "project:read")
}

func TestCanUseOverride_devPlusAdmin(t *testing.T) {
	t.Parallel()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient(), EngineOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_devPlusNonAdmin(t *testing.T) {
	t.Parallel()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient(), EngineOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, false, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_prodPlusAdmin(t *testing.T) {
	t.Parallel()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient())
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_prodPlusNonAdmin(t *testing.T) {
	t.Parallel()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient())
	ctx := scopeOverrideCtx(t, false, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)
}
