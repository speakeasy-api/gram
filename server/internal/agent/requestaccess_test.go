package agent_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func grantUserScope(t *testing.T, ctx context.Context, ti *testInstance, userID string, scope authz.Scope, resourceID string) {
	t.Helper()
	selectors, err := authz.NewSelector(scope, resourceID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func requestablePermission(res *gen.ListRequestableAccessResult, scope authz.Scope) *gen.AgentRequestablePermission {
	idx := slices.IndexFunc(res.Permissions, func(p *gen.AgentRequestablePermission) bool { return p.Scope == string(scope) })
	if idx < 0 {
		return nil
	}
	return res.Permissions[idx]
}

func TestListRequestableAccess_RefusesOrgInstallKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)

	_, err := ti.service.ListRequestableAccess(ctx, &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeChatRead), ResourceID: nil, Message: nil, Duration: nil, RequestedUntil: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListRequestableAccess_RefusesAgentKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "CI agent")

	_, err := ti.service.ListRequestableAccess(agentCtx, &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RequestAccess(agentCtx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeChatRead), ResourceID: nil, Message: nil, Duration: nil, RequestedUntil: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListRequestableAccess_NotFoundWhileRolloutOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, false)

	_, err := ti.service.ListRequestableAccess(perUserCtx(t, ctx), &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeNotFound)
	_, err = ti.service.RequestAccess(perUserCtx(t, ctx), &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeChatRead), ResourceID: nil, Message: nil, Duration: nil, RequestedUntil: nil})
	requireCode(t, err, oops.CodeNotFound)
}

func TestListRequestableAccess_ListsTemporaryScopesTheUserLacks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeProjectRead, ti.projectID.String())

	res, err := ti.service.ListRequestableAccess(perUserCtx(t, ctx), &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	require.NoError(t, err)
	for _, permission := range res.Permissions {
		require.NotEqual(t, string(authz.ScopeOrgAdmin), permission.Scope, "org:admin is never given temporarily")
		require.NotEqual(t, string(authz.ScopeMCPAuth), permission.Scope)
		require.NotEmpty(t, permission.Label)
	}

	logs := requestablePermission(res, authz.ScopeLogRead)
	require.NotNil(t, logs)
	require.True(t, logs.AllResources)
	require.Equal(t, "project", logs.ResourceType)
	require.Len(t, logs.Projects, 1, "a project the user can see is offered")
	require.Equal(t, ti.projectID.String(), logs.Projects[0].ID)
	require.NotEmpty(t, logs.Projects[0].Name)

	chats := requestablePermission(res, authz.ScopeChatRead)
	require.NotNil(t, chats)
	require.Empty(t, chats.Projects, "chat:read does not apply to a project")
	require.Empty(t, res.Pending)
}

func TestListRequestableAccess_HidesWhatTheUserHoldsEverywhere(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeChatWrite, authz.WildcardResource)
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeProjectRead, ti.projectID.String())
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeLogRead, ti.projectID.String())

	res, err := ti.service.ListRequestableAccess(perUserCtx(t, ctx), &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	require.NoError(t, err)
	require.Nil(t, requestablePermission(res, authz.ScopeChatWrite), "a scope held on every resource is hidden")
	require.Nil(t, requestablePermission(res, authz.ScopeChatRead), "so is a scope it implies")
	logs := requestablePermission(res, authz.ScopeLogRead)
	require.NotNil(t, logs, "a scope held on one project can still be asked for everywhere")
	require.Empty(t, logs.Projects, "but not on the project already held")
}

func TestRequestAccess_FilesOnceAndListsItAsPending(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	userCtx := perUserCtx(t, ctx)

	res, err := ti.service.RequestAccess(userCtx, &gen.RequestAccessPayload{
		ApikeyToken: nil, Scope: string(authz.ScopeChatRead), ResourceID: nil,
		Message: new("Investigating an incident"), Duration: new("2h"), RequestedUntil: nil,
	})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, string(authz.ScopeChatRead), res.Request.Scope)
	require.NotEmpty(t, res.Request.PermissionLabel)
	require.Nil(t, res.Request.ResourceID, "every chat was asked for")

	stored, err := accessrepo.New(ti.conn).GetAccessRequest(ctx, accessrepo.GetAccessRequestParams{OrganizationID: ti.orgID, ID: uuid.MustParse(res.Request.ID)})
	require.NoError(t, err)
	require.Equal(t, mockidp.MockUserID, stored.AccessRequest.RequesterUserID)
	require.Equal(t, "Investigating an incident", stored.AccessRequest.Message.String)
	require.Equal(t, int32(7200), stored.AccessRequest.RequestedDurationSeconds.Int32)

	again, err := ti.service.RequestAccess(userCtx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeChatRead), ResourceID: nil, Message: nil, Duration: nil, RequestedUntil: nil})
	require.NoError(t, err)
	require.False(t, again.Created, "asking again returns the pending request")
	require.Equal(t, res.Request.ID, again.Request.ID)

	list, err := ti.service.ListRequestableAccess(userCtx, &gen.ListRequestableAccessPayload{ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, list.Pending, 1)
	require.Equal(t, res.Request.ID, list.Pending[0].ID)
}

