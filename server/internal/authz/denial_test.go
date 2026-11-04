package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

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
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "mcp_1", Dimensions: nil}))

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

func TestEngineEvaluateLoadedGrants_DoesNotRecord(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDenialRecorder(GrantsToContext(enterpriseSessionCtx(t), nil))

	// A probe of another principal's grants says nothing about the caller.
	require.Error(t, engine.EvaluateLoadedGrants(ctx, nil, Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "mcp_1", Dimensions: nil}))
	_, ok := RecordedDenial(ctx)
	require.False(t, ok)

	// Nor does it overwrite the caller's own refusal.
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	require.Error(t, engine.EvaluateLoadedGrants(ctx, nil, Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "mcp_1", Dimensions: nil}))

	denial, ok := RecordedDenial(ctx)
	require.True(t, ok)
	require.Equal(t, Denial{Scope: ScopeLogRead, ResourceKind: "project"}, denial)
}

func TestEngineRequireAny_RecordsTheLeastPrivilegedAlternative(t *testing.T) {
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
			checks: []Check{{Scope: ScopeMCPRead, ResourceKind: "", ResourceID: "mcp_1", Dimensions: nil}, {Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "mcp_1", Dimensions: nil}},
			want:   Denial{Scope: ScopeMCPConnect, ResourceKind: "mcp"},
			none:   false,
		},
		{
			name: "not an alternative a block refused",
			grants: []Grant{
				NewGrant(ScopeProjectRead, WildcardResource),
				NewGrantWithSelector(ScopeProjectBlockedRead, Selector{
					SelectorKeyResourceKind: ResourceKindProject,
					SelectorKeyResourceID:   projectID,
				}),
			},
			checks: []Check{orgCheck, projectCheck},
			want:   Denial{Scope: ScopeOrgRead, ResourceKind: "org"},
			none:   false,
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

			require.Error(t, engine.RequireAny(ctx, tt.checks...))

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
