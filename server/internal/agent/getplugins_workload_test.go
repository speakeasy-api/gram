package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

var deviceAgentTokenScheme = &security.JWTScheme{Name: "device_agent_token", Scopes: []string{}, RequiredScopes: []string{}}

// workloadDevice is an admitted workload assigned to an agent, and a
// device-agent token for it.
type workloadDevice struct {
	agentID  uuid.UUID
	actor    urn.Principal
	issuerID uuid.UUID
	subject  string
	token    string
}

// newWorkloadDevice admits a workload, assigns it a new agent holding grants,
// and mints it a device-agent token.
func newWorkloadDevice(t *testing.T, ctx context.Context, ti *testInstance, grants []authz.Grant) workloadDevice {
	t.Helper()
	return newWorkloadDeviceInProject(t, ctx, ti, grants, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

// newWorkloadDeviceInProject is newWorkloadDevice with the agent bound to
// projectID.
func newWorkloadDeviceInProject(t *testing.T, ctx context.Context, ti *testInstance, grants []authz.Grant, projectID uuid.NullUUID) workloadDevice {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID, OwnerUserID: authCtx.UserID, ProjectID: projectID, Name: "Headless box agent",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	for _, grant := range grants {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: ti.orgID,
			PrincipalUrn:   actor,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}
	// Workload admission also bounds every request by the agent owner's live
	// permissions. The owner holds everything these tests exercise, so the
	// agent's grants alone decide.
	for _, grant := range []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID),
		authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource),
	} {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: ti.orgID,
			PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	subject := "device:" + uuid.NewString()
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: ti.orgID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "devices",
		Issuer:         workloadidentity.DeviceIssuerURL(testServerURL, ti.orgID),
		JwksUri:        workloadidentity.DeviceIssuerURL(testServerURL, ti.orgID) + "/jwks.json",
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   ti.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          subject,
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: issuerID,
		Subject:          subject,
		MatchKind:        string(workloadidentity.MatchKindExact),
		AgentID:          agent.ID,
	}))

	token, _, err := ti.deviceAgentTokens.Mint(ti.orgID, urn.NewWorkloadPrincipal(issuerID, subject), time.Now())
	require.NoError(t, err)
	return workloadDevice{agentID: agent.ID, actor: actor, issuerID: issuerID, subject: subject, token: token}
}

func getPluginsContext(t *testing.T) context.Context {
	t.Helper()
	return context.WithValue(t.Context(), goa.MethodKey, "getPlugins")
}

// A workload's device-agent token polls the plugins assigned to its agent, and
// the poll is attributed to that agent.
func TestJWTAuth_WorkloadTokenPollsItsAgentsPlugins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	agentTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, agentTool, ti.orgID, device.actor.String())
	otherTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "other-tool")
	assignPlugin(t, ctx, ti.conn, otherTool, ti.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()).String())

	admitted, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)

	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.NewWorkloadPrincipal(device.issuerID, device.subject).String(), actor.String(), "the device stays the authenticated actor")
	agent, ok := deviceagenttoken.EffectiveAgent(admitted)
	require.True(t, ok)
	require.Equal(t, device.actor.String(), agent.String(), "the request acts for the assigned agent")

	res, err := ti.service.GetPlugins(admitted, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "agent-tool"}, pluginSlugs(res))
	require.NotNil(t, res.Principal)
	require.Equal(t, device.actor.String(), res.Principal.Urn)
}

// A workload's poll lists the MCP servers in its agent's plugins, as an agent
// key's poll does.
func TestJWTAuth_WorkloadTokenPollsItsAgentsMCPServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	agentTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, agentTool, ti.orgID, device.actor.String())
	_, linearURL := seedToolsetServer(t, ti, agentTool, "team-linear", "Team Linear")
	otherTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "other-tool")
	assignPlugin(t, ctx, ti.conn, otherTool, ti.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()).String())
	seedToolsetServer(t, ti, otherTool, "slack", "Slack")

	admitted, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)
	res, err := ti.service.GetPlugins(admitted, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"speakeasy-team-linear": linearURL}, mcpServersByName(res))
}

// Withdrawing the workload's assignment refuses its token on the next call,
// though the token itself has not expired.
func TestJWTAuth_WithdrawnAssignmentRefusedOnNextCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})

	_, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)

	withdrawn, err := workloadpolicyrepo.New(ti.conn).SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadpolicyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   ti.orgID,
		WorkloadIssuerID: device.issuerID,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Subject:          device.subject,
	})
	require.NoError(t, err)
	require.Len(t, withdrawn, 1)

	_, err = ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	requireCode(t, err, oops.CodeUnauthorized)
}

// Policy polling does not depend on a project: deleting the project its agent
// belongs to, which a device's hooks are not bound to, leaves the device
// polling.
func TestJWTAuth_AgentInDeletedProjectStillPolls(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDeviceInProject(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)}, uuid.NullUUID{UUID: ti.projectID, Valid: true})

	_, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, ti.projectID)
	require.NoError(t, err)

	admitted, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)
	res, err := ti.service.GetPlugins(admitted, &gen.GetPluginsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res.Principal)
	require.Equal(t, device.actor.String(), res.Principal.Urn)
}

// A workload whose agent lacks the device-sync grant cannot poll, as an agent
// key without it cannot.
func TestJWTAuth_WorkloadWithoutDeviceSyncIsForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String())})

	_, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	requireCode(t, err, oops.CodeForbidden)
}

// An expired token is refused as unauthorized, so the device exchanges for a
// new one.
func TestJWTAuth_ExpiredTokenIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	expired, _, err := ti.deviceAgentTokens.Mint(ti.orgID, urn.NewWorkloadPrincipal(device.issuerID, device.subject), time.Now().Add(-2*deviceagenttoken.Lifetime))
	require.NoError(t, err)

	_, err = ti.service.JWTAuth(getPluginsContext(t), expired, deviceAgentTokenScheme)
	requireCode(t, err, oops.CodeUnauthorized)
}

// A token signed by another platform key is refused, and the refusal is what
// the API key requirement reports when the request carried no key.
func TestJWTAuth_ForgedTokenRefusalReachesTheKeyRequirement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	forged, _, err := deviceagenttoken.New(mcpauthztest.NewIssuer(t), ti.conn).Mint(ti.orgID, urn.NewWorkloadPrincipal(device.issuerID, device.subject), time.Now())
	require.NoError(t, err)

	refusedCtx, err := ti.service.JWTAuth(getPluginsContext(t), forged, deviceAgentTokenScheme)
	requireCode(t, err, oops.CodeUnauthorized)

	_, err = ti.service.APIKeyAuth(refusedCtx, "", getPluginsScheme)
	requireCode(t, err, oops.CodeUnauthorized)
	require.ErrorIs(t, err, deviceagenttoken.ErrInvalid)
}

// A request with no device-agent token is left for the API key requirement,
// with nothing recorded against it.
func TestJWTAuth_NoTokenLeavesTheKeyRequirementUntouched(t *testing.T) {
	t.Parallel()
	_, ti := newTestAgentService(t)

	ctx := getPluginsContext(t)
	for _, token := range []string{"", "not-a-device-agent-token"} {
		refusedCtx, err := ti.service.JWTAuth(ctx, token, deviceAgentTokenScheme)
		requireCode(t, err, oops.CodeUnauthorized)
		require.NoError(t, deviceagenttoken.AuthFailure(refusedCtx))
	}
}
