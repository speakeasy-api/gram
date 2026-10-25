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
	facetErr   error
	list       *usersessionsgen.ListUserSessionsPayload
	facets     *usersessionsgen.QueryFacetsPayload
	trend      *usersessionsgen.GetTrendPayload
	trendErr   error
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
	if f.facetErr != nil {
		return nil, f.facetErr
	}
	return &usersessionsgen.UserSessionFilterFacets{UnattributedCount: new(int64(3))}, nil
}

func (f *gatewaySessionsFake) GetTrend(ctx context.Context, payload *usersessionsgen.GetTrendPayload) (*usersessionsgen.UserSessionTrend, error) {
	f.trend = payload
	f.recordProject(ctx)
	if f.trendErr != nil {
		return nil, f.trendErr
	}
	return &usersessionsgen.UserSessionTrend{
		From: payload.From, To: payload.To, Bucket: payload.Bucket,
		Buckets:       []*usersessionsgen.UserSessionTrendBucket{{Start: payload.From, End: payload.To, Created: 2, Revoked: 1, Active: 4}},
		ActiveAtStart: 3, Created: 2, Revoked: 1, ActiveAtEnd: 4, ExcludedLegacyRows: 5, UnattributedRows: new(int64(6)),
	}, nil
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

	project, gateway, server, otherServer, otherProvider := uuid.New(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	fake := &gatewaySessionsFake{items: []*types.UserSession{
		{ID: "s-1", UserSessionIssuerID: "issuer-1", SubjectUrn: "user:u-1", SubjectType: "user", SubjectDisplayName: new("Ada Lovelace"), Jti: "jti-secret", IssuerSlug: "sign-in", ClientName: new("Editor"), UserSessionClientID: new("c-1"), CreatedAt: "2026-10-01T00:00:00Z", LastUsedAt: new("2026-10-20T00:00:00Z"), RevokedAt: new("2026-10-21T00:00:00Z"), RefreshExpiresAt: "2026-11-01T00:00:00Z", Upstreams: []*types.UserSessionUpstream{{IssuerSlug: "mcp.example.com", RemoteSessionIssuerID: "provider-1", RemoteSessionID: "remote-session-1", HasRefreshToken: true}}},
		{ID: "s-2", SubjectUrn: "agent:a-1", SubjectType: "agent", SubjectDisplayName: new("Hidden agent"), Upstreams: []*types.UserSessionUpstream{}},
	}}
	service := gatewayConnectionsTestService(fake, project, nil)

	out, err := service.List(gatewayConnectionsContext(t), testPrincipal(), ListGatewayConnectionsInput{
		ProjectID: project.String(), GatewayID: gateway, Search: "ada", Status: "all",
		Identities: []string{"user:u-1"}, ExcludeIdentities: []string{"user:u-2"},
		IdentityTypes: []string{"user"}, ExcludeIdentityTypes: []string{"apikey"},
		Servers: []string{server}, ExcludeServers: []string{otherServer},
		Clients: []string{"none"}, ExcludeClients: []string{"dashboard"},
		ClientRegistrations: []string{"dcr"}, ExcludeRegistrations: []string{"cimd"},
		Providers: []string{"none"}, ExcludeProviders: []string{otherProvider},
		LastUsedWithin: new(86400), LastUsedOlderThan: new(3600),
		Cursor: "cursor-1", Limit: 25,
	})
	require.NoError(t, err)

	require.Equal(t, []string{project.String(), project.String()}, fake.projectIDs, "both reads run in the named project")
	// The count applies the list's filters; only the list pages.
	for field, v := range map[string][3]any{ // want, list, facets
		"gateway":                {gateway, *fake.list.MetaMcpServerID, *fake.facets.MetaMcpServerID},
		"search":                 {"ada", *fake.list.Search, *fake.facets.Search},
		"status":                 {"all", *fake.list.Status, *fake.facets.Status},
		"identities":             {[]string{"user:u-1"}, fake.list.Identities, fake.facets.Identities},
		"exclude_identities":     {[]string{"user:u-2"}, fake.list.ExcludeIdentities, fake.facets.ExcludeIdentities},
		"identity_types":         {[]string{"user"}, fake.list.IdentityTypes, fake.facets.IdentityTypes},
		"exclude_identity_types": {[]string{"apikey"}, fake.list.ExcludeIdentityTypes, fake.facets.ExcludeIdentityTypes},
		"servers":                {[]string{server}, fake.list.Servers, fake.facets.Servers},
		"exclude_servers":        {[]string{otherServer}, fake.list.ExcludeServers, fake.facets.ExcludeServers},
		"clients":                {[]string{"none"}, fake.list.Clients, fake.facets.Clients},
		"exclude_clients":        {[]string{"dashboard"}, fake.list.ExcludeClients, fake.facets.ExcludeClients},
		"client_registrations":   {[]string{"dcr"}, fake.list.ClientRegistrations, fake.facets.ClientRegistrations},
		"exclude_registrations":  {[]string{"cimd"}, fake.list.ExcludeClientRegistrations, fake.facets.ExcludeClientRegistrations},
		"providers":              {[]string{"none"}, fake.list.Providers, fake.facets.Providers},
		"exclude_providers":      {[]string{otherProvider}, fake.list.ExcludeProviders, fake.facets.ExcludeProviders},
		"last_used_within":       {86400, *fake.list.LastUsedWithin, *fake.facets.LastUsedWithin},
		"last_used_older_than":   {3600, *fake.list.LastUsedOlderThan, *fake.facets.LastUsedOlderThan},
		"last_used_never":        {false, *fake.list.LastUsedNever, *fake.facets.LastUsedNever},
	} {
		require.Equal(t, v[0], v[1], "list %s", field)
		require.Equal(t, v[0], v[2], "facets %s", field)
	}
	require.Equal(t, "cursor-1", *fake.list.Cursor)
	require.Equal(t, 25, *fake.list.Limit)
	require.Nil(t, fake.facets.Field)

	require.Equal(t, int64(3), out.UnattributedCount)
	require.Equal(t, "cursor-2", out.NextCursor)
	require.Equal(t, GatewayConnection{
		ID: "s-1", Identity: "user:u-1", IdentityType: "user", DisplayName: "Ada Lovelace", ClientID: "c-1", ClientName: "Editor",
		SignInServerID: "issuer-1", SignInServer: "sign-in", UpstreamProviders: []string{"mcp.example.com"}, UpstreamProviderIDs: []string{"provider-1"},
		CreatedAt: "2026-10-01T00:00:00Z", ExpiresAt: "2026-11-01T00:00:00Z", LastUsedAt: "2026-10-20T00:00:00Z", RevokedAt: "2026-10-21T00:00:00Z",
	}, out.Connections[0])
	require.Empty(t, out.Connections[1].DisplayName, "agents are not named")

	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	for _, secret := range []string{"jti", "token", "hash", "remote-session-1", "Hidden agent"} {
		require.NotContains(t, strings.ToLower(string(encoded)), strings.ToLower(secret))
	}
}

// Forward no recorded use with no duration bounds.
func TestListGatewayConnectionsForwardsNoRecordedUse(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	fake := &gatewaySessionsFake{}
	_, err := gatewayConnectionsTestService(fake, project, nil).List(gatewayConnectionsContext(t), testPrincipal(), ListGatewayConnectionsInput{ProjectID: project.String(), GatewayID: uuid.NewString(), LastUsedNever: true})
	require.NoError(t, err)
	for name, payload := range map[string]struct {
		never         bool
		within, older *int
	}{
		"list":   {*fake.list.LastUsedNever, fake.list.LastUsedWithin, fake.list.LastUsedOlderThan},
		"facets": {*fake.facets.LastUsedNever, fake.facets.LastUsedWithin, fake.facets.LastUsedOlderThan},
	} {
		require.True(t, payload.never, name)
		require.Nil(t, payload.within, name)
		require.Nil(t, payload.older, name)
	}
	require.Nil(t, fake.list.Cursor, "an omitted cursor starts at the first page")
	require.Nil(t, fake.list.Limit, "an omitted limit takes the list's default page")
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
		facetErr   error
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
		"count denial":           {input: valid, facetErr: oops.C(oops.CodeForbidden), want: ErrForbidden, calls: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &gatewaySessionsFake{items: []*types.UserSession{{ID: "s-1"}}, err: tc.serviceErr, facetErr: tc.facetErr}
			principal := testPrincipal()
			if tc.principal.UserID != "" {
				principal = tc.principal
			}
			out, err := gatewayConnectionsTestService(fake, project, tc.resolveErr).List(gatewayConnectionsContext(t), principal, tc.input)
			require.Equal(t, tc.calls, fake.list != nil, "the session service is reached only for a valid, visible project")
			require.Zero(t, out, "a refusal returns no partial output")
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

func TestGatewayConnectionTrendToolKeepsOneContract(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	for name, service := range map[string]*GatewayConnectionsService{
		"unavailable": nil,
		"live":        gatewayConnectionsTestService(&gatewaySessionsFake{}, project, nil),
	} {
		reg := newRegistrar(newTestMCPServer())
		registerGatewayConnectionsTool(reg, service)
		descriptor := descriptorByName(t, reg, getGatewayConnectionTrendToolName)
		require.Equal(t, bothAudiences, descriptor.Meta.Audiences, name)
		require.Equal(t, externalOnly, descriptorByName(t, reg, listGatewayConnectionsToolName).Meta.Audiences, name, "the session list stays external only")
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization, name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, name)
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name, "no log:read")
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		require.Contains(t, string(descriptor.InputSchema), `"gateway_id"`, name)
	}

	reg := newRegistrar(newTestMCPServer())
	registerGatewayConnectionsTool(reg, nil)
	_, err := descriptorByName(t, reg, getGatewayConnectionTrendToolName).Invoke(gatewayConnectionsContext(t), json.RawMessage(`{"project_id":"`+project.String()+`","gateway_id":"`+uuid.NewString()+`"}`))
	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, err)
	require.Contains(t, refusal.Payload, unavailableCode)
}