func TestRequestAccess_TakesADeadline(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	until := time.Now().Add(5 * time.Hour).UTC().Truncate(time.Second)

	res, err := ti.service.RequestAccess(perUserCtx(t, ctx), &gen.RequestAccessPayload{
		ApikeyToken: nil, Scope: string(authz.ScopeLogRead), ResourceID: nil, Message: nil, Duration: nil,
		RequestedUntil: new(until.Format(time.RFC3339)),
	})
	require.NoError(t, err)
	stored, err := accessrepo.New(ti.conn).GetAccessRequest(ctx, accessrepo.GetAccessRequestParams{OrganizationID: ti.orgID, ID: uuid.MustParse(res.Request.ID)})
	require.NoError(t, err)
	require.True(t, stored.AccessRequest.RequestedUntil.Time.Equal(until))

	_, err = ti.service.RequestAccess(perUserCtx(t, ctx), &gen.RequestAccessPayload{
		ApikeyToken: nil, Scope: string(authz.ScopeChatWrite), ResourceID: nil, Message: nil, Duration: new("1h"),
		RequestedUntil: new(until.Format(time.RFC3339)),
	})
	requireCode(t, err, oops.CodeBadRequest)
}

func TestRequestAccess_RefusesWhatCannotBeRequested(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeChatRead, authz.WildcardResource)
	userCtx := perUserCtx(t, ctx)
	file := func(scope authz.Scope, resourceID *string) error {
		_, err := ti.service.RequestAccess(userCtx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(scope), ResourceID: resourceID, Message: nil, Duration: nil, RequestedUntil: nil})
		return err
	}

	requireCode(t, file(authz.ScopeOrgAdmin, nil), oops.CodeBadRequest)
	requireCode(t, file(authz.ScopeChatRead, nil), oops.CodeBadRequest)
	requireCode(t, file(authz.ScopeLogRead, new(uuid.NewString())), oops.CodeBadRequest)
	requireCode(t, file(authz.ScopeLogRead, new(ti.projectID.String())), oops.CodeBadRequest)
	requireCode(t, file(authz.ScopeMCPConnect, new(uuid.NewString())), oops.CodeBadRequest)
	requireCode(t, file(authz.ScopeSkillRead, new(uuid.NewString())), oops.CodeBadRequest)
}

func TestRequestAccess_ProjectTheUserCanSee(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	grantUserScope(t, ctx, ti, mockidp.MockUserID, authz.ScopeProjectRead, ti.projectID.String())

	res, err := ti.service.RequestAccess(perUserCtx(t, ctx), &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeLogRead), ResourceID: new(ti.projectID.String()), Message: nil, Duration: nil, RequestedUntil: nil})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, ti.projectID.String(), *res.Request.ResourceID)
	require.NotEmpty(t, *res.Request.ResourceName, "the project is named by the server")
}

func TestRequestAccess_BlockedServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	server, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
		server: {
			MCPID: server.String(), MCPName: "Linear",
			State: platformmcp.MCPConnectionStateNotApplicable, Reason: platformmcp.MCPConnectionReasonPermissionRequired,
			RequiredScope: "mcp:connect", RequestAccessURL: testSiteURL + "/request-access",
		},
	}})
	userCtx := perUserCtx(t, ctx)

	conns, err := ti.service.GetMyConnections(userCtx, &gen.GetMyConnectionsPayload{Email: nil})
	require.NoError(t, err)
	target := connectionByID(t, conns, server).RequestAccess
	require.NotNil(t, target, "a per-user key is told what to request")
	require.Equal(t, "mcp:connect", target.Scope)
	require.Equal(t, server.String(), target.ResourceID)
	require.Equal(t, "Linear", *target.ResourceName)

	orgKeyConns, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Nil(t, connectionByID(t, orgKeyConns, server).RequestAccess, "the org install key cannot file it")

	res, err := ti.service.RequestAccess(userCtx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: target.Scope, ResourceID: new(target.ResourceID), Message: nil, Duration: new("1h"), RequestedUntil: nil})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, server.String(), *res.Request.ResourceID)
	require.Equal(t, "Linear", *res.Request.ResourceName)

	_, err = ti.service.RequestAccess(userCtx, &gen.RequestAccessPayload{ApikeyToken: nil, Scope: string(authz.ScopeMCPWrite), ResourceID: new(target.ResourceID), Message: nil, Duration: nil, RequestedUntil: nil})
	requireCode(t, err, oops.CodeBadRequest) // only the scope the server names can be asked for on it
}

func TestGetMyConnections_NoRequestTargetWhileRolloutOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, false)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	server, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
		server: {
			MCPID: server.String(), MCPName: "Linear",
			State: platformmcp.MCPConnectionStateNotApplicable, Reason: platformmcp.MCPConnectionReasonPermissionRequired,
			RequiredScope: "mcp:connect",
		},
	}})

	conns, err := ti.service.GetMyConnections(perUserCtx(t, ctx), &gen.GetMyConnectionsPayload{Email: nil})
	require.NoError(t, err)
	c := connectionByID(t, conns, server)
	require.Equal(t, "no_access", c.State)
	require.Nil(t, c.RequestAccess)
}
