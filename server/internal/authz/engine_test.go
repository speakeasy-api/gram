package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func staticChallengeLogging(enabled bool) ChallengeLoggingEnabled {
	return func(context.Context, string) (bool, error) {
		return enabled, nil
	}
}

func TestEngineRequire_requiresAuthContext(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

	err := engine.Require(t.Context(), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestEngineRequire_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), nil)

	err := engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Equal(t, "permission denied", oopsErr.Error())
}

func TestEngineRequire_mapsMissingGrantsToUnexpected(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

	err := engine.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestEvaluateLoadedGrants_doesNotConsultShouldEnforce(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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

func TestEngineRequireAny_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, "tool_a")})

	err := engine.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_b"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_c"},
	)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_returnsAllowedSubset(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_allowed"},
		{Scope: ScopeProjectRead, ResourceID: "proj_denied"},
		{Scope: ScopeProjectRead, ResourceID: "proj_other"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_allowed"}, resourceIDs)

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, orgID, message.GetOrganizationId())
	require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
	require.Equal(t, string(authzrepo.OutcomeAllow), message.GetOutcome())
	require.Equal(t, string(authzrepo.ReasonGrantMatched), message.GetReason())
	require.Equal(t, uint32(3), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
	require.Len(t, message.GetRequestedChecks(), 3)
}

func TestEngineFilter_logsDenyWhenNoMatches(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_other")})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_a"},
		{Scope: ScopeProjectRead, ResourceID: "proj_b"},
	})
	require.NoError(t, err)
	require.Empty(t, resourceIDs)

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, string(authzrepo.OutcomeDeny), message.GetOutcome())
	require.Equal(t, string(authzrepo.ReasonScopeUnsatisfied), message.GetReason())
	require.Equal(t, uint32(2), message.GetFilterCandidateCount())
	require.Zero(t, message.GetFilterAllowedCount())
}

func TestEngineFilter_skipsLogWhenNoChecks(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	resourceIDs, err := engine.Filter(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, resourceIDs)

	count, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestEngineRequire_projectWriteBlocklistBlocksAccess(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectWrite, WildcardResource),
		NewGrantWithSelector(ScopeProjectBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindProject,
			SelectorKeyResourceID:   projectID,
		}),
	})

	err := engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "project_other", Dimensions: nil})
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: projectID, Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_mcpWriteBlocklistExcludesProjectScopedResources(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope: ScopeMCPConnect,
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope: ScopeMCPConnect,
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope: ScopeMCPConnect,
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope: ScopeMCPConnect,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
	})

	// Tool call on server in proj_A should pass.
	err := engine.Require(ctx, MCPToolCallCheck("serverX", MCPToolCallDimensions{
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		{
			Scope: ScopeMCPRead,
			Selector: Selector{
				SelectorKeyResourceKind: ResourceKindMCP,
				SelectorKeyResourceID:   WildcardResource,
				SelectorKeyProjectID:    "proj_A",
			},
		},
	})

	// mcp:read check for server in proj_A passes.
	err := engine.Require(ctx, MCPCheck(ScopeMCPRead, "serverX", "proj_A"))
	require.NoError(t, err)

	// mcp:read check for server in proj_B fails.
	err = engine.Require(ctx, MCPCheck(ScopeMCPRead, "serverY", "proj_B"))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineFilter_projectAndServerGrantsCombine(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		// Project-scoped grant for proj_A
		{
			Scope: ScopeMCPConnect,
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	err := engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: ""})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestEngineRequire_requiresChecks(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	err := engine.Require(ctx)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrNoChecks)
}

func TestEngineRequire_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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

	err := engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestEngineFilter_enforcesForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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

	ctx = GrantsToContext(ctx, []Grant{NewGrant(ScopeProjectRead, "proj_123")})
	resourceIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
		{Scope: ScopeProjectRead, ResourceID: "proj_456"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123"}, resourceIDs)
}

