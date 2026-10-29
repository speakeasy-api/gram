package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// recordingGatewayProxy answers each gateway tool with a canned result and
// records who asked for what.
type recordingGatewayProxy struct {
	results map[string]string
	calls   []recordedGatewayCall
}

type recordedGatewayCall struct {
	organizationID string
	userID         string
	tool           string
	arguments      map[string]any
}

func (p *recordingGatewayProxy) CallPlatformGatewayTool(_ context.Context, organizationID, userID, tool string, arguments json.RawMessage) (json.RawMessage, error) {
	var decoded map[string]any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return nil, fmt.Errorf("decode recorded arguments: %w", err)
	}
	p.calls = append(p.calls, recordedGatewayCall{organizationID: organizationID, userID: userID, tool: tool, arguments: decoded})
	result, ok := p.results[tool]
	if !ok {
		return nil, errors.New("unexpected gateway tool " + tool)
	}
	return json.RawMessage(result), nil
}

func newGatewayProxyFixture(t *testing.T, issuer bool, enabled bool) (context.Context, *GatewayProxyService, *recordingGatewayProxy, Principal) {
	t.Helper()
	ctx, _, service, proxy, principal := newGatewayProxyFixtureWithDB(t, issuer, enabled)
	return ctx, service, proxy, principal
}

func newGatewayProxyFixtureWithDB(t *testing.T, issuer bool, enabled bool) (context.Context, *pgxpool.Pool, *GatewayProxyService, *recordingGatewayProxy, Principal) {
	t.Helper()
	ctx, conn, gateway, principal, _ := newOrganizationGatewayFixture(t)
	if issuer {
		createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	}
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPGatewayProxy, principal.OrganizationID, enabled)
	proxy := &recordingGatewayProxy{results: map[string]string{}, calls: nil}
	return ctx, conn, &GatewayProxyService{gateway: gateway, proxy: proxy, flags: flags}, proxy, principal
}

// Platform MCP uses the gateway in an organization that never configured it,
// although get_my_organization_gateway still reports it off there.
func TestGatewayProxyTreatsAnUnconfiguredGatewayAsOn(t *testing.T) {
	t.Parallel()
	ctx, conn, service, proxy, principal := newGatewayProxyFixtureWithDB(t, true, true)
	_, err := testrepo.New(conn).ClearOrganizationFeatureSettingFixture(ctx, testrepo.ClearOrganizationFeatureSettingFixtureParams{OrganizationID: principal.OrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)})
	require.NoError(t, err)
	proxy.results[metamcp.ToolListServers] = `{"content":[],"structuredContent":{"servers":[]}}`

	gateway, err := service.gateway.Get(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, "turned_off", gateway.UnavailableReason)

	_, err = service.ListMyServers(ctx, principal)
	require.NoError(t, err)
	require.Len(t, proxy.calls, 1)
}

// An administrator who turned the gateway off turns these tools off too.
func TestGatewayProxyRespectsAnExplicitTurnOff(t *testing.T) {
	t.Parallel()
	ctx, conn, service, proxy, principal := newGatewayProxyFixtureWithDB(t, true, true)
	_, err := featuresrepo.New(conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: principal.OrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)})
	require.NoError(t, err)

	_, err = service.ListMyServers(ctx, principal)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "turned_off")
	require.Empty(t, proxy.calls)
}

func TestGatewayProxyRefusesWhenFlagIsOff(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, false)

	_, err := service.ListMyServers(ctx, principal)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
	require.Contains(t, refusal.Payload, "not switched on")
	require.Empty(t, proxy.calls)
}

func TestGatewayProxyRefusesWithoutAnOrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, false, true)

	_, err := service.ListMyServers(ctx, principal)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "no_organization_issuer")
	require.Empty(t, proxy.calls)
}

