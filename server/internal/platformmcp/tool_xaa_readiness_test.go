package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

const xaaProjectID = "11111111-1111-4111-8111-111111111111"
const xaaServerID = "22222222-2222-4222-8222-222222222222"

type xaaReaderStub struct {
	result *srv.ListOktaResourceConnectionsResult
	err    error
	calls  int
}

func (s *xaaReaderStub) List(_ context.Context, payload *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error) {
	s.calls++
	if !payload.IncludeAll {
		return nil, errors.New("readiness must include non-pending states")
	}
	return s.result, s.err
}

func xaaTestSession(t *testing.T, service *xaaReadinessService, deny bool) *mcp.ClientSession {
	t.Helper()
	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	if deny {
		reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	} else {
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	}
	registerXAAReadinessTool(reg, service)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "xaa-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func xaaCall(t *testing.T, session *mcp.ClientSession, projectID, serverID string) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_xaa_readiness", Arguments: map[string]any{"project_id": projectID, "mcp_server_id": serverID}})
	require.NoError(t, err)
	return result
}

func TestXAAReadinessContract(t *testing.T) {
	t.Parallel()
	live, unavailable := newRegistrar(newTestMCPServer()), newRegistrar(newTestMCPServer())
	registerXAAReadinessTool(live, &xaaReadinessService{})
	registerXAAReadinessTool(unavailable, nil)
	a, b := live.Descriptors()[0], unavailable.Descriptors()[0]
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.output, b.output)
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, discoveryMCPRead, a.Meta.DiscoveryScopes)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.True(t, a.Annotations.ReadOnlyHint)
	require.Empty(t, live.For(AudienceAssistant))
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(a.InputSchema, &schema))
	require.ElementsMatch(t, []string{"project_id", "mcp_server_id"}, schema.Required)
	principal := testPrincipal()
	tools := []*mcp.Tool{{Name: a.Name}}
	admin := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	require.Empty(t, live.FilterExternalTools(admin, principal, tools))
	both := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeMCPRead, xaaServerID)})
	require.Len(t, live.FilterExternalTools(both, principal, tools), 1)
}

func TestXAAReadinessProjectsExistingStatesWithoutLeakingSnapshot(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"not_applicable", "needs_agent", "needs_connection", "broken", "connected", "verified"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			observation, at, reason := "success", "2026-01-01T00:00:00Z", "downstream_rejected"
			reader := &xaaReaderStub{result: &srv.ListOktaResourceConnectionsResult{TotalCount: 99, Servers: []*srv.OktaResourceConnectionServer{
				{ProjectID: xaaProjectID, McpServerID: "hidden-server", State: "hidden-state"},
				{ProjectID: xaaProjectID, McpServerID: xaaServerID, State: state, Pending: true, BrokenReason: &reason, ObservedResult: &observation, ObservedAt: &at, ServerName: "private-label", ResourceIndicator: "https://private.invalid", ClientID: &observation},
			}}}
			session := xaaTestSession(t, &xaaReadinessService{connections: reader, enabled: func(context.Context, string) (bool, error) { return true, nil }}, false)
			result := xaaCall(t, session, xaaProjectID, xaaServerID)
			require.False(t, result.IsError)
			var output GetXAAReadinessOutput
			wire, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(wire, &output))
			require.Equal(t, state, output.State)
			require.Equal(t, xaaProjectID, output.ProjectID)
			require.Equal(t, xaaServerID, output.MCPServerID)
			require.True(t, output.Pending)
			require.Equal(t, &observation, output.ObservedResult)
			require.Equal(t, &at, output.ObservedAt)
			require.Equal(t, &reason, output.BrokenReason)
			for _, secret := range []string{"hidden", "private", "total_count", "client_id", "servers"} {
				require.NotContains(t, string(wire), secret)
			}
			require.Equal(t, 1, reader.calls)
		})
	}
}

func TestXAAReadinessRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code            string
		enabled, deny, absent bool
		flagErr, serviceErr   error
		project, server       string
		calls                 int
	}{
		{name: "unavailable", code: unavailableCode, absent: true},
		{name: "disabled", code: unavailableCode},
		{name: "flag failure", code: unavailableCode, flagErr: errors.New("private flag detail")},
		{name: "org admin denial", code: "permission_denied", enabled: true, deny: true},
		{name: "service denial", code: "forbidden", enabled: true, serviceErr: oops.C(oops.CodeForbidden), calls: 1},
		{name: "no identity provider", code: "setup_required", enabled: true, serviceErr: oops.C(oops.CodeFailedPrecondition), calls: 1},
		{name: "service failure", code: unavailableCode, enabled: true, serviceErr: errors.New("private database detail"), calls: 1},
		{name: "hidden or missing", code: "not_found", enabled: true, calls: 1},
		{name: "wrong project", code: "not_found", enabled: true, project: "33333333-3333-4333-8333-333333333333", calls: 1},
		{name: "invalid project", code: "invalid_request", project: "not-a-uuid"},
		{name: "invalid server", code: "invalid_request", server: "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := &xaaReaderStub{err: tc.serviceErr, result: &srv.ListOktaResourceConnectionsResult{Servers: []*srv.OktaResourceConnectionServer{{ProjectID: xaaProjectID, McpServerID: "44444444-4444-4444-8444-444444444444", State: "verified"}}}}
			service := &xaaReadinessService{connections: reader, enabled: func(context.Context, string) (bool, error) { return tc.enabled, tc.flagErr }}
			if tc.absent {
				service = nil
			}
			if tc.name == "wrong project" {
				reader.result.Servers[0].McpServerID = xaaServerID
			}
			session := xaaTestSession(t, service, tc.deny)
			project, server := tc.project, tc.server
			if project == "" {
				project = xaaProjectID
			}
			if server == "" {
				server = xaaServerID
			}
			result := xaaCall(t, session, project, server)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			content, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			text := content.Text
			require.Contains(t, text, `"code":"`+tc.code+`"`)
			require.NotContains(t, text, "private")
			require.Equal(t, tc.calls, reader.calls)
		})
	}
}
