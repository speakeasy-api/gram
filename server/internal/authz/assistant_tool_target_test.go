package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestExplicitAssistantToolTargetPreservesAuthorization(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	connect := MCPCheck(ScopeMCPConnect, "toolset_1", "project_1")
	allow := []Grant{NewGrant(ScopeMCPConnect, WildcardResource)}
	for _, kind := range []MandatoryCaller{MandatoryCallerAgent, MandatoryCallerWorkload} {
		for _, allowed := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: " denied", true: " allowed"}[allowed], func(t *testing.T) {
				t.Parallel()
				policy := allow
				if !allowed {
					policy = nil
				}
				ctx := principalPolicyTestContext(t, allow, policy, allow)
				if kind == MandatoryCallerWorkload {
					auth := new(contextvalues.AuthContext)
					auth.ActiveOrganizationID = "org_123"
					actor := urn.NewWorkloadPrincipal(uuid.New(), "test-workload")
					ctx = contextvalues.WithAuthenticatedActor(t.Context(), auth, actor)
					ctx = admittedPoliciesToContext(ctx, RepresentedUserPolicy{UserID: "", Policy: nil}, false, allow, policy, allow)
				}
				assertToolTargetAuthorizationUnchanged(t, engine, ctx, connect, kind, allowed)
			})
		}
	}
	sessionlessAuth := new(contextvalues.AuthContext)
	sessionlessAuth.ActiveOrganizationID = "org_123"
	sessionlessAuth.UserID = "user_123"
	sessionlessAuth.AccountType = "enterprise"
	sessionless := contextvalues.SetAuthContext(t.Context(), sessionlessAuth)
	assertToolTargetAuthorizationUnchanged(t, engine, sessionless, connect, MandatoryCallerUser, false)
	require.NoError(t, engine.Require(sessionless, connect))
	selected := contextvalues.SetAssistantToolTarget(sessionless, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	require.NoError(t, engine.Require(selected, connect), "target selection must not turn on session-less RBAC enforcement")
	require.Equal(t, "user_123", loadedGrantsUser(selected), "target selection must not erase the represented user")
}

func assertToolTargetAuthorizationUnchanged(t *testing.T, engine *Engine, before context.Context, check Check, kind MandatoryCaller, allowed bool) {
	t.Helper()
	target := contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}
	after := contextvalues.SetAssistantToolTarget(before, target)
	original, err := engine.PrepareMandatory(before)
	require.NoError(t, err)
	selected, err := engine.PrepareMandatory(after)
	require.NoError(t, err)
	require.Equal(t, kind, original.Caller())
	require.Equal(t, original.Caller(), selected.Caller())
	require.Equal(t, original.PrincipalID(), selected.PrincipalID())
	decisionBefore, err := engine.AuthorizeMandatory(before, check)
	require.NoError(t, err)
	decisionAfter, err := engine.AuthorizeMandatory(after, check)
	require.NoError(t, err)
	require.Equal(t, allowed, decisionBefore.Allowed)
	require.Equal(t, decisionBefore, decisionAfter)
	require.Equal(t, engine.Require(before, check) == nil, engine.Require(after, check) == nil)
	require.Equal(t, enforcedWithoutSession(before), enforcedWithoutSession(after))
	require.Equal(t, loadedGrantsUser(before), loadedGrantsUser(after))
	_, authenticatedAssistant := contextvalues.GetAssistantPrincipal(after)
	require.False(t, authenticatedAssistant)
	got, ok := contextvalues.GetAssistantToolTarget(after)
	require.True(t, ok)
	require.Equal(t, target, got)
}