func TestGatewayConnectionTrendReadsThroughTheScopedSessionService(t *testing.T) {
	t.Parallel()

	project, gateway := uuid.New(), uuid.New()
	fake := &gatewaySessionsFake{}
	service := gatewayConnectionsTestService(fake, project, nil)

	out, err := service.Trend(gatewayConnectionsContext(t), testPrincipal(), GatewayConnectionTrendInput{ProjectID: project.String(), GatewayID: gateway.String(), From: "", To: "2026-01-08T00:00:00Z", Bucket: ""})
	require.NoError(t, err)
	require.Equal(t, []string{project.String()}, fake.projectIDs, "the read runs in the named project")
	require.Equal(t, []string{gateway.String(), "2026-01-01T00:00:00Z", "2026-01-08T00:00:00Z", "hour"}, []string{fake.trend.MetaMcpServerID, fake.trend.From, fake.trend.To, fake.trend.Bucket}, "seven days before to, hourly")
	require.Equal(t, []int64{3, 2, 1, 4, 5, 6}, []int64{out.ActiveAtStart, out.Created, out.Revoked, out.ActiveAtEnd, out.ExcludedLegacyRows, *out.UnattributedRows})
	require.Equal(t, []GatewayConnectionTrendBucket{{Start: "2026-01-01T00:00:00Z", End: "2026-01-08T00:00:00Z", Created: 2, Revoked: 1, Active: 4}}, out.Buckets)
	require.Equal(t, project.String(), out.ProjectID)

	_, err = service.Trend(gatewayConnectionsContext(t), testPrincipal(), GatewayConnectionTrendInput{ProjectID: project.String(), GatewayID: gateway.String(), From: "2026-01-01T00:00:00Z", To: "2026-01-31T00:00:00Z", Bucket: ""})
	require.NoError(t, err)
	require.Equal(t, "day", fake.trend.Bucket, "a window over seven days defaults to days")
}

