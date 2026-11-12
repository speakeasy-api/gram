package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestRecordedDecisions_AbsentWithoutRecorder(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeLogRead, "proj_123")})

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	_, ok := RecordedDecisions(ctx)
	require.False(t, ok)
}

func TestRecordedDecisions_ZeroUntilAnEntryPointRuns(t *testing.T) {
	t.Parallel()
	ctx := WithDecisionRecorder(enterpriseSessionCtx(t))

	summary, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	require.Equal(t, 0, summary.Total())
	require.Empty(t, summary.Operations)
}

func TestRecordedDecisions_CountsAllowsAndDenials(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDecisionRecorder(GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeLogRead, "proj_123")}))

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	allowed, err := engine.Evaluate(ctx, Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil})
	require.NoError(t, err)
	require.False(t, allowed)
	_, err = engine.Filter(ctx, []Check{{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}})
	require.NoError(t, err)

	summary, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	require.Equal(t, 4, summary.Enforced)
	require.Equal(t, 0, summary.Skipped)
	require.Len(t, summary.Operations, 3)
	require.Equal(t, 2, summary.Operations[DecisionOperationRequire])
	require.Equal(t, 1, summary.Operations[DecisionOperationEvaluate])
	require.Equal(t, 1, summary.Operations[DecisionOperationFilter])
}

func TestRecordedDecisions_CountsSkippedEvaluation(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	// A signed-in caller with no session is one Require does not enforce.
	ctx := WithDecisionRecorder(contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	}))

	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)

	summary, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	require.Equal(t, 0, summary.Enforced)
	require.Equal(t, 2, summary.Skipped)
	require.Len(t, summary.Operations, 2)
	require.Equal(t, 1, summary.Operations[DecisionOperationRequire])
	require.Equal(t, 1, summary.Operations[DecisionOperationShouldEnforce])
}

func TestRecordedDecisions_ReturnsACopy(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDecisionRecorder(GrantsToContext(enterpriseSessionCtx(t), nil))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	first, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	first.Operations[DecisionOperationFilter] = 99

	second, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	require.Len(t, second.Operations, 1)
	require.Equal(t, 1, second.Operations[DecisionOperationRequire])
}

func TestRecordedDecisions_RequireAnyUnblockedCountsOnce(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := WithDecisionRecorder(GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeLogRead, "proj_123")}))

	require.NoError(t, engine.RequireAnyUnblocked(ctx, Check{Scope: ScopeLogRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil}))

	summary, ok := RecordedDecisions(ctx)
	require.True(t, ok)
	require.Equal(t, 1, summary.Total())
	require.Len(t, summary.Operations, 1)
	require.Equal(t, 1, summary.Operations[DecisionOperationRequireAnyUnblocked])
}
