package authz

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const otherOrganizationLog = "authz check names another organization than the caller's"

// An org check about another organization than the caller's still goes
// through for now, but it is reported so the call site can be fixed before
// such checks are refused. The call site states nothing: the organization is
// the check's own resource.
func TestCheckOrganization_AnotherOrganizationIsLoggedNotRefused(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, "org-one"), []Grant{
		NewGrant(ScopeOrgAdmin, WildcardResource),
		NewGrant(ScopeProjectRead, WildcardResource),
	})

	for name, check := range map[string]Check{
		"org check on the caller's organization": {Scope: ScopeOrgAdmin, ResourceID: "org-one"},
		"check on another kind of resource":      {Scope: ScopeProjectRead, ResourceID: "project-one"},
	} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, check), name)
		require.NotContains(t, logs.String(), otherOrganizationLog, name)
	}

	other := Check{Scope: ScopeOrgAdmin, ResourceID: "org-two"}

	logs.Reset()
	require.NoError(t, engine.Require(ctx, other))
	require.Contains(t, logs.String(), otherOrganizationLog)

	logs.Reset()
	allowed, err := engine.Can(ctx, other)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Contains(t, logs.String(), otherOrganizationLog)

	logs.Reset()
	require.NoError(t, engine.Require(ctx, AnyOf(other)))
	require.Equal(t, 1, strings.Count(logs.String(), otherOrganizationLog))

	logs.Reset()
	require.NoError(t, engine.RequireAnyUnblocked(ctx, other))
	require.Equal(t, 1, strings.Count(logs.String(), otherOrganizationLog))
}
