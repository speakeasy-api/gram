package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A local tool is a tool Speakeasy answers itself on an MCP server, whatever
// backs the server, such as the feedback tool. It is offered wherever the
// server's own tools are, directly or through a gateway, and is answered at
// the point a call to one of the server's own tools would be dispatched. A
// local tool therefore passes every check that path makes before dispatch,
// such as the kill switch a proxied server applies, and is never sent to the
// server's backend.
//
// A local tool takes its name on the server: a tool of the server's own with
// the same name is not listed, and calls to the name reach the local tool.

// localTool is one tool Speakeasy answers itself on one MCP server.
type localTool struct {
	// definition is the tool as agents list it.
	definition *sdkmcp.Tool

	// call answers one call to the tool. A failure the agent can act on is
	// an error result, not an error.
	call func(ctx context.Context, call localToolCall) (*sdkmcp.CallToolResult, error)
}

// localToolServer is the MCP server a local tool is offered on.
type localToolServer struct {
	// organizationID is the organization that owns the server.
	organizationID string

	// projectID is the server's project.
	projectID uuid.UUID

	// mcpServerID is the server's mcp_servers id.
	mcpServerID uuid.UUID

	// feedbackToolEnabled reports whether the server has its feedback tool
	// switched on, read with the mcp_servers row the request is served from.
	// The tool's name and description are loaded only when it is true.
	feedbackToolEnabled bool
}

// localToolCall is one call to a local tool.
type localToolCall struct {
	// arguments are the call's arguments, as the agent sent them.
	arguments json.RawMessage

	// request is the client's HTTP request, when there is one.
	request *http.Request

	// authenticated reports whether the caller presented a credential, which
	// is what makes the request's auth context the caller's own.
	authenticated bool

	// clientName and clientVersion name the MCP client, when it is known.
	clientName    string
	clientVersion string
}

// localToolSource lists the local tools one server offers. Each kind of local
// tool is one source.
type localToolSource func(ctx context.Context, server localToolServer) ([]localTool, error)

// localTools lists every local tool a server offers. When two tools share a
// name, the first is kept. A source that fails is logged and left out: local
// tools are extras, so a server's own tools and its other local tools stay
// listed and callable, and a call to the missing tool's name is served as if
// it were not there.
func (s *Service) localTools(ctx context.Context, logger *slog.Logger, server localToolServer) []localTool {
	if len(s.localToolSources) == 0 {
		return nil
	}

	var tools []localTool
	named := map[string]struct{}{}
	for _, source := range s.localToolSources {
		offered, err := source(ctx, server)
		if err != nil {
			logger.ErrorContext(ctx, "load local tools; serving without them", attr.SlogError(err), attr.SlogMcpServerID(server.mcpServerID.String()))
			continue
		}
		for _, tool := range offered {
			if tool.definition == nil || tool.call == nil {
				continue
			}
			if _, taken := named[tool.definition.Name]; taken {
				continue
			}
			named[tool.definition.Name] = struct{}{}
			tools = append(tools, tool)
		}
	}

	return tools
}

// localToolNamed returns the local tool with a name.
func localToolNamed(tools []localTool, name string) (localTool, bool) {
	for _, tool := range tools {
		if tool.definition.Name == name {
			return tool, true
		}
	}

	return localTool{definition: nil, call: nil}, false
}

// hostedLocalTools lists the local tools of the server a hosted request is
// served as. A request with no serving server, such as an internal caller's,
// is offered none.
type hostedLocalTools func(ctx context.Context, logger *slog.Logger, organizationID string, payload *mcpInputs) []localTool

// hostedLocalTools is the hostedLocalTools of this service.
func (s *Service) hostedLocalTools(ctx context.Context, logger *slog.Logger, organizationID string, payload *mcpInputs) []localTool {
	serverID := payload.servingServerID()
	if serverID == nil {
		return nil
	}

	return s.localTools(ctx, logger, localToolServer{organizationID: organizationID, projectID: payload.projectID, mcpServerID: *serverID, feedbackToolEnabled: payload.feedbackToolEnabled})
}

// localToolListEntry is a local tool as a hosted tools/list lists it.
func localToolListEntry(tool localTool) *toolListEntry {
	definition := tool.definition
	var annotations *externalmcp.ToolAnnotations
	if definition.Annotations != nil {
		idempotent := definition.Annotations.IdempotentHint
		readOnly := definition.Annotations.ReadOnlyHint
		annotations = &externalmcp.ToolAnnotations{
			Title:           definition.Annotations.Title,
			ReadOnlyHint:    &readOnly,
			DestructiveHint: definition.Annotations.DestructiveHint,
			IdempotentHint:  &idempotent,
			OpenWorldHint:   definition.Annotations.OpenWorldHint,
		}
	}

	var inputSchema json.RawMessage
	switch schema := definition.InputSchema.(type) {
	case json.RawMessage:
		inputSchema = schema
	case nil:
	default:
		encoded, err := json.Marshal(schema)
		if err == nil {
			inputSchema = encoded
		}
	}

	return &toolListEntry{
		Title:        definition.Title,
		OutputSchema: nil,
		Icons:        nil,
		Execution:    nil,
		Name:         definition.Name,
		Description:  definition.Description,
		InputSchema:  inputSchema,
		Annotations:  annotations,
		Meta:         nil,
	}
}

