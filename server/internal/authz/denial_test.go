package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestEngineRequire_RecordsTheDeniedScope(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), nil))

	err := engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	// The response body stays the generic refusal; the detail travels beside it.
	require.Equal(t, "permission denied", oopsErr.Error())

	denial, ok := RecordedDenial(ctx)
	require.True(t, ok)
	require.Equal(t, Denial{Scope: ScopeLogRead, ResourceKind: "project"}, denial)
}

func TestEngineRequire_RecordsNothingWhenAllowed(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeLogRead, "proj_123")}))

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	_, ok := RecordedDenial(ctx)
	require.False(t, ok)
}

func TestEngineRequire_LastDenialWins(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), nil))

	// A handler probes one scope, swallows the refusal, then fails on another:
	// the explanation must match the refusal that produced the response.
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	require.Error(t, engine.Require(ctx, MCPServer(MCPServerIDFrom("mcp_1"), ProjectIDFrom(testProjectID)).Connect()))

	denial, ok := RecordedDenial(ctx)
	require.True(t, ok)
	require.Equal(t, Denial{Scope: ScopeMCPConnect, ResourceKind: "mcp"}, denial)
}

func TestEngineRequire_RecordsNothingWhenABlockRefuses(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	// The caller already holds project:write everywhere; a block on one
	// project refuses it there. Naming project:write would send them after a
	// grant they have.
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), []Grant{
		NewGrant(ScopeProjectWrite, WildcardResource),
		NewGrantWithSelector(ScopeProjectBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindProject,
			SelectorKeyResourceID:   projectID,
		}),
	}))

	require.Error(t, engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: projectID, Dimensions: nil}))

	_, ok := RecordedDenial(ctx)
	require.False(t, ok)
}

func TestDecideWithoutChallengesOnLoadedGrants_DoesNotRecord(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), nil))

	// A probe of another principal's grants says nothing about the caller.
	require.Error(t, engine.EvaluateLoadedGrants(ctx, nil, MCPServer(MCPServerIDFrom("mcp_1"), ProjectIDFrom(testProjectID)).Connect()))
	_, ok := RecordedDenial(ctx)
	require.False(t, ok)

	// Nor does it overwrite the caller's own refusal.
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	require.Error(t, engine.EvaluateLoadedGrants(ctx, nil, MCPServer(MCPServerIDFrom("mcp_1"), ProjectIDFrom(testProjectID)).Connect()))

	denial, ok := RecordedDenial(ctx)
	require.True(t, ok)
	require.Equal(t, Denial{Scope: ScopeLogRead, ResourceKind: "project"}, denial)
}

func TestEngineEither_RecordsTheLeastPrivilegedAlternative(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	orgCheck := Check{Scope: ScopeOrgRead, ResourceKind: "", ResourceID: "org_123", Dimensions: nil}
	projectCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: projectID, Dimensions: nil}

	tests := []struct {
		name   string
		grants []Grant
		checks []Check
		want   Denial
		none   bool
	}{
		{
			name:   "a project scope over an organization scope, whatever the order",
			grants: nil,
			checks: []Check{orgCheck, projectCheck},
			want:   Denial{Scope: ScopeProjectRead, ResourceKind: "project"},
			none:   false,
		},
		{
			name:   "the scope the other alternative implies",
			grants: nil,
			checks: []Check{MCPServer(MCPServerIDFrom("mcp_1"), ProjectIDFrom(testProjectID)).Write(), MCPServer(MCPServerIDFrom("mcp_1"), ProjectIDFrom(testProjectID)).Read()},
			want:   Denial{Scope: ScopeMCPRead, ResourceKind: "mcp"},
			none:   false,
		},
		{
			name: "nothing when a block refuses the group",
			grants: []Grant{
				NewGrant(ScopeProjectRead, WildcardResource),
				NewGrantWithSelector(ScopeProjectBlockedRead, Selector{
					SelectorKeyResourceKind: ResourceKindProject,
					SelectorKeyResourceID:   projectID,
				}),
			},
			checks: []Check{orgCheck, projectCheck},
			want:   Denial{Scope: "", ResourceKind: ""},
			none:   true,
		},
		{
			name: "nothing when every alternative is blocked",
			grants: []Grant{
				NewGrant(ScopeProjectRead, WildcardResource),
				NewGrantWithSelector(ScopeProjectBlockedRead, Selector{
					SelectorKeyResourceKind: ResourceKindProject,
					SelectorKeyResourceID:   projectID,
				}),
			},
			checks: []Check{projectCheck},
			want:   Denial{Scope: "", ResourceKind: ""},
			none:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
			ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), tt.grants))

			require.Error(t, engine.Require(ctx, Either(tt.checks...)))

			denial, ok := RecordedDenial(ctx)
			require.Equal(t, !tt.none, ok)
			require.Equal(t, tt.want, denial)
		})
	}
}

