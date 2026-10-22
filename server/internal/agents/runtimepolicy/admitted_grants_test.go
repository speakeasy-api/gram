package runtimepolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func connectSelector(serverID, projectID string) authz.Selector {
	selector := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID}
	if projectID != "" {
		selector[authz.SelectorKeyProjectID] = projectID
	}
	return selector
}

func authCeiling(projectID string) authz.Grant {
	selector := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource}
	if projectID != "" {
		selector[authz.SelectorKeyProjectID] = projectID
	}
	return authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPAuth, Selector: selector, Inferred: false}
}

func noServers([]string) (map[string]string, error) { return map[string]string{}, nil }

func mustAdmit(t *testing.T, version DelegatedPolicyVersion, policy DelegatedPolicy, resolve mcpProjectResolver) []authz.Grant {
	t.Helper()
	grants, err := admittedGrants(version, policy, resolve)
	require.NoError(t, err)
	return grants
}

func TestAdmittedGrantsBoundProjectlessServerGrantsToTheServersProject(t *testing.T) {
	t.Parallel()

	legacy, err := NewDelegatedPolicy(DelegatedPolicyVersion4, []authz.Grant{
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "")),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-gone", "")),
	})
	require.NoError(t, err)
	calls := 0
	admitted := mustAdmit(t, DelegatedPolicyVersion4, legacy, func(ids []string) (map[string]string, error) {
		calls++
		require.ElementsMatch(t, []string{"server-1", "server-gone"}, ids)
		return map[string]string{"server-1": "project-q"}, nil
	})
	require.Equal(t, 1, calls, "every server is resolved in one lookup")
	require.Contains(t, admitted, authCeiling("project-q"), "a server grant reaches its server's project")
	require.NotContains(t, admitted, authCeiling(""), "never the whole organization")
	require.Len(t, admitted, len(legacy.RuntimeGrants())+1, "an unknown server reaches nothing")
}

func TestAdmittedGrantsPrepareLegacyPoliciesInPlace(t *testing.T) {
	t.Parallel()

	legacy, err := NewDelegatedPolicy(DelegatedPolicyVersion4, []authz.Grant{
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p")),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-2", "project-p")),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("*", "")),
		authz.NewGrant(authz.ScopeProjectRead, "project-p"),
	})
	require.NoError(t, err)
	admitted, err := admittedGrants(DelegatedPolicyVersion4, legacy, noServers)
	require.NoError(t, err)
	require.Contains(t, admitted, authCeiling("project-p"), "connecting in a project reaches that project's clients")
	require.Contains(t, admitted, authCeiling(""), "organization-wide connect reaches every client")
	auth := 0
	for _, grant := range admitted {
		if grant.Scope == authz.ScopeMCPAuth {
			auth++
		}
	}
	require.Equal(t, 2, auth, "one ceiling per reach")
	require.ElementsMatch(t, legacy.RuntimeGrants(), admitted[:len(legacy.RuntimeGrants())], "the stored policy itself is unchanged")

	noConnect, err := NewDelegatedPolicy(DelegatedPolicyVersion4, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-p")})
	require.NoError(t, err)
	require.Equal(t, noConnect.RuntimeGrants(), mustAdmit(t, DelegatedPolicyVersion4, noConnect, noServers), "no server access, no clients")

	current, err := NewDelegatedPolicy(CurrentDelegatedPolicyVersion, []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p"))})
	require.NoError(t, err)
	require.Equal(t, current.RuntimeGrants(), mustAdmit(t, CurrentDelegatedPolicyVersion, current, noServers), "a version 5 policy states its auth clients itself")

	for _, version := range []DelegatedPolicyVersion{DelegatedPolicyVersion1, DelegatedPolicyVersion2, DelegatedPolicyVersion3, DelegatedPolicyVersion4} {
		writer, err := NewDelegatedPolicy(version, []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPWrite, connectSelector("server-1", "project-p"))})
		require.NoError(t, err)
		require.Contains(t, mustAdmit(t, version, writer, noServers), authCeiling("project-p"), "write implies connect at version %d", version)
	}
}

