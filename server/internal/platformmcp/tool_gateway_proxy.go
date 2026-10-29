//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
)

// These tools let a person use the MCP servers their organization gateway
// reaches without adding the gateway as a second connection. They are the
// gateway's own Progressive tools, run as the Platform MCP caller: the same
// members, the same per-member authorization, and the caller's own upstream
// credentials. No consent catalog narrows them; live grants alone decide.
const (
	listMyServersName = "list_my_servers"
	searchMyToolsName = "search_my_tools"
	describeToolsName = "describe_tools"
	executeToolName   = "execute_tool"

	// gatewaySearchTool is the bridge's cross-server search, which the
	// gateway itself does not serve.
	gatewaySearchTool = "search_tools"

	gatewayProxyFeature = "gateway_proxy"
)

// gatewayProxyMeta is shared by the three tools and their unavailable
// registrations. They act as the caller's own person, so only an external
// client asks for them; members span projects, so no project is named.
var gatewayProxyMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone}

// GatewayProxy answers one organization gateway tool for the Platform MCP
// caller bound to ctx and returns the tools/call result object. The MCP
// service implements it; it is an interface because that package imports
// this one.
type GatewayProxy interface {
	CallPlatformGatewayTool(ctx context.Context, organizationID, userID, tool string, arguments json.RawMessage) (json.RawMessage, error)
}

// GatewayProxyService answers list_my_servers, describe_tools and execute_tool.
type GatewayProxyService struct {
	gateway *OrganizationGatewayService
	proxy   GatewayProxy
	flags   feature.Provider
}

func (s *GatewayProxyService) valid() bool {
	return s != nil && s.gateway.valid() && s.proxy != nil && s.flags != nil
}

// MyServer is one MCP server the caller can reach through their organization gateway.
type MyServer struct {
	Slug   string `json:"slug" jsonschema:"Server slug to pass to describe_tools and execute_tool; prefixed with the project slug only when two projects share it"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty" jsonschema:"available, unavailable, or unknown until a call is made"`
}

// ListMyServersOutput lists the servers the caller can use.
type ListMyServersOutput struct {
	Servers []MyServer `json:"servers"`
	// Hidden are servers the caller hid from their gateway; their tools are
	// not served until shown again at ConnectURL.
	Hidden     []MyServer `json:"hidden,omitempty"`
	ConnectURL string     `json:"connect_url,omitempty"`
	Note       string     `json:"note,omitempty"`
}

// SearchMyToolsInput finds tools across every server the caller can use.
type SearchMyToolsInput struct {
	Query  string `json:"query" jsonschema:"What you want to do, in plain words (for example: find a customer by email), or an exact server--tool path"`
	Server string `json:"server,omitempty" jsonschema:"Limit the search to one server slug from list_my_servers"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Results per page, 1 to 50; default 10"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page of the same search"`
}

// FoundTool is one ranked tool, ready to pass to execute_tool.
type FoundTool struct {
	Server      string `json:"server"`
	Tool        string `json:"tool"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
}

// FailedServer is a server whose tools could not be searched.
type FailedServer struct {
	Server string `json:"server"`
	Reason string `json:"reason"`
}

// SearchMyToolsOutput is one page of ranked tools.
type SearchMyToolsOutput struct {
	Tools      []FoundTool `json:"tools"`
	NextCursor string      `json:"next_cursor,omitempty"`
	// Incomplete is true when a server's tools could not all be read; those
	// servers are named in FailedServers.
	Incomplete    bool           `json:"incomplete"`
	FailedServers []FailedServer `json:"failed_servers,omitempty"`
}

// DescribeToolsInput names one server and, optionally, the tools to fetch schemas for.
type DescribeToolsInput struct {
	Server string   `json:"server" jsonschema:"Server slug from list_my_servers"`
	Tools  []string `json:"tools,omitempty" jsonschema:"Tool names on that server to return input schemas for; omit to list every tool's name and description"`
}

// DescribedTool is one tool on a server.
type DescribedTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// InputSchema is set only for tools named in the request.
	InputSchema any `json:"input_schema,omitempty"`
}

// DescribeToolsOutput is a server's tools.
type DescribeToolsOutput struct {
	Server   string          `json:"server"`
	Tools    []DescribedTool `json:"tools"`
	NotFound []string        `json:"not_found,omitempty"`
}

// ExecuteToolInput calls one tool on one server.
type ExecuteToolInput struct {
	Server    string         `json:"server" jsonschema:"Server slug from list_my_servers"`
	Tool      string         `json:"tool" jsonschema:"Tool name from describe_tools"`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"Arguments matching the tool's input_schema from describe_tools"`
}

