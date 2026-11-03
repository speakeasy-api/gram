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

func TestRecordedDenial_WithoutARecorder(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), nil)

	require.Error(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	_, ok := RecordedDenial(ctx)
	require.False(t, ok)
}
