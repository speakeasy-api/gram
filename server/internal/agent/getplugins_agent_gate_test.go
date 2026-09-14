package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var getPluginsScheme = &security.APIKeyScheme{
	Name:           "apikey",
	Scopes:         []string{"consumer", "producer", "chat", "hooks", "agent", "agent_user"},
	RequiredScopes: []string{"agent_user"},
}

type realAgentKey struct {
	key     string
	agentID uuid.UUID
	actor   urn.Principal
}

// mintAgentKey stores a real agent-principal key through the keys repo writer;
// agent and owner hold every delegated grant so live admission keeps them.
func mintAgentKey(t *testing.T, ctx context.Context, ti *testInstance, delegated []authz.Grant) realAgentKey {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID, OwnerUserID: authCtx.UserID, Name: "Gate agent",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())

	for _, grant := range delegated {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		for _, principal := range []urn.Principal{actor, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)} {
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
				OrganizationID: ti.orgID,
				PrincipalUrn:   principal,
				Scope:          string(grant.Scope),
				Selectors:      selectors,
			})
			require.NoError(t, err)
		}
	}

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, delegated)
	require.NoError(t, err)
	rawPolicy, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	key, keyHash, keyPrefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("local"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         ti.orgID,
		CreatedByUserID:        authCtx.UserID,
		Name:                   "gate-key",
		KeyPrefix:              keyPrefix,
		KeyHash:                keyHash,
		SubjectUrn:             conv.ToPGText(actor.String()),
		DelegatedGrants:        rawPolicy,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return realAgentKey{key: key, agentID: agent.ID, actor: actor}
}

func requireCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func TestAPIKeyAuth_AgentKeyWithDeviceSyncGrantPollsPlugins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	minted := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	agentTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, agentTool, ti.orgID, minted.actor.String())

	admitted, err := ti.service.APIKeyAuth(context.WithValue(t.Context(), goa.MethodKey, "getPlugins"), minted.key, getPluginsScheme)
	require.NoError(t, err)

	res, err := ti.service.GetPlugins(admitted, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "agent-tool"}, pluginSlugs(res))
	require.NotNil(t, res.Principal)
	require.Equal(t, minted.actor.String(), res.Principal.Urn)
}

func TestAPIKeyAuth_AgentKeyWithoutDeviceSyncGrantIsForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	minted := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String())})

	_, err := ti.service.APIKeyAuth(context.WithValue(t.Context(), goa.MethodKey, "getPlugins"), minted.key, getPluginsScheme)
	requireCode(t, err, oops.CodeForbidden)
}

func TestAPIKeyAuth_AgentKeyOnOtherMethodIsForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	minted := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})

	_, err := ti.service.APIKeyAuth(context.WithValue(t.Context(), goa.MethodKey, "listSyncedUsers"), minted.key, getPluginsScheme)
	requireCode(t, err, oops.CodeForbidden)
}

func TestGetPlugins_AgentKeyReceivesRoleAssignedPlugins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	minted := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})

	q := accessrepo.New(ti.conn)
	now := time.Now()
	role, err := q.CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    ti.orgID,
		WorkosSlug:        "agent-role",
		WorkosName:        "agent-role",
		WorkosDescription: conv.ToPGText(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGText(""),
	})
	require.NoError(t, err)
	rows, err := q.UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{
		OrganizationID: ti.orgID, RoleUrn: role.RoleUrn, AgentID: minted.agentID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	roleTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "role-tool")
	assignPlugin(t, ctx, ti.conn, roleTool, ti.orgID, role.RoleUrn)
	otherRoleTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "other-role-tool")
	assignPlugin(t, ctx, ti.conn, otherRoleTool, ti.orgID, "role:organization:unheld")

	admitted, err := ti.service.APIKeyAuth(context.WithValue(t.Context(), goa.MethodKey, "getPlugins"), minted.key, getPluginsScheme)
	require.NoError(t, err)
	res, err := ti.service.GetPlugins(admitted, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "role-tool"}, pluginSlugs(res))
}