// ExecuteToolOutput is the server's answer, passed through unchanged.
type ExecuteToolOutput struct {
	Server            string `json:"server"`
	Tool              string `json:"tool"`
	IsError           bool   `json:"is_error"`
	Content           []any  `json:"content"`
	StructuredContent any    `json:"structured_content,omitempty"`
}

// proxiedToolResult is the tools/call result object the gateway returns.
type proxiedToolResult struct {
	Content           []any           `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// call runs one gateway tool. An isError answer from the gateway itself, such
// as an unknown server or one the caller has not connected, is returned as
// the refusal result to show; execute_tool passes upstream errors through
// instead, so it reads the result itself.
func (s *GatewayProxyService) call(ctx context.Context, principal Principal, tool string, arguments any) (proxiedToolResult, error) {
	var zero proxiedToolResult
	enabled, err := s.flags.IsFlagEnabled(ctx, feature.FlagPlatformMCPGatewayProxy, principal.OrganizationID, nil)
	if err != nil || !enabled {
		return zero, gatewayProxyUnavailable("Using your MCP servers through Speakeasy is not switched on for your organization yet. Call get_my_organization_gateway to connect to them directly.")
	}
	gateway, err := s.gateway.getForPlatformMCP(ctx, principal)
	if err != nil {
		return zero, err
	}
	if !gateway.Available {
		return zero, gatewayProxyUnavailable("Your organization gateway is not available (" + gateway.UnavailableReason + "). " + gateway.NextAction)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return zero, fmt.Errorf("encode %s arguments: %w", tool, err)
	}
	body, err := s.proxy.CallPlatformGatewayTool(ctx, principal.OrganizationID, principal.UserID, tool, raw)
	if err != nil {
		return zero, fmt.Errorf("call gateway %s: %w", tool, err)
	}
	var result proxiedToolResult
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("decode gateway %s result: %w", tool, err)
	}
	return result, nil
}

// structured decodes a gateway discovery answer, or refuses with its message.
func proxiedStructured[T any](result proxiedToolResult) (T, error) {
	var out T
	if result.IsError {
		return out, &ToolRefusalError{Code: "", Payload: proxiedResultText(result)}
	}
	if err := json.Unmarshal(result.StructuredContent, &out); err != nil {
		return out, fmt.Errorf("decode gateway result: %w", err)
	}
	return out, nil
}

func proxiedResultText(result proxiedToolResult) string {
	for _, content := range result.Content {
		if chunk, ok := content.(map[string]any); ok {
			if text, ok := chunk["text"].(string); ok && text != "" {
				return text
			}
		}
	}
	return "The server returned an error with no message."
}

func gatewayProxyUnavailable(message string) error {
	payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: gatewayProxyFeature, Message: message})
	if err != nil {
		return fmt.Errorf("encode gateway proxy refusal: %w", err)
	}
	return &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
}

// ListMyServers lists the servers the caller can reach.
func (s *GatewayProxyService) ListMyServers(ctx context.Context, principal Principal) (ListMyServersOutput, error) {
	result, err := s.call(ctx, principal, metamcp.ToolListServers, struct{}{})
	if err != nil {
		return ListMyServersOutput{}, err
	}
	listed, err := proxiedStructured[metamcp.ListServersResult](result)
	if err != nil {
		return ListMyServersOutput{}, err
	}
	output := ListMyServersOutput{Servers: make([]MyServer, 0, len(listed.Servers)), ConnectURL: listed.ConnectURL, Note: listed.Note}
	for _, server := range listed.Servers {
		output.Servers = append(output.Servers, MyServer{Slug: server.Slug, Name: server.Name, Status: server.Status})
	}
	for _, server := range listed.Hidden {
		output.Hidden = append(output.Hidden, MyServer{Slug: server.Slug, Name: server.Name})
	}
	return output, nil
}

// SearchMyTools ranks tools across every server the caller can use.
func (s *GatewayProxyService) SearchMyTools(ctx context.Context, principal Principal, input SearchMyToolsInput) (SearchMyToolsOutput, error) {
	result, err := s.call(ctx, principal, gatewaySearchTool, input)
	if err != nil {
		return SearchMyToolsOutput{}, err
	}
	page, err := proxiedStructured[struct {
		Items []struct {
			Server      string `json:"server"`
			Tool        string `json:"tool"`
			Description string `json:"description"`
			InputSchema any    `json:"input_schema"`
		} `json:"items"`
		NextCursor    string         `json:"next_cursor"`
		Incomplete    bool           `json:"incomplete"`
		FailedMembers []FailedServer `json:"failed_members"`
	}](result)
	if err != nil {
		return SearchMyToolsOutput{}, err
	}
	output := SearchMyToolsOutput{Tools: make([]FoundTool, 0, len(page.Items)), NextCursor: page.NextCursor, Incomplete: page.Incomplete, FailedServers: page.FailedMembers}
	for _, item := range page.Items {
		output.Tools = append(output.Tools, FoundTool(item))
	}
	return output, nil
}

// DescribeTools lists a server's tools, with input schemas for the named ones.
func (s *GatewayProxyService) DescribeTools(ctx context.Context, principal Principal, input DescribeToolsInput) (DescribeToolsOutput, error) {
	output := DescribeToolsOutput{Server: input.Server, Tools: []DescribedTool{}}
	if len(input.Tools) == 0 {
		result, err := s.call(ctx, principal, metamcp.ToolDescribeServer, map[string]string{"server": input.Server})
		if err != nil {
			return DescribeToolsOutput{}, err
		}
		described, err := proxiedStructured[metamcp.DescribeServerResult](result)
		if err != nil {
			return DescribeToolsOutput{}, err
		}
		for _, tool := range described.Tools {
			output.Tools = append(output.Tools, DescribedTool{Name: unqualifiedToolName(input.Server, tool.Name), Description: tool.Description})
		}
		return output, nil
	}

	qualified := make([]string, 0, len(input.Tools))
	for _, tool := range input.Tools {
		qualified = append(qualified, metamcp.QualifyName(input.Server, tool))
	}
	result, err := s.call(ctx, principal, metamcp.ToolDescribeTools, map[string][]string{"tools": qualified})
	if err != nil {
		return DescribeToolsOutput{}, err
	}
	described, err := proxiedStructured[metamcp.DescribeToolsResult](result)
	if err != nil {
		return DescribeToolsOutput{}, err
	}
	// One server was named, so a failure is that server's whole answer.
	if len(described.Failed) > 0 {
		return DescribeToolsOutput{}, &ToolRefusalError{Code: "", Payload: described.Failed[0].Message}
	}
	for _, tool := range described.Tools {
		var schema any
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				return DescribeToolsOutput{}, fmt.Errorf("decode %s input schema: %w", tool.Name, err)
			}
		}
		output.Tools = append(output.Tools, DescribedTool{Name: unqualifiedToolName(input.Server, tool.Name), Description: tool.Description, InputSchema: schema})
	}
	for _, name := range described.NotFound {
		output.NotFound = append(output.NotFound, unqualifiedToolName(input.Server, name))
	}
	return output, nil
}

// ExecuteTool calls one tool and returns the server's answer, errors included.
func (s *GatewayProxyService) ExecuteTool(ctx context.Context, principal Principal, input ExecuteToolInput) (ExecuteToolOutput, error) {
	arguments := input.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := s.call(ctx, principal, metamcp.ToolExecuteTool, map[string]any{
		"name":      metamcp.QualifyName(input.Server, input.Tool),
		"arguments": arguments,
	})
	if err != nil {
		return ExecuteToolOutput{}, err
	}
	output := ExecuteToolOutput{Server: input.Server, Tool: input.Tool, IsError: result.IsError, Content: result.Content, StructuredContent: nil}
	if output.Content == nil {
		output.Content = []any{}
	}
	if len(result.StructuredContent) > 0 {
		if err := json.Unmarshal(result.StructuredContent, &output.StructuredContent); err != nil {
			return ExecuteToolOutput{}, fmt.Errorf("decode %s structured content: %w", input.Tool, err)
		}
	}
	return output, nil
}

func unqualifiedToolName(server, name string) string {
	return strings.TrimPrefix(name, server+metamcp.QualifiedNameSeparator)
}

func registerGatewayProxyTools(reg *Registrar, service *GatewayProxyService) {
	listTool := &mcp.Tool{
		Name:  listMyServersName,
		Title: "List My MCP Servers",
		Description: "List the MCP servers the user can use through this Speakeasy connection: every server in their organization their access reaches, across all projects, including ones just added here. " +
			"Call it whenever the user asks for data or an action in another system (a CRM, tickets, docs, a database, an internal API) before saying you cannot help, and after adding a server. " +
			"Then call search_my_tools to find the right tool, or describe_tools for one server's tools, and execute_tool to run it. Servers that sign in upstream use the user's own connection; one they have not connected says so when called, with a link to connect it.",
		Annotations: readOnlyAnnotations(),
	}
	searchTool := &mcp.Tool{
		Name:  searchMyToolsName,
		Title: "Search My MCP Server Tools",
		Description: "Find the right tool across all of the user's MCP servers in one call, by describing the task in plain words (for example: create a ticket, look up a customer by email). " +
			"Returns ranked tools with their server, tool name, description and input_schema, ready for execute_tool. Use it whenever the user wants something done in another system; narrow with server when you already know it.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(true)},
	}
	describeTool := &mcp.Tool{
		Name:  describeToolsName,
		Title: "Describe My MCP Server Tools",
		Description: "Describe the tools of one server from list_my_servers. Omit tools to list every tool's name and description. " +
			"Pass tool names to get their input_schema, which you need before calling execute_tool.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), OpenWorldHint: new(true)},
	}
	executeTool := &mcp.Tool{
		Name:  executeToolName,
		Title: "Run My MCP Server Tool",
		Description: "Call one tool on one server from list_my_servers, as you, with arguments matching its input_schema from describe_tools. " +
			"Returns the server's answer unchanged: is_error is true when the server or tool refused, with the reason in content. The tool may change data in the system behind the server.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(true), OpenWorldHint: new(true)},
	}
	if !service.valid() {
		addTool(reg, listTool, gatewayProxyMeta, unavailableTool(gatewayProxyFeature))
		addTool(reg, searchTool, gatewayProxyMeta, unavailableTool(gatewayProxyFeature))
		addTool(reg, describeTool, gatewayProxyMeta, unavailableTool(gatewayProxyFeature))
		addTool(reg, executeTool, gatewayProxyMeta, unavailableTool(gatewayProxyFeature))
		return
	}
	noRefusal := func(error) (*mcp.CallToolResult, bool) { return nil, false }
	addTool(reg, listTool, gatewayProxyMeta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListMyServersOutput, error) {
		return principalToolCall(ctx, noRefusal, func(principal Principal) (ListMyServersOutput, error) {
			return service.ListMyServers(ctx, principal)
		})
	})
	addTool(reg, searchTool, gatewayProxyMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchMyToolsInput) (*mcp.CallToolResult, SearchMyToolsOutput, error) {
		return principalToolCall(ctx, noRefusal, func(principal Principal) (SearchMyToolsOutput, error) {
			return service.SearchMyTools(ctx, principal, input)
		})
	})
	addTool(reg, describeTool, gatewayProxyMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input DescribeToolsInput) (*mcp.CallToolResult, DescribeToolsOutput, error) {
		return principalToolCall(ctx, noRefusal, func(principal Principal) (DescribeToolsOutput, error) {
			return service.DescribeTools(ctx, principal, input)
		})
	})
	addTool(reg, executeTool, gatewayProxyMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input ExecuteToolInput) (*mcp.CallToolResult, ExecuteToolOutput, error) {
		return principalToolCall(ctx, noRefusal, func(principal Principal) (ExecuteToolOutput, error) {
			return service.ExecuteTool(ctx, principal, input)
		})
	})
}

const (
	// gatewayServersInstructionsDeadline bounds the server listing a connection
	// waits for at initialize; a slower one is left out, never waited on.
	gatewayServersInstructionsDeadline = 2 * time.Second
	// maxInstructionServers bounds how many servers the instructions name.
	maxInstructionServers = 30
)

// gatewayServersInstructionsMiddleware names the caller's own servers in the
// instructions their connection receives, so an agent knows from the first
// turn which systems it can reach and that search_my_tools reaches them. Tool
// descriptions are shared by every caller and cached by clients, so they
// cannot carry one person's servers; instructions are built per connection.
// Any failure, refusal or delay leaves the instructions as they were.
func gatewayServersInstructionsMiddleware(service *GatewayProxyService, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil || !service.valid() {
				return result, err
			}
			var instructions *string
			switch typed := result.(type) {
			case *mcp.InitializeResult:
				instructions = &typed.Instructions
			case *mcp.DiscoverResult:
				instructions = &typed.Instructions
				typed.CacheScope = "private"
			default:
				return result, nil
			}
			principal, ok := PrincipalFromContext(ctx)
			if !ok {
				return result, nil
			}
			listCtx, cancel := context.WithTimeout(ctx, gatewayServersInstructionsDeadline)
			defer cancel()
			servers, listErr := service.ListMyServers(listCtx, principal)
			if listErr != nil {
				if logger != nil {
					logger.DebugContext(ctx, "platform mcp instructions omit the caller's servers", attr.SlogError(listErr))
				}
				return result, nil
			}
			if paragraph := myServersInstructions(servers.Servers); paragraph != "" {
				*instructions = strings.TrimSpace(*instructions + "\n\n" + paragraph)
			}
			return result, nil
		}
	}
}

// myServersInstructions is the instructions paragraph naming the caller's
// servers, or empty when they have none.
func myServersInstructions(servers []MyServer) string {
	if len(servers) == 0 {
		return ""
	}
	names := make([]string, 0, min(len(servers), maxInstructionServers))
	for _, server := range servers[:min(len(servers), maxInstructionServers)] {
		if server.Name != "" && server.Name != server.Slug {
			names = append(names, server.Name+" ("+server.Slug+")")
		} else {
			names = append(names, server.Slug)
		}
	}
	paragraph := "The user's own MCP servers are usable through this connection: " + strings.Join(names, ", ")
	if more := len(servers) - len(names); more > 0 {
		paragraph += fmt.Sprintf(", and %d more", more)
	}
	return paragraph + ". When the user asks for data or an action in one of these systems, call search_my_tools with the task in plain words, then execute_tool with the server and tool it returns. " +
		"Servers added later in this conversation are usable too; call list_my_servers to see the current list."
}