func TestGatewayProxyListsServersAsTheCaller(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolListServers] = `{"content":[],"structuredContent":{"servers":[{"slug":"crm","name":"CRM","sortOrder":0,"status":"available"}],"hidden":[{"slug":"wiki","name":"Wiki"}],"connectUrl":"https://example.test/connect","note":"hidden"}}`

	output, err := service.ListMyServers(ctx, principal)

	require.NoError(t, err)
	require.Equal(t, []MyServer{{Slug: "crm", Name: "CRM", Status: "available"}}, output.Servers)
	require.Equal(t, []MyServer{{Slug: "wiki", Name: "Wiki", Status: ""}}, output.Hidden)
	require.Equal(t, "https://example.test/connect", output.ConnectURL)
	require.Len(t, proxy.calls, 1)
	require.Equal(t, principal.OrganizationID, proxy.calls[0].organizationID)
	require.Equal(t, principal.UserID, proxy.calls[0].userID)
}

func TestGatewayProxyDescribesToolsByPlainName(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolDescribeServer] = `{"content":[],"structuredContent":{"server":{"slug":"crm","sortOrder":0,"status":"available"},"tools":[{"name":"crm--find_contact","description":"Find a contact"}]}}`
	proxy.results[metamcp.ToolDescribeTools] = `{"content":[],"structuredContent":{"tools":[{"name":"crm--find_contact","inputSchema":{"type":"object"}}],"not_found":["crm--missing"]}}`

	catalog, err := service.DescribeTools(ctx, principal, DescribeToolsInput{Server: "crm", Tools: nil})
	require.NoError(t, err)
	require.Equal(t, []DescribedTool{{Name: "find_contact", Description: "Find a contact", InputSchema: nil}}, catalog.Tools)
	require.Equal(t, map[string]any{"server": "crm"}, proxy.calls[0].arguments)

	schemas, err := service.DescribeTools(ctx, principal, DescribeToolsInput{Server: "crm", Tools: []string{"find_contact", "missing"}})
	require.NoError(t, err)
	require.Equal(t, []DescribedTool{{Name: "find_contact", Description: "", InputSchema: map[string]any{"type": "object"}}}, schemas.Tools)
	require.Equal(t, []string{"missing"}, schemas.NotFound)
	require.Equal(t, map[string]any{"tools": []any{"crm--find_contact", "crm--missing"}}, proxy.calls[1].arguments)
}

// A gateway refusal, such as an unknown server, is shown to the agent as the
// gateway wrote it.
func TestGatewayProxyRelaysGatewayRefusals(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolDescribeServer] = `{"content":[{"type":"text","text":"unknown server \"nope\""}],"isError":true}`

	_, err := service.DescribeTools(ctx, principal, DescribeToolsInput{Server: "nope", Tools: nil})

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, `unknown server "nope"`, refusal.Payload)
}

// An upstream tool error is the tool's answer, not a refusal.
func TestGatewayProxyExecutePassesTheServersAnswerThrough(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolExecuteTool] = `{"content":[{"type":"text","text":"contact not found"}],"structuredContent":{"code":404},"isError":true}`

	output, err := service.ExecuteTool(ctx, principal, ExecuteToolInput{Server: "crm", Tool: "find_contact", Arguments: map[string]any{"email": "a@example.test"}})

	require.NoError(t, err)
	require.True(t, output.IsError)
	require.Equal(t, []any{map[string]any{"type": "text", "text": "contact not found"}}, output.Content)
	require.Equal(t, map[string]any{"code": float64(404)}, output.StructuredContent)
	require.Equal(t, map[string]any{"name": "crm--find_contact", "arguments": map[string]any{"email": "a@example.test"}}, proxy.calls[0].arguments)
}

// Over the MCP transport the three tools list, validate their output against
// the declared schemas, and run as the connected principal.
func TestGatewayProxyToolsServeOverMCP(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolExecuteTool] = `{"content":[{"type":"text","text":"ok"}],"structuredContent":{"id":7}}`

	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerGatewayProxyTools(reg, service)
	session := connectTestClient(t, server)

	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, []string{listMyServersName, searchMyToolsName, describeToolsName, executeToolName}, names)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: executeToolName, Arguments: map[string]any{"server": "crm", "tool": "create", "arguments": map[string]any{"name": "Ada"}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	body, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output ExecuteToolOutput
	require.NoError(t, json.Unmarshal(body, &output))
	require.Equal(t, map[string]any{"id": float64(7)}, output.StructuredContent)
	require.Equal(t, principal.UserID, proxy.calls[0].userID)
}

