package authz

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// A credential that acts for a user is still allowed what that user may no
// longer do, for now, but each such request is reported so the impact is
// known before the user's live policy bounds every check.
func TestAuthorizerLivePolicy_ReportsWhatItWouldRefuse(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())

	const project = "project-one"
	connectAll := NewGrant(ScopeMCPConnect, WildcardResource)
	credential := []Grant{connectAll, NewGrant(ScopeProjectRead, WildcardResource)}
	ctxFor := func(authorizer RepresentedUserPolicy) context.Context {
		return admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), authorizer, false, credential, credential, credential)
	}
	one := MCPServer(MCPServerIDFrom("server-one"), ProjectIDFrom(project)).Connect()
	two := MCPServer(MCPServerIDFrom("server-two"), ProjectIDFrom(project)).Connect()
	count := func() int { return strings.Count(logs.String(), authorizerWouldRefuseLog) }

	same := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: credential})
	narrowed := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{NewGrant(ScopeMCPConnect, "server-one")}})
	workload := ctxFor(RepresentedUserPolicy{UserID: "", Policy: nil})

	for name, ctx := range map[string]context.Context{"authorizer may do the same": same, "workload acts for nobody": workload} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, one, two), name)
		require.NoError(t, engine.Require(ctx, Either(one, two)), name)
		allowed, err := allAllowed(ctx, engine, Either(one, two))
		require.NoError(t, err, name)
		require.True(t, allowed, name)
		ids, err := allowedIDs(ctx, engine, []Check{one, two})
		require.NoError(t, err, name)
		require.Len(t, ids, 2, name)
		require.Zero(t, count(), name)
	}

	logs.Reset()
	require.NoError(t, engine.Require(narrowed, one))
	require.Zero(t, count(), "the authorizer may connect to server one")

	logs.Reset()
	require.NoError(t, engine.Require(narrowed, two), "the decision is unchanged")
	require.Equal(t, 1, count())
	for _, want := range []string{"user_u", "server-two", string(ScopeMCPConnect)} {
		require.Contains(t, logs.String(), want, "the warning names the user, the resource and the scope")
	}
	require.NotContains(t, logs.String(), "server-one")

	logs.Reset()
	allowed, err := allAllowed(narrowed, engine, two)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 1, count())

	logs.Reset()
	require.NoError(t, engine.Require(narrowed, Either(two, one)))
	require.Zero(t, count(), "one alternative survives the authorizer's policy")

	logs.Reset()
	require.NoError(t, engine.Require(narrowed, Either(two)))
	require.Equal(t, 1, count())

	logs.Reset()
	allowed, err = allAllowed(narrowed, engine, Either(two, one))
	require.NoError(t, err)
	require.True(t, allowed)
	require.Zero(t, count())

	logs.Reset()
	ids, err := allowedIDs(narrowed, engine, []Check{one, two})
	require.NoError(t, err)
	require.Len(t, ids, 2, "the list is unchanged")
	require.Equal(t, 1, count(), "one line per call, however many rows")
}

// A block in the authorizer's policy refuses an Either group as it would in
// the credential's own policy. MCP tool and connect decisions made through
// admission are reported the same way.
func TestAuthorizerLivePolicy_ReportsGroupVetoesAndAdmissionDecisions(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())

	const project = "project-one"
	connectAll := NewGrant(ScopeMCPConnect, WildcardResource)
	credential := []Grant{connectAll, NewGrant(ScopeProjectRead, WildcardResource)}
	ctxFor := func(authorizer RepresentedUserPolicy) context.Context {
		return admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), authorizer, false, credential, credential, credential)
	}
	one := MCPServer(MCPServerIDFrom("server-one"), ProjectIDFrom(project)).Connect()
	two := MCPServer(MCPServerIDFrom("server-two"), ProjectIDFrom(project)).Connect()
	count := func() int { return strings.Count(logs.String(), authorizerWouldRefuseLog) }

	blocksTwo := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{connectAll, NewGrant(ScopeMCPBlockedConnect, "server-two")}})

	logs.Reset()
	allowed, err := allAllowed(blocksTwo, engine, Either(two, one))
	require.NoError(t, err)
	require.True(t, allowed, "the decision is unchanged")
	require.Equal(t, 1, count(), "the authorizer's block on server two vetoes the group")
	require.Contains(t, logs.String(), "server-two")

	logs.Reset()
	require.NoError(t, engine.Require(blocksTwo, Either(two, one)))
	require.Equal(t, 1, count(), "Require refuses a blocked alternative, so the warning does too")

	narrowed := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{NewGrant(ScopeMCPConnect, "server-one")}})

	logs.Reset()
	decision, err := engine.AuthorizeMandatory(narrowed, one)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Zero(t, count())

	logs.Reset()
	decision, err = engine.AuthorizeMandatory(narrowed, two)
	require.NoError(t, err)
	require.True(t, decision.Allowed, "the decision is unchanged")
	require.Equal(t, 1, count(), "admission decisions are reported too")
	require.Contains(t, logs.String(), "server-two")

	logs.Reset()
	prepared, err := engine.PrepareMandatory(narrowed)
	require.NoError(t, err)
	for range 3 {
		decision, err = prepared.Decide(prepared.OrganizationID(), two)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	}
	require.Equal(t, 1, count(), "one prepared authorization reports once")
}

func TestAuthorizerLivePolicy_ReportsTheAlternativeTheCredentialAllowed(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())

	const project = "project-one"
	credential := []Grant{NewGrant(ScopeMCPConnect, "server-two"), NewGrant(ScopeProjectRead, WildcardResource)}
	authorizer := RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{NewGrant(ScopeMCPConnect, "server-one")}}
	ctx := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), authorizer, false, credential, credential, credential)
	one := MCPServer(MCPServerIDFrom("server-one"), ProjectIDFrom(project)).Connect()
	two := MCPServer(MCPServerIDFrom("server-two"), ProjectIDFrom(project)).Connect()

	require.NoError(t, engine.Require(ctx, Either(one, two)))
	require.Equal(t, 1, strings.Count(logs.String(), authorizerWouldRefuseLog))
	require.Contains(t, logs.String(), "server-two")
	require.NotContains(t, logs.String(), "server-one")
}