func TestAdmitStoredPolicyFlagsOnlyDerivedCeilings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		grants  []authz.Grant
		derived bool
	}{
		{"connect", []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p"))}, true},
		{"no connect", []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-p")}, false},
	} {
		policy, err := NewDelegatedPolicy(DelegatedPolicyVersion4, tc.grants)
		require.NoError(t, err)
		raw, err := EncodeDelegatedPolicy(DelegatedPolicyVersion4, policy)
		require.NoError(t, err)
		_, derived, err := admitStoredPolicy(t.Context(), nil, "org-1", int32(DelegatedPolicyVersion4), raw)
		require.NoError(t, err)
		require.Equal(t, tc.derived, derived, tc.name)
	}
}

func TestIssuedPolicyVersionWaitsForAuthClientIssuers(t *testing.T) {
	t.Parallel()
	// Raising it stops admission deriving auth ceilings for new credentials.
	// Make the session, workload and device agent encoders state mcp:auth
	// first; see docs/rbac.md.
	require.Less(t, IssuedDelegatedPolicyVersion, DelegatedPolicyVersion5)
}

func TestLegacyWorkloadSessionsKeepSharedAuthClientUse(t *testing.T) {
	t.Parallel()

	f := newRuntimeBoundaryFixture(t, "workload")
	for _, principal := range []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()),
		urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID),
	} {
		seedSelectorGrant(t, f, principal, authz.ScopeMCPAuth, sharedClientSelector)
		seedSelectorGrant(t, f, principal, authz.ScopeMCPAuth, representedUserSelector)
	}
	authCtx, ok := contextvalues.GetAuthContext(f.requestContext)
	require.True(t, ok)
	workload, ok := contextvalues.AuthenticatedActor(f.requestContext)
	require.True(t, ok)
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, workload,
		storedCredential(t, DelegatedPolicyVersion4, "", authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p"))))

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, sharedClientUse("project-p")), "a version 4 workload session keeps its server's shared clients")
	requireDenied(t, f.engine.Require(prepared, sharedClientUse("project-q")), "but not another project's")
	personal := authz.MCPAuthClientCheck("client-a", authz.MCPAuthClientDimensions{ClientType: authz.AuthClientTypeRemoteSession, ProjectID: "project-p", ConnectionOwner: f.ownerUserID})
	requireDenied(t, f.engine.Require(prepared, personal), "a workload represents no user, so no personal connection")

	mandatory, err := f.engine.PrepareMandatory(ctx)
	require.NoError(t, err)
	require.True(t, mandatory.LegacyAuthCeiling())
}

func storedCredential(t *testing.T, version DelegatedPolicyVersion, authorizer string, grants ...authz.Grant) contextvalues.PrincipalCredential {
	t.Helper()
	policy, err := NewDelegatedPolicy(version, grants)
	require.NoError(t, err)
	raw, err := EncodeDelegatedPolicy(version, policy)
	require.NoError(t, err)
	return contextvalues.PrincipalCredential{AuthorizerUserID: authorizer, DelegatedGrants: raw, DelegatedGrantsVersion: int32(version)}
}

func credentialContext(t *testing.T, f credentialAdmissionFixture, version DelegatedPolicyVersion, grants ...authz.Grant) context.Context {
	t.Helper()
	return contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: f.organizationID,
	}, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), storedCredential(t, version, f.authorizerUserID, grants...))
}

func legacyCredentialContext(t *testing.T, f credentialAdmissionFixture, grants ...authz.Grant) context.Context {
	t.Helper()
	return credentialContext(t, f, DelegatedPolicyVersion4, grants...)
}

func sharedClientUse(project string) authz.Check {
	return authz.MCPAuthClientCheck("client-s", authz.MCPAuthClientDimensions{ClientType: authz.AuthClientTypeServiceAccount, ProjectID: project, ConnectionOwner: ""})
}

var sharedClientSelector = authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: "client-s"}

func TestLegacyCredentialsKeepTheirAuthClientUse(t *testing.T) {
	t.Parallel()

	f := newCredentialAdmissionFixture(t)
	for _, principal := range []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()),
		urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID),
		urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID),
	} {
		seedSelectorGrant(t, f, principal, authz.ScopeMCPAuth, sharedClientSelector)
		seedSelectorGrant(t, f, principal, authz.ScopeMCPAuth, representedUserSelector)
	}
	sharedUse := sharedClientUse
	personalUse := func(owner string) authz.Check {
		return authz.MCPAuthClientCheck("client-a", authz.MCPAuthClientDimensions{ClientType: authz.AuthClientTypeRemoteSession, ProjectID: "project-p", ConnectionOwner: owner})
	}

	connecting := legacyCredentialContext(t, f, authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p")))
	prepared, err := f.engine.PrepareContext(connecting)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, sharedUse("project-p")), "a version 4 credential keeps using its server's clients")
	requireDenied(t, f.engine.Require(prepared, sharedUse("project-q")), "but not another project's")
	require.NoError(t, f.engine.Require(prepared, personalUse(f.authorizerUserID)), "the authorizer's own connection stays usable")
	requireDenied(t, f.engine.Require(prepared, personalUse(f.ownerUserID)), "the ceiling never reaches another user's connection")

	mandatory, err := f.engine.PrepareMandatory(connecting)
	require.NoError(t, err)
	require.True(t, mandatory.LegacyAuthCeiling(), "the shadow can tell a derived ceiling from explicit grants")
	decision, err := mandatory.Decide(f.organizationID, sharedUse("project-p"))
	require.NoError(t, err)
	require.True(t, decision.Allowed, "the mandatory check reads the same ceiling")
	decision, err = mandatory.Decide(f.organizationID, sharedUse("project-q"))
	require.NoError(t, err)
	require.False(t, decision.Allowed)

	explicit, err := f.engine.PrepareMandatory(credentialContext(t, f, DelegatedPolicyVersion5, authz.NewGrantWithSelector(authz.ScopeMCPConnect, connectSelector("server-1", "project-p"))))
	require.NoError(t, err)
	require.False(t, explicit.LegacyAuthCeiling())
	decision, err = explicit.Decide(f.organizationID, sharedUse("project-p"))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a version 5 policy gets no ceiling")

	readOnly := legacyCredentialContext(t, f, authz.NewGrant(authz.ScopeProjectRead, f.projectID))
	prepared, err = f.engine.PrepareContext(readOnly)
	require.NoError(t, err)
	requireDenied(t, f.engine.Require(prepared, sharedUse("project-p")), "a credential that cannot connect gets no clients")
}
