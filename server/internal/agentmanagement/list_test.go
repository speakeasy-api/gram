package agentmanagement

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestListAgentsReusesOwnerProfileWithinRequest(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	createAgent(t, conn, "org-a", "caller", "First")
	createAgent(t, conn, "org-a", "caller", "Second")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.NotNil(t, listed[0].OwnerProfile)
	require.Same(t, listed[0].OwnerProfile, listed[1].OwnerProfile)
	require.Equal(t, "caller", listed[0].OwnerProfile.DisplayName)
	require.True(t, listed[0].Permissions.Read)
	require.True(t, listed[1].Permissions.Read)

	// A subsequent request must load a fresh profile rather than retaining
	// tenant membership/profile data on the shared service.
	again, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, again, 2)
	require.NotSame(t, listed[0].OwnerProfile, again[0].OwnerProfile)
	require.Same(t, again[0].OwnerProfile, again[1].OwnerProfile)
}

func TestListAgentsUsesOwnershipOrExplicitRead(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganization(t, conn, "org-b")
	for _, user := range []string{"caller", "other"} {
		seedOrganizationUser(t, conn, "org-a", user)
	}
	seedOrganizationUser(t, conn, "org-b", "caller")
	owned := createAgent(t, conn, "org-a", "caller", "Owned")
	delegated := createAgent(t, conn, "org-a", "other", "Delegated")
	createAgent(t, conn, "org-a", "other", "Hidden")
	createAgent(t, conn, "org-b", "caller", "Other tenant")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, owned.ID.String(), listed[0].ID)
	require.True(t, listed[0].Permissions.Read)
	require.NotNil(t, listed[0].OwnerProfile)
	require.Equal(t, "caller", listed[0].OwnerProfile.DisplayName)
	_, err = agentsrepo.New(conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: "org-a", ID: owned.ID})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, gen.AgentLifecycle("suspended"), listed[0].Lifecycle)
	_, err = agentsrepo.New(conn).RevokeAgent(ctx, agentsrepo.RevokeAgentParams{OrganizationID: "org-a", ID: owned.ID})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, owned.ID.String(), listed[0].ID)
	require.Equal(t, gen.AgentLifecycle("revoked"), listed[0].Lifecycle)
	support := contextvalues.WithValidatedGramSession(ctx, mustAuthContext(t, ctx), true)
	_, err = service.List(support, &gen.ListPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	selectors, err := authz.NewSelector(authz.ScopeAgentRead, delegated.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-a", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), Scope: string(authz.ScopeAgentRead),
		Selectors: selectors,
	})
	require.NoError(t, err)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, delegated.ID.String(), listed[0].ID)
	require.True(t, listed[0].Permissions.Read)
	require.False(t, listed[0].Permissions.Write)
	_, err = service.List(validatedHumanContext(t, "org-a", "absent"), &gen.ListPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}
