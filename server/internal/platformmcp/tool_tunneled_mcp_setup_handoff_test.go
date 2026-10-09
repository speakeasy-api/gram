package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The stub must advertise the live contract: the same input and output schema,
// annotations, audiences, authorization and project scope, so the tool never
// changes shape as the dashboard origin or budget is composed.
func TestTunneledSetupHandoffStubMatchesLiveContract(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(live, &TunneledMCPSetupHandoffService{})
	stub := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(stub, nil)

	liveDescriptor := descriptorByName(t, live, getTunneledMCPSetupHandoffToolName)
	stubDescriptor := descriptorByName(t, stub, getTunneledMCPSetupHandoffToolName)
	require.JSONEq(t, string(liveDescriptor.InputSchema), string(stubDescriptor.InputSchema))
	require.Equal(t, liveDescriptor.output, stubDescriptor.output)
	require.Equal(t, liveDescriptor.Annotations, stubDescriptor.Annotations)
	require.Equal(t, liveDescriptor.Meta, stubDescriptor.Meta)
	require.Equal(t, liveDescriptor.Description, stubDescriptor.Description)

	require.Equal(t, ExternalAuthorizationOrgAdmin, liveDescriptor.Meta.Authorization)
	require.ElementsMatch(t, bothAudiences, liveDescriptor.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, liveDescriptor.Meta.ProjectScope)
	require.True(t, liveDescriptor.Annotations.ReadOnlyHint)
	require.NotNil(t, liveDescriptor.Annotations.OpenWorldHint)
	require.False(t, *liveDescriptor.Annotations.OpenWorldHint)

	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(liveDescriptor.InputSchema, &schema))
	require.Equal(t, []string{"project_id"}, schema.Required)
	require.Contains(t, schema.Properties, "mcp_id")
}

func TestTunneledSetupHandoffStubRefusesAsUnavailable(t *testing.T) {
	t.Parallel()

	stub := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(stub, nil)

	out, err := descriptorByName(t, stub, getTunneledMCPSetupHandoffToolName).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001"}`),
	)
	require.Nil(t, out)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
	require.NotContains(t, refusal.Payload, "setup_url")
}

func TestTunneledSetupHandoffComposesOnlyWithSafeDependencies(t *testing.T) {
	t.Parallel()

	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	budget := allowBudget()
	compose := func(withAuthorization bool, dashboardURL string, budget OperationBudget) *TunneledMCPSetupHandoffService {
		reader := NewPostgresReader(testenv.NewLogger(t), new(pgxpool.Pool))
		if withAuthorization {
			reader.WithAuthorization(engine)
		}
		var origin *url.URL
		if dashboardURL != "" {
			origin = mustParseURL(t, dashboardURL)
		}
		return reader.WithTunneledMCPSetupHandoff(origin, budget).tunneledSetup
	}

	require.Nil(t, compose(true, "", budget), "no dashboard origin")
	require.Nil(t, compose(true, "http://dashboard.example.test", budget), "a plain-http origin")
	require.Nil(t, compose(true, "https://user:pass@dashboard.example.test", budget), "an origin carrying credentials")
	require.Nil(t, compose(true, "https://dashboard.example.test", OperationBudget{}), "no handoff budget")
	require.Nil(t, compose(false, "https://dashboard.example.test", budget), "no authorization engine")

	service := compose(true, "https://dashboard.example.test/base?x=1#frag", budget)
	require.NotNil(t, service)
	require.Equal(t, "https://dashboard.example.test/base", service.dashboardURL.String())
}

func connectExternalTestSession(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "tunneled-setup-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func listedTool(t *testing.T, session *mcp.ClientSession, name string) *mcp.Tool {
	t.Helper()
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("%s is not listed", name)
	return nil
}

// What an external client sees over a real MCP session: the stub and the live
// tool advertise the same schemas and annotations, and the stub's call is a
// readable error result rather than an empty handoff.
func TestTunneledSetupHandoffExternalSessionContract(t *testing.T) {
	t.Parallel()

	liveServer := newTestMCPServer()
	bindExternalTestPrincipal(liveServer)
	live := newRegistrar(liveServer)
	live.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerTunneledMCPSetupHandoffTool(live, &TunneledMCPSetupHandoffService{})

	stubServer := newTestMCPServer()
	bindExternalTestPrincipal(stubServer)
	stub := newRegistrar(stubServer)
	stub.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerTunneledMCPSetupHandoffTool(stub, nil)

	stubSession := connectExternalTestSession(t, stubServer)
	liveTool := listedTool(t, connectExternalTestSession(t, liveServer), getTunneledMCPSetupHandoffToolName)
	stubTool := listedTool(t, stubSession, getTunneledMCPSetupHandoffToolName)
	liveSchemas, err := json.Marshal([]any{liveTool.InputSchema, liveTool.OutputSchema, liveTool.Annotations})
	require.NoError(t, err)
	stubSchemas, err := json.Marshal([]any{stubTool.InputSchema, stubTool.OutputSchema, stubTool.Annotations})
	require.NoError(t, err)
	require.JSONEq(t, string(liveSchemas), string(stubSchemas))
	require.NotNil(t, liveTool.OutputSchema)

	result, err := stubSession.CallTool(t.Context(), &mcp.CallToolParams{Name: getTunneledMCPSetupHandoffToolName, Arguments: map[string]any{"project_id": "00000000-0000-0000-0000-000000000001"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"feature_unavailable","feature":"tunneled_mcp_setup","message":"Tunneled MCP setup handoffs are unavailable on this server."}`, text.Text)
}

// The external endpoint applies organization administration before the
// handler runs, so a denied caller never reaches the handoff.
func TestTunneledSetupHandoffExternalDenialSkipsHandler(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	registrar := newRegistrar(server)
	registrar.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", RequestAccessURL: "", cause: ErrForbidden}})
	// A service without dependencies would answer "unavailable" if reached.
	registerTunneledMCPSetupHandoffTool(registrar, &TunneledMCPSetupHandoffService{})

	result, err := connectExternalTestSession(t, server).CallTool(t.Context(), &mcp.CallToolParams{Name: getTunneledMCPSetupHandoffToolName, Arguments: map[string]any{"project_id": "00000000-0000-0000-0000-000000000001"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"permission_denied"`)
	require.NotContains(t, text.Text, "setup_url")
}
