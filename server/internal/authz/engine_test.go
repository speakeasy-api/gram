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

func TestEvaluateLoadedGrants_doesNotConsultShouldEnforce(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "apikey_123",
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "pro",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)

	err = engine.EvaluateLoadedGrants(ctx, nil, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
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

	// Empty input must not emit a challenge log entry; confirm none ever lands.
	require.Never(t, func() bool {
		rows, err := chConn.Query(t.Context(), `
			SELECT count() FROM authz_challenges WHERE organization_id = ? AND operation = 'filter'
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "no challenge log entry should be emitted for empty input")
}

// --- Engine deny-wins tests ---

func TestEngineRequire_denyGrantBlocksAccess(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_secret"),
	})

	// Allowed resource — should pass.
	err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_normal"})
	require.NoError(t, err)

	// Denied resource — should be forbidden.
	err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_secret"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequireAny_denySkipsToNextCheck(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "tool_blocked"),
	})

	// One denied, one allowed — RequireAny should succeed via the allowed one.
	err = engine.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_blocked"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_ok"},
	)
	require.NoError(t, err)
}

func TestEngineRequireAny_allDeniedReturnsForbidden(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, WildcardResource),
	})

	err = engine.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_a"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_b"},
	)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_denyExcludesResources(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectRead, WildcardResource),
		NewDenyGrant(ScopeProjectRead, "proj_secret"),
	})

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_normal"},
		{Scope: ScopeProjectRead, ResourceID: "proj_secret"},
		{Scope: ScopeProjectRead, ResourceID: "proj_other"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_normal", "proj_other"}, resourceIDs)
}

func TestEngineFindMatched_denyReturnsFalse(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeMCPConnect, WildcardResource),
		NewDenyGrant(ScopeMCPConnect, "tool_blocked"),
	})

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeMCPConnect, ResourceID: "tool_ok"},
		{Scope: ScopeMCPConnect, ResourceID: "tool_blocked"},
		{Scope: ScopeMCPConnect, ResourceID: "tool_also_ok"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, true}, matched)
}

func TestEngineRequire_projectWriteBlocklistBlocksAccess(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectWrite, WildcardResource),
		NewGrantWithSelector(ScopeProjectBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindProject,
			SelectorKeyResourceID:   projectID,
		}),
	})

	err = engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "project_other", Dimensions: nil})
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: projectID, Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_mcpWriteBlocklistExcludesProjectScopedResources(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeMCPWrite, WildcardResource),
		NewGrantWithSelector(ScopeMCPBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindMCP,
			SelectorKeyResourceID:   WildcardResource,
			SelectorKeyProjectID:    projectID,
		}),
	})

	resourceIDs, err := engine.Filter(ctx, []Check{
		MCPCheck(ScopeMCPWrite, "server_in_project", projectID),
		MCPCheck(ScopeMCPWrite, "server_other_project", "project_other"),
		{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "dimensionless_probe", Dimensions: nil},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"server_other_project", "dimensionless_probe"}, resourceIDs)
}

func TestEngineFilter_withDimensions(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope:  ScopeMCPConnect,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   "toolsetA",
				SelectorKeyTool:         "tool_1",
			},
		},
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
		{
			Scope:  ScopeMCPConnect,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   "toolsetA",
				SelectorKeyDisposition:  DispositionReadOnly,
			},
		},
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
		{
			Scope:  ScopeMCPConnect,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
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
		{
			Scope:  ScopeMCPConnect,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
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
		{
			Scope:  ScopeMCPRead,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
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
		{
			Scope:  ScopeMCPConnect,
			Effect: PolicyEffectAllow,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
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

func TestEngineFindMatched_returnsParallelBools(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, "proj_123")})

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
		{Scope: ScopeProjectRead, ResourceID: "proj_456"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, matched, "result must align with input order")
}

func TestEngineFindMatched_preservesOrderAcrossMixedMatches(t *testing.T) {
	t.Parallel()

	// Grants allow proj_b and proj_d. Input ordering puts allowed entries
	// at index 1 and 3 — the returned bools must reflect those positions
	// exactly, with no implicit reordering.
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectRead, "proj_b"),
		NewGrant(ScopeProjectRead, "proj_d"),
	})

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_a"},
		{Scope: ScopeProjectRead, ResourceID: "proj_b"},
		{Scope: ScopeProjectRead, ResourceID: "proj_c"},
		{Scope: ScopeProjectRead, ResourceID: "proj_d"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{false, true, false, true}, matched)
}

func TestEngineFindMatched_returnsAllTrueWhenEnforcementDisabled(t *testing.T) {
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

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
		{Scope: ScopeProjectRead, ResourceID: "proj_456"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, true}, matched, "non-enforcing mode mirrors Filter's permissive behavior — every check passes")
}

func TestEngineFindMatched_emptyInputReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	matched, err := engine.FindMatched(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, matched)

	// Empty input must not emit a challenge log entry; confirm none ever lands.
	require.Never(t, func() bool {
		rows, err := chConn.Query(t.Context(), `
			SELECT count() FROM authz_challenges WHERE organization_id = ? AND operation = 'filter'
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "empty input must skip challenge logging")
}

func TestEngineFindMatched_missingGrantsReturnsError(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	_, err = engine.FindMatched(enterpriseSessionCtx(t), []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestEngineFindMatched_rejectsInvalidCheck(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	_, err = engine.FindMatched(ctx, []Check{{Scope: ScopeProjectRead, ResourceID: ""}})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestEngineFindMatched_logsSingleAggregateChallenge(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_allowed")})
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_allowed"},
		{Scope: ScopeProjectRead, ResourceID: "proj_denied"},
		{Scope: ScopeProjectRead, ResourceID: "proj_other"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, false}, matched)

	// A batched FindMatched must emit exactly one challenge log entry for
	// the whole input, not N per check — the per-check granularity lives in
	// the returned slice, not in the log table.
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
		var (
			count                    uint64
			outcome, reason          string
			candidateCnt, allowedCnt uint32
		)
		if err := rows.Scan(&count, &outcome, &reason, &candidateCnt, &allowedCnt); err != nil {
			return false
		}
		return count == 1 &&
			outcome == string(authzrepo.OutcomeAllow) &&
			reason == string(authzrepo.ReasonGrantMatched) &&
			candidateCnt == 3 && allowedCnt == 1
	}, 5*time.Second, 100*time.Millisecond)
}

// --- Engine.Evaluate tests ---

func TestEngineEvaluate_trueWhenGranted(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeChatRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestEngineEvaluate_falseWhenUnsatisfied(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestEngineEvaluate_trueWhenRBACDisabled(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(false), staticChallengeLogging(true), workos.NewStubClient())

	allowed, err := engine.Evaluate(enterpriseSessionCtx(t), ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestEngineEvaluate_errorsWhenGrantsMissing(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	allowed, err := engine.Evaluate(enterpriseSessionCtx(t), ChatReadCheck("proj_123"))
	require.False(t, allowed)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

// An unsatisfied Evaluate check is a routine visibility branch, not a denial —
// it must never emit an authz challenge, otherwise it would pollute the
// diagnostics UI with false "denied" scopes (the coupling AIS-305 removes).
func TestEngineEvaluate_neverLogsChallenge(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.False(t, allowed)

	require.Never(t, func() bool {
		rows, err := chConn.Query(t.Context(), `
			SELECT count() FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return false
		}
		return count > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "Evaluate must not emit any challenge log entry")
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

// TestPrepareContext_adminImpersonationGrantsAllScopes verifies that when a
// Speakeasy admin impersonates a customer org (IsAdmin + AdminOverride), the
// engine injects wildcard grants for every scope so that Require() calls
// succeed. Without this, the admin has no WorkOS membership in the target org
// and every endpoint returns 403.
func TestPrepareContext_adminImpersonationGrantsAllScopes(t *testing.T) {
	t.Parallel()

	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), nil, chConn, staticRBAC(true), staticChallengeLogging(true), workos.NewStubClient())

	// Build a context that looks like admin impersonation: enterprise account,
	// IsAdmin flag, and AdminOverride pointing at the target org.
	sessionID := "session_admin"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_customer",
		UserID:               "user_admin",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
		IsAdmin:              true,
	})
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "org_customer")

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok, "grants should be present in context after PrepareContext")
	require.NotEmpty(t, grants, "admin impersonation should produce non-empty grants")

	// Every scope should be satisfiable via Require.
	for _, scope := range []Scope{
		ScopeOrgRead, ScopeOrgAdmin,
		ScopeProjectRead, ScopeProjectWrite,
		ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect,
		ScopeEnvironmentRead, ScopeEnvironmentWrite,
	} {
		err := engine.Require(ctx, Check{Scope: scope, ResourceID: "org_customer"})
		require.NoError(t, err, "admin impersonation should satisfy scope %s", scope)
	}
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
