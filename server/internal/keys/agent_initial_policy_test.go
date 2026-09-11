package keys_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	agentgen "github.com/speakeasy-api/gram/server/gen/agents"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestKeysService_InitialAgentPolicyCannotElevateCredentialAuthority(t *testing.T) {
	t.Parallel()
	for _, constrained := range []string{"owner", "authorizer"} {
		t.Run(constrained, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestKeysService(t)
			authCtx := testAuthContext(t, ctx)
			require.NotNil(t, authCtx.ProjectID)
			ownerID := "owner-" + uuid.NewString()
			_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: ownerID, Email: ownerID + "@example.com", DisplayName: ownerID, PhotoUrl: conv.PtrToPGText(nil), Admin: false})
			require.NoError(t, err)
			_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: conv.ToPGText(ownerID)})
			require.NoError(t, err)
			caller := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
			owner := urn.NewPrincipal(urn.PrincipalTypeUser, ownerID)
			upsertGrant(t, ctx, ti, caller, authz.ScopeAgentWrite, "*")
			logger := testenv.NewLogger(t)
			engine := authz.NewEngine(logger, ti.conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
			management := agentmanagement.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), ti.features, nil, nil)
			agent, err := management.Create(ctx, &agentgen.CreatePayload{Name: "Initial policy agent", OwnerUserID: &ownerID, PolicyGrants: []*agentgen.AgentPolicyGrantForm{{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "*"}}}})
			require.NoError(t, err)
			agentID, err := uuid.Parse(agent.ID)
			require.NoError(t, err)
			upsertGrant(t, ctx, ti, caller, authz.ScopeAgentAuthorize, agent.ID)
			limited, broad := owner, caller
			if constrained == "authorizer" {
				limited, broad = caller, owner
			}
			upsertGrant(t, ctx, ti, broad, authz.ScopeMCPConnect, "*")
			payload := agentKeyPayload(agentID, *authCtx.ProjectID)
			payload.RequestedGrants = []*gen.AgentPolicyGrantForm{{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "example-server", Tool: new("allowed-tool")}}}
			// The initial broad policy and explicit management rights cannot supply missing live resource rights.
			_, err = ti.service.CreateKey(ctx, payload)
			requireOopsCode(t, err, oops.CodeForbidden)
			narrow := authz.NewSelector(authz.ScopeMCPConnect, "example-server")
			narrow[authz.SelectorKeyTool] = "allowed-tool"
			upsertGrantSelector(t, ctx, ti, limited, authz.ScopeMCPConnect, narrow)
			payload.RequestedGrants[0].Selector.Tool = nil
			_, err = ti.service.CreateKey(ctx, payload)
			requireOopsCode(t, err, oops.CodeForbidden)
			payload.RequestedGrants[0].Selector.Tool = new("allowed-tool")
			created, err := ti.service.CreateKey(ctx, payload)
			require.NoError(t, err)
			require.NotNil(t, created.Key)
			require.Len(t, created.DelegatedGrants.Requested, 1)
			require.Equal(t, "allowed-tool", *created.DelegatedGrants.Requested[0].Selector.Tool)
		})
	}
}