func TestGatewayProxyToolsAreUnavailableWithoutAProxy(t *testing.T) {
	t.Parallel()
	server := newTestMCPServer()
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})

	registerGatewayProxyTools(reg, nil)

	session := connectTestClient(t, server)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: listMyServersName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
}

func TestGatewayProxySearchesAcrossServers(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[gatewaySearchTool] = `{"content":[],"structuredContent":{"items":[{"server":"crm","tool":"find_contact","description":"Find a contact","input_schema":{"type":"object"}}],"next_cursor":"c2","incomplete":true,"failed_members":[{"server":"wiki","reason":"catalog_unavailable"}]}}`

	output, err := service.SearchMyTools(ctx, principal, SearchMyToolsInput{Query: "find a customer", Server: "", Limit: 5, Cursor: ""})

	require.NoError(t, err)
	require.Equal(t, []FoundTool{{Server: "crm", Tool: "find_contact", Description: "Find a contact", InputSchema: map[string]any{"type": "object"}}}, output.Tools)
	require.Equal(t, "c2", output.NextCursor)
	require.True(t, output.Incomplete)
	require.Equal(t, []FailedServer{{Server: "wiki", Reason: "catalog_unavailable"}}, output.FailedServers)
	require.Equal(t, map[string]any{"query": "find a customer", "limit": float64(5)}, proxy.calls[0].arguments)
}

// Each connection's instructions name the caller's own servers.
func TestGatewayServersInstructionsNameTheCallersServers(t *testing.T) {
	t.Parallel()
	ctx, service, proxy, principal := newGatewayProxyFixture(t, true, true)
	proxy.results[metamcp.ToolListServers] = `{"content":[],"structuredContent":{"servers":[{"slug":"crm","name":"CRM","sortOrder":0,"status":"available"},{"slug":"wiki","name":"wiki","sortOrder":0,"status":"unknown"}]}}`
	initialize := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.InitializeResult{Instructions: "Base."}, nil
	}

	result, err := gatewayServersInstructionsMiddleware(service, nil)(initialize)(contextWithPrincipal(ctx, principal), "initialize", nil)

	require.NoError(t, err)
	initialized, ok := result.(*mcp.InitializeResult)
	require.True(t, ok)
	instructions := initialized.Instructions
	require.True(t, strings.HasPrefix(instructions, "Base.\n\n"))
	require.Contains(t, instructions, "CRM (crm), wiki.")
	require.Contains(t, instructions, searchMyToolsName)
}

// A refusal leaves the instructions unchanged rather than failing initialize.
func TestGatewayServersInstructionsFailOpen(t *testing.T) {
	t.Parallel()
	ctx, service, _, principal := newGatewayProxyFixture(t, true, false)
	initialize := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.InitializeResult{Instructions: "Base."}, nil
	}

	result, err := gatewayServersInstructionsMiddleware(service, nil)(initialize)(contextWithPrincipal(ctx, principal), "initialize", nil)

	require.NoError(t, err)
	initialized, ok := result.(*mcp.InitializeResult)
	require.True(t, ok)
	require.Equal(t, "Base.", initialized.Instructions)
}

func TestMyServersInstructionsCapsTheList(t *testing.T) {
	t.Parallel()
	servers := make([]MyServer, 0, maxInstructionServers+3)
	for i := range maxInstructionServers + 3 {
		servers = append(servers, MyServer{Slug: fmt.Sprintf("s%d", i), Name: "", Status: ""})
	}

	paragraph := myServersInstructions(servers)

	require.Contains(t, paragraph, "and 3 more")
	require.NotContains(t, paragraph, fmt.Sprintf("s%d", maxInstructionServers))
	require.Empty(t, myServersInstructions(nil))
}