// withLocalTools gives local tools their names in a list of a server's tools:
// tools of the server's own that share a name with one are left out, and the
// local tools are appended. They are added after every filter has run,
// because access to them follows access to the server rather than to any one
// of the server's tools.
func withLocalTools(tools []*toolListEntry, local []localTool) []*toolListEntry {
	if len(local) == 0 {
		return tools
	}

	taken := make(map[string]struct{}, len(local))
	for _, tool := range local {
		taken[tool.definition.Name] = struct{}{}
	}

	listed := make([]*toolListEntry, 0, len(tools)+len(local))
	for _, entry := range tools {
		if _, shadowed := taken[entry.Name]; !shadowed {
			listed = append(listed, entry)
		}
	}
	for _, tool := range local {
		listed = append(listed, localToolListEntry(tool))
	}

	return listed
}

// withLocalToolsInCatalog adds a member's local tools to the catalog a
// gateway describes it with, taking their names as withLocalTools does.
func withLocalToolsInCatalog(catalog *memberCatalog, local []localTool) *memberCatalog {
	if catalog == nil || len(local) == 0 {
		return catalog
	}

	catalog.entries = withLocalTools(catalog.entries, local)
	catalog.byName = make(map[string]*toolListEntry, len(catalog.entries))
	for _, entry := range catalog.entries {
		catalog.byName[entry.Name] = entry
	}
	catalog.local = make(map[string]struct{}, len(local))
	for _, tool := range local {
		catalog.local[tool.definition.Name] = struct{}{}
	}

	return catalog
}

// withoutLocalTools is the catalog without the member's local tools: what
// consent approves and a frozen catalog holds.
func (catalog *memberCatalog) withoutLocalTools() *memberCatalog {
	if catalog == nil || len(catalog.local) == 0 {
		return catalog
	}

	kept := &memberCatalog{
		entries:      make([]*toolListEntry, 0, len(catalog.entries)),
		byName:       make(map[string]*toolListEntry, len(catalog.byName)),
		local:        nil,
		incomplete:   catalog.incomplete,
		instructions: catalog.instructions,
	}
	for _, entry := range catalog.entries {
		if _, local := catalog.local[entry.Name]; !local {
			kept.entries = append(kept.entries, entry)
		}
	}
	for name, entry := range catalog.byName {
		if _, local := catalog.local[name]; !local {
			kept.byName[name] = entry
		}
	}

	return kept
}

// answerHostedLocalToolCall answers a hosted tools/call that names a local
// tool. The call is not one of the server's tool calls: it is neither billed
// nor written to the tool logs.
func answerHostedLocalToolCall(
	ctx context.Context,
	logger *slog.Logger,
	clientInfoStore sessionClientInfoStore,
	payload *mcpInputs,
	reqID mcpjsonrpc.ID,
	tool localTool,
	arguments json.RawMessage,
	clientHint *mcprequests.SanitizedClientInfo,
) (json.RawMessage, error) {
	client, _ := resolveClientIdentity(ctx, logger, clientInfoStore, payload, clientHint)

	called, err := tool.call(ctx, localToolCall{
		arguments:     arguments,
		request:       payload.request,
		authenticated: payload.authenticated,
		clientName:    client.Name,
		clientVersion: client.Version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "answer local tool call").LogError(ctx, logger)
	}
	if called == nil {
		return nil, oops.E(oops.CodeUnexpected, errors.New("local tool returned no result"), "answer local tool call").LogError(ctx, logger)
	}
	// Counted as a served call when it succeeds, like the gateway's other own
	// tools, although no backend answered it.
	payload.resultSucceeded = !called.IsError

	content := make([]json.RawMessage, 0, len(called.Content))
	for _, item := range called.Content {
		chunk, err := json.Marshal(item)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize local tool result chunk").LogError(ctx, logger)
		}
		content = append(content, chunk)
	}

	var structured json.RawMessage
	if called.StructuredContent != nil {
		encoded, err := json.Marshal(called.StructuredContent)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize local tool structured result").LogError(ctx, logger)
		}
		structured = encoded
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content:           content,
			StructuredContent: structured,
			IsError:           called.IsError,
		},
		serverIdentity: serverInfoHostedToolset,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize local tool response").LogError(ctx, logger)
	}

	return response, nil
}

// memberLocalTools lists the local tools a gateway offers for one of its
// members. Only a member whose tools Speakeasy serves itself offers any: a
// remote or tunneled member belongs to a third party, and the gateway does not
// add tools to it.
func (s *Service) memberLocalTools(ctx context.Context, logger *slog.Logger, organizationID string, member metaMember) []localTool {
	if member.backend != metaMemberBackendHosted {
		return nil
	}

	return s.localTools(ctx, logger, localToolServer{organizationID: organizationID, projectID: member.projectID, mcpServerID: member.serverID, feedbackToolEnabled: member.feedbackToolEnabled})
}

// localToolsOnlyKey marks a context whose tools/call only a local tool may
// answer.
type localToolsOnlyKey struct{}

// withLocalToolsOnly marks a tools/call as allowed only because it names a
// local tool, as a gateway allows a member's local tools that the caller did
// not approve one by one. The member refuses such a call rather than run a
// tool of its own when the name is not, or is no longer, a local tool, so none
// of its tools runs under an approval that did not cover it.
func withLocalToolsOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, localToolsOnlyKey{}, true)
}

// localToolsOnly reports whether a context was marked by withLocalToolsOnly.
func localToolsOnly(ctx context.Context) bool {
	only, _ := ctx.Value(localToolsOnlyKey{}).(bool)
	return only
}