func TestRecordedDenial_WithoutARecorder(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), nil)

	require.Error(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	_, ok := RecordedDenial(ctx)
	require.False(t, ok)
}

func TestDecideReason_WhenRBACIsNotEnforced(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"})
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce, "a sessionless context skips RBAC")

	valid := MCPServer(MCPServerIDFrom("toolset_1"), ProjectIDFrom(authProjectP)).Connect()
	invalid := Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "", Dimensions: nil}
	authClient := MCPAuthClientCheck("ac_1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: authProjectP, ConnectionOwner: "", UpstreamClient: ""})

	reasons, err := decisionReasons(ctx, engine, []Check{valid, authClient})
	require.NoError(t, err, "unenforced callers get the unenforced result")
	require.Equal(t, []Reason{ReasonNone, ReasonNotGranted}, reasons)

	_, err = decisionReasons(ctx, engine, []Check{valid, invalid})
	require.ErrorIs(t, err, ErrInvalidCheck, "in tests an invalid check fails even when RBAC is skipped")
}

// A call refused only because the caller's grant names another disposition
// than read_only is reported as a disposition refusal, even though a missing
// key never matches on a decision.
func TestDecideReason_OtherDispositionIsReported(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeMCPConnect, "toolset_1")
	selector[SelectorKeyDisposition] = DispositionIdempotent
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)})

	call := MCPServer(MCPServerIDFrom("toolset_1"), ProjectIDFrom(authProjectP)).ToolCall("delete_all", DispositionDestructive)
	reasons, err := decisionReasons(ctx, engine, []Check{call})
	require.NoError(t, err)
	require.Equal(t, []Reason{ReasonDisposition}, reasons)
}

// A call refused because it names no tool while the grant is limited to one
// is not a disposition refusal: no other disposition would allow it.
func TestDecideReason_MissingToolIsNotADispositionRefusal(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeMCPConnect, "toolset_1")
	selector[SelectorKeyTool] = "search"
	selector[SelectorKeyDisposition] = DispositionIdempotent
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)})

	call := MCPServer(MCPServerIDFrom("toolset_1"), ProjectIDFrom(authProjectP)).ToolCall("", DispositionDestructive)
	reasons, err := decisionReasons(ctx, engine, []Check{call})
	require.NoError(t, err)
	require.Equal(t, []Reason{ReasonNotGranted}, reasons)
}

// A tool call recorded as a denial is rebuilt as a tool call, so a grant that
// covers the original call also covers the rebuilt one.
func TestRecordedCheckRebuildsToolCalls(t *testing.T) {
	t.Parallel()

	grant := NewGrantWithSelector(ScopeMCPConnect, Selector{
		SelectorKeyResourceKind: "mcp",
		SelectorKeyResourceID:   "server-one",
		SelectorKeyTool:         "search",
	})
	dimensions := map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}

	recorded := RecordedCheck(ScopeMCPConnect, "", "server-one", dimensions)
	require.True(t, recorded.isToolCall())
	allowed, err := GrantsAuthorize([]Grant{grant}, recorded)
	require.NoError(t, err)
	require.True(t, allowed)

	other := RecordedCheck(ScopeMCPConnect, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "delete", SelectorKeyDisposition: DispositionDestructive})
	allowed, err = GrantsAuthorize([]Grant{grant}, other)
	require.NoError(t, err)
	require.False(t, allowed)
}

// Only connect has tool calls: a recorded view check that names a tool is
// still refused as invalid, and a recorded connection stays a connection.
func TestRecordedCheckKeepsOtherChecksAsTheyWere(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	read := RecordedCheck(ScopeMCPRead, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"})
	_, err := GrantsAuthorize(root, read)
	require.ErrorIs(t, err, ErrInvalidCheck)

	connection := RecordedCheck(ScopeMCPConnect, "", "server-one", map[string]string{SelectorKeyProjectID: "project-one"})
	require.False(t, connection.isToolCall())
	allowed, err := GrantsAuthorize(root, connection)
	require.NoError(t, err)
	require.True(t, allowed)
}
