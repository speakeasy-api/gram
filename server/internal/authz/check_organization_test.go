package authz

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const otherOrganizationLog = "authz check names another organization than the caller's"

func TestCheckOrganization_StatedOrganization(t *testing.T) {
	t.Parallel()

	_, named := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}.statedOrganization()
	require.False(t, named, "a check that was told nothing names no organization")

	organization, named := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}.InOrganization(OrgIDFrom("org-one")).statedOrganization()
	require.True(t, named)
	require.Equal(t, "org-one", organization)

	organization, named = Check{Scope: ScopeOrgAdmin, ResourceID: "org-one"}.statedOrganization()
	require.True(t, named, "an org check is about its organization")
	require.Equal(t, "org-one", organization)
}

func TestCheckOrganization_InvalidStatementsAreRefusedEvenForRoot(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	for name, check := range map[string]Check{
		"absent organization":                 Check{Scope: ScopeProjectRead, ResourceID: "project-one"}.InOrganization(OrgIDFrom("")),
		"wildcard organization":               Check{Scope: ScopeProjectRead, ResourceID: "project-one"}.InOrganization(OrgIDFrom(WildcardResource)),
		"org check placed in a different org": Check{Scope: ScopeOrgAdmin, ResourceID: "org-one"}.InOrganization(OrgIDFrom("org-two")),
	} {
		allowed, err := GrantsAuthorize(root, check)
		require.ErrorIs(t, err, ErrInvalidCheck, name)
		require.False(t, allowed, name)
	}

	allowed, err := GrantsAuthorize(root, Check{Scope: ScopeOrgAdmin, ResourceID: "org-one"}.InOrganization(OrgIDFrom("org-one")))
	require.NoError(t, err)
	require.True(t, allowed)
}

// A check about another organization than the caller's still goes through
// for now, but it is reported so the call site can be fixed before such
// checks are refused.
func TestCheckOrganization_AnotherOrganizationIsLoggedNotRefused(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, "org-one"), []Grant{
		NewGrant(ScopeOrgAdmin, WildcardResource),
		NewGrant(ScopeProjectRead, WildcardResource),
	})
	project := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}

	for name, check := range map[string]Check{
		"org check on the caller's organization": {Scope: ScopeOrgAdmin, ResourceID: "org-one"},
		"resource in the caller's organization":  project.InOrganization(OrgIDFrom("org-one")),
		"resource with no organization stated":   project,
	} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, check), name)
		require.NotContains(t, logs.String(), otherOrganizationLog, name)
	}

	for name, check := range map[string]Check{
		"org check on another organization": {Scope: ScopeOrgAdmin, ResourceID: "org-two"},
		"resource in another organization":  project.InOrganization(OrgIDFrom("org-two")),
	} {
		logs.Reset()
		require.NoError(t, engine.Require(ctx, check), name)
		require.Contains(t, logs.String(), otherOrganizationLog, name)

		logs.Reset()
		allowed, err := engine.Can(ctx, check)
		require.NoError(t, err, name)
		require.True(t, allowed, name)
		require.Contains(t, logs.String(), otherOrganizationLog, name)

		logs.Reset()
		require.NoError(t, engine.Require(ctx, AnyOf(check)), name)
		require.Contains(t, logs.String(), otherOrganizationLog, name)
	}
}
