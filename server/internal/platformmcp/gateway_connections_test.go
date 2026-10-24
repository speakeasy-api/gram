package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	usersessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type gatewaySessionsFake struct {
	items      []*types.UserSession
	err        error
	list       *usersessionsgen.ListUserSessionsPayload
	facets     *usersessionsgen.QueryFacetsPayload
	projectIDs []string
}

func (f *gatewaySessionsFake) ListUserSessions(ctx context.Context, payload *usersessionsgen.ListUserSessionsPayload) (*usersessionsgen.ListUserSessionsResult, error) {
	f.list = payload
	f.recordProject(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &usersessionsgen.ListUserSessionsResult{Items: f.items, NextCursor: new("cursor-2")}, nil
}

func (f *gatewaySessionsFake) QueryFacets(ctx context.Context, payload *usersessionsgen.QueryFacetsPayload) (*usersessionsgen.UserSessionFilterFacets, error) {
	f.facets = payload
	f.recordProject(ctx)
	return &usersessionsgen.UserSessionFilterFacets{UnattributedCount: new(int64(3))}, nil
}

func (f *gatewaySessionsFake) recordProject(ctx context.Context) {
	auth, _ := contextvalues.GetAuthContext(ctx)
	f.projectIDs = append(f.projectIDs, auth.ProjectID.String())
}

func gatewayConnectionsTestService(sessions gatewaySessions, project uuid.UUID, resolveErr error) *GatewayConnectionsService {
	return &GatewayConnectionsService{
		sessions: sessions,
		resolveProject: func(context.Context, Principal, FindMCPInput) (ResolvedProject, error) {
			return ResolvedProject{ID: project, Name: "Project", Slug: "project"}, resolveErr
		},
	}
}

func gatewayConnectionsContext(t *testing.T) context.Context {
	t.Helper()
	principal := testPrincipal()
	ctx := contextWithPrincipal(t.Context(), principal)
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: principal.UserID, ActiveOrganizationID: principal.OrganizationID})
}

func TestGatewayConnectionsToolKeepsOneContract(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	for name, service := range map[string]*GatewayConnectionsService{
		"unavailable": nil,
		"live":        gatewayConnectionsTestService(&gatewaySessionsFake{}, project, nil),
	} {
		reg := newRegistrar(newTestMCPServer())
		registerGatewayConnectionsTool(reg, service)
		descriptor := descriptorByName(t, reg, listGatewayConnectionsToolName)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, name)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization, name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, name)
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		require.Contains(t, string(descriptor.InputSchema), `"gateway_id"`, name)
	}

	reg := newRegistrar(newTestMCPServer())
	registerGatewayConnectionsTool(reg, nil)
	_, err := descriptorByName(t, reg, listGatewayConnectionsToolName).Invoke(gatewayConnectionsContext(t), json.RawMessage(`{"project_id":"`+project.String()+`","gateway_id":"`+uuid.NewString()+`"}`))
	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, err)
	require.JSONEq(t, `{"code":"`+unavailableCode+`","feature":"gateway_connections","message":"Gateway connections are unavailable on this server."}`, refusal.Payload)
}