func TestEngineFindMatched_returnsParallelBools(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
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

func TestEngineFindMatched_emptyInputReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	seedOrganization(t, t.Context(), conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	matched, err := engine.FindMatched(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, matched)

	count, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestEngineFindMatched_missingGrantsReturnsError(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

	_, err := engine.FindMatched(enterpriseSessionCtx(t), []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_123"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestEngineFindMatched_rejectsInvalidCheck(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	_, err := engine.FindMatched(ctx, []Check{{Scope: ScopeProjectRead, ResourceID: ""}})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestEngineFindMatched_logsSingleAggregateChallenge(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, "proj_allowed")})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	matched, err := engine.FindMatched(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj_allowed"},
		{Scope: ScopeProjectRead, ResourceID: "proj_denied"},
		{Scope: ScopeProjectRead, ResourceID: "proj_other"},
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, false}, matched)

	// A batched FindMatched must emit exactly one challenge log entry for
	// the whole input, not N per check — the per-check granularity lives in
	// the returned slice, not in the outbox.
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, string(authzrepo.OutcomeAllow), message.GetOutcome())
	require.Equal(t, string(authzrepo.ReasonGrantMatched), message.GetReason())
	require.Equal(t, uint32(3), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
}

// --- Engine.Evaluate tests ---

func TestEngineEvaluate_trueWhenGranted(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeChatRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestEngineEvaluate_falseWhenUnsatisfied(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestEngineEvaluate_errorsWhenGrantsMissing(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

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
	conn := newTestDB(t)
	seedOrganization(t, t.Context(), conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{NewGrant(ScopeProjectRead, WildcardResource)})

	allowed, err := engine.Evaluate(ctx, ChatReadCheck("proj_123"))
	require.NoError(t, err)
	require.False(t, allowed)

	count, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
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
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())

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

	ctx, err := engine.PrepareContext(ctx)
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
		ScopeSkillRead, ScopeSkillWrite,
	} {
		err := engine.Require(ctx, Check{Scope: scope, ResourceID: "org_customer"})
		require.NoError(t, err, "admin impersonation should satisfy scope %s", scope)
	}
}

func TestEngineRequire_skillReadIsProjectScoped(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeSkillRead, "project_a")})

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "project_a", Dimensions: nil}))
	err := engine.Require(ctx, Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "project_b", Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequire_skillWriteImpliesReadButReadDoesNotImplyWrite(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	writeCtx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeSkillWrite, "project_a")})
	require.NoError(t, engine.Require(writeCtx, Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "project_a", Dimensions: nil}))

	readCtx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeSkillRead, "project_a")})
	err := engine.Require(readCtx, Check{Scope: ScopeSkillWrite, ResourceKind: "", ResourceID: "project_a", Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequire_projectScopesDoNotImplySkillScopes(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectRead, "project_a"),
		NewGrant(ScopeProjectWrite, "project_a"),
	})

	err := engine.Require(ctx, Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "project_a", Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEngineRequire_skillBlocklistExpansion(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	blockedWriteCtx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeSkillWrite, WildcardResource),
		NewGrant(ScopeSkillBlockedWrite, "project_a"),
	})

	require.NoError(t, engine.Require(blockedWriteCtx, Check{Scope: ScopeSkillWrite, ResourceKind: "", ResourceID: "project_b", Dimensions: nil}))
	require.NoError(t, engine.Require(blockedWriteCtx, Check{Scope: ScopeSkillRead, ResourceKind: "", ResourceID: "project_a", Dimensions: nil}))
	err := engine.Require(blockedWriteCtx, Check{Scope: ScopeSkillWrite, ResourceKind: "", ResourceID: "project_a", Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	blockedReadCtx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeSkillWrite, WildcardResource),
		NewGrant(ScopeSkillBlockedRead, "project_a"),
	})
	for _, scope := range []Scope{ScopeSkillRead, ScopeSkillWrite} {
		err = engine.Require(blockedReadCtx, Check{Scope: scope, ResourceKind: "", ResourceID: "project_a", Dimensions: nil})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	}
}

func TestCanUseOverride_devPlusAdmin(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_devPlusNonAdmin(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, false, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_prodPlusAdmin(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

func TestCanUseOverride_prodPlusNonAdmin(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := scopeOverrideCtx(t, false, "pro")

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

// A Platform MCP call carries a real user and no browser session. Enforcement
// keys on the session for every other human-driven surface, so without the
// surface carve-out every scope check an OAuth-authenticated agent makes would
// pass regardless of what its user is allowed to do.
func TestShouldEnforce_platformMCPSurfaceEnforcesWithoutASession(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "pro",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	}

	unmarked, err := engine.ShouldEnforce(contextvalues.SetAuthContext(t.Context(), authCtx))
	require.NoError(t, err)
	require.False(t, unmarked, "a session-less call from an unmarked surface stays unenforced")

	marked, err := engine.ShouldEnforce(contextvalues.SetAuthContext(
		contextvalues.SetActingSurface(t.Context(), contextvalues.ActingSurfacePlatformMCP),
		authCtx,
	))
	require.NoError(t, err)
	require.True(t, marked)
}