func TestGatewayConnectionTrendRefusals(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	invalid := []GatewayConnectionTrendInput{
		{ProjectID: "", GatewayID: uuid.NewString(), From: "", To: "", Bucket: ""},
		{ProjectID: project.String(), GatewayID: "nope", From: "", To: "", Bucket: ""},
		{ProjectID: project.String(), GatewayID: uuid.NewString(), From: "yesterday", To: "", Bucket: ""},
	}
	for _, input := range invalid {
		_, err := gatewayConnectionsTestService(&gatewaySessionsFake{}, project, nil).Trend(gatewayConnectionsContext(t), testPrincipal(), input)
		require.ErrorIs(t, err, errGatewayConnectionTrendInvalid)
	}

	hidden := &gatewaySessionsFake{trendErr: oops.E(oops.CodeNotFound, nil, "gateway not found")}
	_, err := gatewayConnectionsTestService(hidden, project, nil).Trend(gatewayConnectionsContext(t), testPrincipal(), GatewayConnectionTrendInput{ProjectID: project.String(), GatewayID: uuid.NewString(), From: "", To: "", Bucket: ""})
	result, ok := gatewayConnectionsToolResult(err)
	require.True(t, ok)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "not_found", "a gateway the caller may not read is not found")

	denied := &gatewaySessionsFake{trendErr: oops.C(oops.CodeForbidden)}
	_, err = gatewayConnectionsTestService(denied, project, nil).Trend(gatewayConnectionsContext(t), testPrincipal(), GatewayConnectionTrendInput{ProjectID: project.String(), GatewayID: uuid.NewString(), From: "", To: "", Bucket: ""})
	require.ErrorIs(t, err, ErrForbidden)
}
