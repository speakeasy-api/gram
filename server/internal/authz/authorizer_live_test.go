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
	one := MCPCheck(ScopeMCPConnect, "server-one", project)
	two := MCPCheck(ScopeMCPConnect, "server-two", project)
	count := func() int { return strings.Count(logs.String(), authorizerWouldRefuseLog) }

	same := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: credential})
	narrowed := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{NewGrant(ScopeMCPConnect, "server-one")}})
	workload := ctxFor(RepresentedUserPolicy{UserID: "", Policy: nil})

	for name, ctx := range map[string]context.Context{"authorizer may do the same": same, "workload acts for nobody": workload} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, one, two), name)
		require.NoError(t, engine.RequireAny(ctx, one, two), name)
		allowed, err := engine.Can(ctx, AnyOf(one, two))
		require.NoError(t, err, name)
		require.True(t, allowed, name)
		ids, err := engine.Filter(ctx, []Check{one, two})
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

	logs.Reset()
	allowed, err := engine.Can(narrowed, two)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 1, count())

	logs.Reset()
	require.NoError(t, engine.RequireAny(narrowed, two, one))
	require.Zero(t, count(), "one alternative survives the authorizer's policy")

	logs.Reset()
	require.NoError(t, engine.RequireAny(narrowed, two))
	require.Equal(t, 1, count())

	logs.Reset()
	allowed, err = engine.Can(narrowed, AnyOf(two, one))
	require.NoError(t, err)
	require.True(t, allowed)
	require.Zero(t, count())

	logs.Reset()
	ids, err := engine.Filter(narrowed, []Check{one, two})
	require.NoError(t, err)
	require.Len(t, ids, 2, "the list is unchanged")
	require.Equal(t, 1, count(), "one line per request, however many rows")
}