func TestListGatewayConnectionsReadsThroughTheScopedSessionService(t *testing.T) {
	t.Parallel()

	project, gateway := uuid.New(), uuid.NewString()
	fake := &gatewaySessionsFake{items: []*types.UserSession{
		{ID: "s-1", UserSessionIssuerID: "issuer-1", SubjectUrn: "user:u-1", SubjectType: "user", SubjectDisplayName: new("Ada Lovelace"), Jti: "jti-secret", IssuerSlug: "sign-in", ClientName: new("Editor"), UserSessionClientID: new("c-1"), RefreshExpiresAt: "2026-11-01T00:00:00Z", Upstreams: []*types.UserSessionUpstream{{IssuerSlug: "mcp.example.com", RemoteSessionID: "remote-session-1", HasRefreshToken: true}}},
		{ID: "s-2", SubjectUrn: "agent:a-1", SubjectType: "agent", SubjectDisplayName: new("Hidden agent"), Upstreams: []*types.UserSessionUpstream{}},
	}}
	service := gatewayConnectionsTestService(fake, project, nil)

	out, err := service.List(gatewayConnectionsContext(t), testPrincipal(), ListGatewayConnectionsInput{
		ProjectID: project.String(), GatewayID: gateway, Search: "ada", Clients: []string{"none"}, ExcludeIdentityTypes: []string{"apikey"}, Servers: []string{"issuer-1"},
	})
	require.NoError(t, err)

	require.Equal(t, []string{project.String(), project.String()}, fake.projectIDs, "both reads run in the named project")
	require.Equal(t, gateway, *fake.list.MetaMcpServerID)
	require.Equal(t, gateway, *fake.facets.MetaMcpServerID)
	require.Equal(t, "ada", *fake.list.Search)
	require.Equal(t, []string{"none"}, fake.facets.Clients, "the count uses the list's filters")
	require.Equal(t, []string{"apikey"}, fake.facets.ExcludeIdentityTypes)
	require.Equal(t, []string{"issuer-1"}, fake.list.Servers)
	require.Equal(t, []string{"issuer-1"}, fake.facets.Servers)
	require.Nil(t, fake.list.Limit, "an omitted limit takes the list's default page")
	require.Nil(t, fake.facets.Field)

	require.Equal(t, int64(3), out.UnattributedCount)
	require.Equal(t, "cursor-2", out.NextCursor)
	require.Equal(t, GatewayConnection{
		ID: "s-1", Identity: "user:u-1", IdentityType: "user", DisplayName: "Ada Lovelace", ClientID: "c-1", ClientName: "Editor",
		SignInServerID: "issuer-1", SignInServer: "sign-in", UpstreamProviders: []string{"mcp.example.com"}, ExpiresAt: "2026-11-01T00:00:00Z",
	}, out.Connections[0])
	require.Empty(t, out.Connections[1].DisplayName, "agents are not named")

	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	for _, secret := range []string{"jti", "token", "hash", "remote-session-1", "Hidden agent"} {
		require.NotContains(t, strings.ToLower(string(encoded)), strings.ToLower(secret))
	}
}

func TestListGatewayConnectionsForwardsAnInRangeLimit(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	for _, limit := range []int{1, 100} {
		fake := &gatewaySessionsFake{}
		_, err := gatewayConnectionsTestService(fake, project, nil).List(gatewayConnectionsContext(t), testPrincipal(), ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString(), Limit: limit})
		require.NoError(t, err)
		require.Equal(t, limit, *fake.list.Limit)
	}
}

func TestListGatewayConnectionsRefusals(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	valid := ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString()}
	for name, tc := range map[string]struct {
		input      ListGatewayConnectionsInput
		resolveErr error
		serviceErr error
		principal  Principal
		want       error
		code       string
		message    string
		calls      bool
	}{
		"bad gateway id":         {input: ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: "gw"}, code: "invalid_request"},
		"search too long":        {input: ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString(), Search: strings.Repeat("é", 201)}, code: "invalid_request"},
		"negative limit":         {input: ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString(), Limit: -1}, code: "invalid_request"},
		"limit over 100":         {input: ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString(), Limit: 101}, code: "invalid_request"},
		"project hidden":         {input: valid, resolveErr: ErrForbidden, want: ErrForbidden},
		"another user":           {input: valid, principal: Principal{UserID: "user-2", OrganizationID: "organization-1"}, want: ErrForbidden},
		"foreign gateway":        {input: valid, serviceErr: oops.E(oops.CodeNotFound, nil, "gateway not found"), code: "not_found", message: "That gateway is not in the selected project.", calls: true},
		"session service denial": {input: valid, serviceErr: oops.C(oops.CodeForbidden), want: ErrForbidden, calls: true},
		"bad filter":             {input: valid, serviceErr: oops.E(oops.CodeBadRequest, nil, "identities values must not be empty"), code: "invalid_request", message: "identities values must not be empty", calls: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &gatewaySessionsFake{err: tc.serviceErr}
			principal := testPrincipal()
			if tc.principal.UserID != "" {
				principal = tc.principal
			}
			_, err := gatewayConnectionsTestService(fake, project, tc.resolveErr).List(gatewayConnectionsContext(t), principal, tc.input)
			require.Equal(t, tc.calls, fake.list != nil, "the session service is reached only for a valid, visible project")
			if tc.want != nil {
				require.Equal(t, tc.want, err)
				return
			}
			result, ok := gatewayConnectionsToolResult(err)
			require.True(t, ok, err)
			var refusal featureUnavailableResult
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok, "%T", result.Content[0])
			require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
			require.Equal(t, tc.code, refusal.Code)
			if tc.message != "" {
				require.Equal(t, tc.message, refusal.Message)
			}
		})
	}
}
