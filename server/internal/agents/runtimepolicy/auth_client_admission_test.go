package runtimepolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var representedUserSelector = authz.Selector{
	authz.SelectorKeyResourceKind:    authz.ResourceKindMCPAuthClient,
	authz.SelectorKeyResourceID:      authz.WildcardResource,
	authz.SelectorKeyConnectionOwner: authz.SelectorValueRepresentedUser,
}

func seedSelectorGrant(t *testing.T, f credentialAdmissionFixture, principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	t.Helper()
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.organizationID, PrincipalUrn: principal, Scope: string(scope), Selectors: raw,
	})
	require.NoError(t, err)
}

// authClientCredentialContext is a stored v5 credential for the fixture's agent
// carrying the symbolic personal grant, owned by one user and authorized by another.
func authClientCredentialContext(t *testing.T, f credentialAdmissionFixture) context.Context {
	t.Helper()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedSelectorGrant(t, f, agent, authz.ScopeMCPAuth, representedUserSelector)
	seedSelectorGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.ownerUserID), authz.ScopeMCPAuth, representedUserSelector)

	policy, err := NewDelegatedPolicy(CurrentDelegatedPolicyVersion, []authz.Grant{
		authz.NewGrant(authz.ScopeProjectRead, f.projectID),
		authz.NewGrantWithSelector(authz.ScopeMCPAuth, representedUserSelector),
	})
	require.NoError(t, err)
	raw, err := EncodeDelegatedPolicy(CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	return contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: f.organizationID,
	}, agent, contextvalues.PrincipalCredential{
		AuthorizerUserID:       f.authorizerUserID,
		DelegatedGrants:        raw,
		DelegatedGrantsVersion: int32(CurrentDelegatedPolicyVersion),
	})
}

func connectionCheck(owner string) authz.Check {
	return authz.MCPAuthClientCheck("client-a", authz.MCPAuthClientDimensions{
		ClientType: authz.AuthClientTypeRemoteSession, ProjectID: "project-p", ConnectionOwner: owner,
	})
}

func requireDenied(t *testing.T, err error, msg string) {
	t.Helper()
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied, msg)
	require.Equal(t, oops.CodeForbidden, denied.Code, msg)
}

func TestAgentAuthClientUseFollowsTheAuthorizer(t *testing.T) {
	t.Parallel()

	f := newCredentialAdmissionFixture(t)
	ctx := authClientCredentialContext(t, f)

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	requireDenied(t, f.engine.Require(prepared, connectionCheck(f.authorizerUserID)), "the authorizer holds no mcp:auth yet")
	require.NoError(t, f.engine.Require(prepared, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}), "other scopes ignore the authorizer's policy")

	seedSelectorGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID), authz.ScopeMCPAuth, representedUserSelector)
	prepared, err = f.engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, f.engine.Require(prepared, connectionCheck(f.authorizerUserID)), "the owner's symbolic grant binds to the authorizer")
	requireDenied(t, f.engine.Require(prepared, connectionCheck(f.ownerUserID)), "never the agent owner's connection")
}

func TestAgentAuthClientUseEndsWhenTheAuthorizerLeaves(t *testing.T) {
	t.Parallel()

	f := newCredentialAdmissionFixture(t)
	ctx := authClientCredentialContext(t, f)
	seedSelectorGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.authorizerUserID), authz.ScopeMCPAuth, representedUserSelector)
	require.NoError(t, orgrepo.New(f.db).DeleteOrganizationUserRelationship(t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: f.organizationID, UserID: conv.ToPGText(f.authorizerUserID),
	}))

	prepared, err := f.engine.PrepareContext(ctx)
	require.NoError(t, err, "a stored policy still admits as before")
	require.NoError(t, f.engine.Require(prepared, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: f.projectID}))
	requireDenied(t, f.engine.Require(prepared, connectionCheck(f.authorizerUserID)), "no auth client is usable for a departed authorizer")
}
