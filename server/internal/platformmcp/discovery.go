//nolint:exhaustruct // MCP SDK and Code Mode types deliberately rely on documented zero-value defaults.
package platformmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// discoveryMode is the tool surface one Platform MCP request is served.
//
// Some hosts cap the tool specs an MCP server may list — Codex hides every
// Agent Plugin tool past a 64 KB total and cuts server instructions to 1 KB —
// and the full catalogue is far past that. Those hosts are served a small
// fixed surface instead, from which every catalogue tool is still reachable.
type discoveryMode string

const (
	// discoveryFull lists the full catalogue.
	discoveryFull discoveryMode = "full"
	// discoveryProgressive lists only the search, describe and call tools.
	discoveryProgressive discoveryMode = "progressive"
	// discoveryCode adds the execute tool to the progressive surface.
	discoveryCode discoveryMode = "code_mode"
)

const (
	searchPlatformToolsName  = "search_platform_tools"
	describePlatformToolName = "describe_platform_tool"
	callPlatformToolName     = "call_platform_tool"
	executePlatformCodeName  = "execute"

	// maxDescribeTools bounds one describe_platform_tool call.
	maxDescribeTools = 20
	// maxSearchDescriptionBytes keeps search results small; the full
	// description comes back from describe_platform_tool.
	maxSearchDescriptionBytes = 320
	// codeToolServer is the one Code Mode member every catalogue tool sits in,
	// so paths read platform--<tool>.
	codeToolServer = "platform"
	codeToolPrefix = codeToolServer + "--"
)

// codeGatewayID scopes Platform MCP executions in the Code Mode coordinator.
// Platform MCP has no stored gateway, so the ID is derived and fixed.
var codeGatewayID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://speakeasy.com/platform-mcp/code-mode"))

// DiscoveryConfig supplies what choosing and serving the small surface needs.
// A zero config serves every caller the full catalogue.
type DiscoveryConfig struct {
	// Flags resolves FlagPlatformMCPDiscoveryMode.
	Flags feature.Provider
	// CodeExecutor runs execute programs. Nil keeps Code Mode unavailable.
	CodeExecutor codemode.Executor
	// CodeModeAvailable reports whether an organization can use Code Mode
	// now. Nil keeps Code Mode unavailable.
	CodeModeAvailable CodeModeAvailability
}

// WithDiscovery lets the flag move a caller's harness to the small surface.
func (r *Runtime) WithDiscovery(config DiscoveryConfig) *Runtime {
	if r != nil {
		r.discovery = config
	}
	return r
}

// harnessProducts maps each CIMD document URL in the aivendors registry to its
// product ID, resolved once because the registry is static.
var harnessProducts = sync.OnceValue(func() map[string]string {
	products := make(map[string]string)
	for _, product := range aivendors.Products() {
		for _, clientID := range aivendors.GatewayMatchersFor(product).ClientIDs {
			products[clientID] = product.ID
		}
	}
	return products
})

// harnessForClient names the harness behind a verified OAuth client_id: the
// aivendors product whose CIMD document admitted it, or "unknown". It
// resolves through the CIMD admission catalog the same way the MCP gateway
// does, never through the client's self-reported clientInfo. A dynamically
// registered client has an opaque client_id that matches no entry.
func harnessForClient(clientID string) string {
	if preset, ok := admission.CatalogPreset(clientID); ok {
		if product, ok := harnessProducts()[preset.URL]; ok {
			return product
		}
	}
	return "unknown"
}

// discoveryModeFor resolves the surface for one request. Every failure serves
// the full catalogue, the pre-rollout behaviour.
func (r *Runtime) discoveryModeFor(ctx context.Context, logger *slog.Logger, principal Principal) discoveryMode {
	if r.discovery.Flags == nil || principal.OrganizationID == "" {
		return discoveryFull
	}
	variant, err := feature.FlagVariantLocal(ctx, r.discovery.Flags, feature.FlagPlatformMCPDiscoveryMode, principal.OrganizationID, nil, map[string]string{
		"harness":         harnessForClient(principal.ClientID),
		"organization_id": principal.OrganizationID,
	})
	if err != nil {
		logger.WarnContext(ctx, "resolve platform mcp discovery mode", attr.SlogError(err))
		return discoveryFull
	}
	switch variant {
	case feature.VariantPlatformMCPDiscoveryProgressive:
		return discoveryProgressive
	case feature.VariantPlatformMCPDiscoveryCodeMode:
		if r.codeModeAvailable(ctx, principal.OrganizationID) {
			return discoveryCode
		}
		return discoveryProgressive
	default:
		return discoveryFull
	}
}

func (r *Runtime) codeModeAvailable(ctx context.Context, organizationID string) bool {
	return r.discovery.CodeExecutor != nil && r.discovery.CodeModeAvailable != nil && r.discovery.CodeModeAvailable(ctx, organizationID)
}

type discoveryModeKey struct{}

// discoveryModeFromContext reads the surface the discovery middleware chose.
// A request it never saw is a full-catalogue request.
func discoveryModeFromContext(ctx context.Context) discoveryMode {
	if mode, ok := ctx.Value(discoveryModeKey{}).(discoveryMode); ok {
		return mode
	}
	return discoveryFull
}

// discoveryMiddleware serves a caller in the small surface from the meta-tools
// plus the first-run tools. It sits outside the allowlist and catalogue
// middleware: its tools/list takes the first-run tools from the catalogue they
// filter for this caller, and a dispatched call enters them as an ordinary tools/call,
// so the meta-tools reach a catalogue tool through the same SDK validation,
// per-tool authorization, handler and fail-safe as a direct call does.
func discoveryMiddleware(runtime *Runtime, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			principal := mustPrincipal(ctx)
			mode := runtime.discoveryModeFor(ctx, logger, principal)
			ctx = context.WithValue(ctx, discoveryModeKey{}, mode)
			if mode == discoveryFull {
				return next(ctx, method, req)
			}
			switch method {
			case "tools/list":
				if listReq, ok := req.(*mcp.ListToolsRequest); ok && listReq.Params != nil && listReq.Params.Cursor != "" {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "tools/list is not paginated for this caller"}
				}
				firstRun, err := firstRunToolsFor(ctx, next, method, req)
				if err != nil {
					return nil, err
				}
				listed := &mcp.ListToolsResult{Tools: append(discoveryTools(mode), firstRun...)}
				listed.CacheScope = "private"
				return listed, nil
			case "tools/call":
				callReq, ok := req.(*mcp.CallToolRequest)
				if !ok || callReq.Params == nil {
					return next(ctx, method, req)
				}
				surface := &discoverySurface{runtime: runtime, logger: logger, principal: principal, request: callReq, next: next}
				switch callReq.Params.Name {
				case searchPlatformToolsName:
					return surface.search(ctx, callReq.Params.Arguments)
				case describePlatformToolName:
					return surface.describe(ctx, callReq.Params.Arguments)
				case callPlatformToolName:
					return surface.call(ctx, callReq.Params.Arguments)
				case executePlatformCodeName:
					if mode == discoveryCode {
						return surface.execute(ctx, callReq.Params.Arguments)
					}
				}
				return next(ctx, method, req)
			}
			result, err := next(ctx, method, req)
			if err != nil {
				return result, err
			}
			switch typed := result.(type) {
			case *mcp.InitializeResult:
				typed.Instructions = discoveryInstructions(mode)
			case *mcp.DiscoverResult:
				typed.Instructions = discoveryInstructions(mode)
				// The instructions depend on the caller, so no shared cache may
				// hand one client's answer to another.
				typed.CacheScope = "private"
			}
			return result, nil
		}
	}
}

// firstRunTools are the tools most people reach for first: find or add an MCP
// server, check it works, install it, and ask how. The small surface lists them
// directly next to the meta-tools, so these steps take one call instead of a
// search, a describe and a call. Every other tool stays behind search.
var firstRunTools = []string{
	"search_mcp_catalog",
	"inspect_mcp_candidate",
	"register_catalog_mcp",
	"register_remote_mcp",
	"find_mcp",
	"get_mcp_readiness",
	"get_my_install_instructions",
	"search_speakeasy_docs",
}

// firstRunToolsFor returns the first-run tools this caller may see, taken from
// the caller's own filtered catalogue so discovery and authorization rules
// apply unchanged. Output schemas are left out to fit the host's tool budget;
// results still carry their structured content.
func firstRunToolsFor(ctx context.Context, next mcp.MethodHandler, method string, req mcp.Request) ([]*mcp.Tool, error) {
	result, err := next(ctx, method, req)
	if err != nil {
		return nil, err
	}
	catalogue, ok := result.(*mcp.ListToolsResult)
	if !ok {
		return nil, nil
	}
	tools := make([]*mcp.Tool, 0, len(firstRunTools))
	for _, tool := range catalogue.Tools {
		if slices.Contains(firstRunTools, tool.Name) {
			listed := *tool
			listed.OutputSchema = nil
			tools = append(tools, &listed)
		}
	}
	return tools, nil
}

// discoveryInstructions replace the full server instructions, which Codex cuts
// to 1 KB. Tool-specific rules travel with describe_platform_tool instead.
func discoveryInstructions(mode discoveryMode) string {
	instructions := "Speakeasy's AI Control Plane lets an administrator put MCP servers and skills in front of the people in their organization. This connection lists the tools most people use first; call them directly. " +
		"For any other Platform tool, find it with search_platform_tools (plain words, such as \"add an MCP server to a plugin\"), call describe_platform_tool for its input schema and rules, then run it with call_platform_tool. Answer how-to and setup questions with search_speakeasy_docs and cite the pages it returns. " +
		"Write tools preview first: show the user what will change and wait for explicit confirmation before calling again with confirmed set. Never request or accept tokens, passwords or secrets in chat. Do not read tool names, IDs or error codes aloud."
	if mode == discoveryCode {
		instructions += " For multi-step work, execute runs Python that can search, describe and call every tool as platform--<tool name>."
	}
	return instructions
}

var (
	searchPlatformToolsSchema  = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","maxLength":1024,"description":"What you want to do or inspect, in plain words, or an exact tool name. Empty lists every tool."},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Results per page; default 10."},"cursor":{"type":"string","maxLength":512,"description":"next_cursor from the previous page."}},"additionalProperties":false}`)
	describePlatformToolSchema = json.RawMessage(`{"type":"object","properties":{"names":{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","maxLength":128},"description":"Exact tool names from search_platform_tools."}},"required":["names"],"additionalProperties":false}`)
	callPlatformToolSchema     = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","maxLength":128,"description":"Exact tool name."},"arguments":{"type":"object","description":"Arguments matching the tool's input schema from describe_platform_tool."}},"required":["name"],"additionalProperties":false}`)
	executePlatformCodeSchema  = json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","minLength":1,"maxLength":65536,"description":"Python program; its final JSON-compatible expression is returned."},"tools":{"type":"array","maxItems":10000,"uniqueItems":true,"items":{"type":"string"},"description":"Optional exact platform--<tool> allowlist narrowing this execution. Omit for every permitted tool; [] permits none."}},"required":["code"],"additionalProperties":false}`)
)

const executePlatformCodeDescription = `Execute Python against every Platform tool you may use. Top-level await works; await every helper. Fresh state each run; the final expression returns JSON.

Discover, then inspect:

    page = await tools.search("add an MCP server from the catalog")
    details = await tools.describe(page["items"][0]["path"]) if page["items"] else None
    {"page": page, "details": details}

Next run, call with the described inputSchema:

    r = await tools.call("platform--search_mcp_catalog", {"query": "linear"})
    r["data"] if r["ok"] else r

Paths are platform--<tool name>. search(query="", limit=10, cursor=None), max limit 50; empty query lists tools; paginate with next_cursor. Calls return {ok, data, content, outcome, error?}; check ok. A refused call (unknown tool, invalid arguments) gives ok False, error "tool_call_rejected" and the reason in content.

Write tools keep their own safeguards: preview with confirmed false and get the user's explicit confirmation before a confirmed call. Errors and cancellation never undo writes; when an outcome is unknown, read the state again and never replay automatically.

Python subset; import asyncio enables await asyncio.gather(...). No external packages, filesystem, environment, network or sleep. Limits: 64 KiB source, 30 s wall, 64 helper calls, 256 KiB result.`

// discoveryTools are the meta-tools every caller in the small surface lists.
// The first-run tools listed next to them depend on the caller; for every other
// tool, what a caller may use is decided when it searches, describes or calls.
func discoveryTools(mode discoveryMode) []*mcp.Tool {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
	tools := []*mcp.Tool{
		{
			Name:        searchPlatformToolsName,
			Title:       "Search Platform tools",
			Description: "Search the Speakeasy Platform tools you may use, ranked for a plain-language query. Returns names and short descriptions; describe a tool before calling it.",
			InputSchema: searchPlatformToolsSchema,
			Annotations: readOnly,
		},
		{
			Name:        describePlatformToolName,
			Title:       "Describe Platform tools",
			Description: "Return the full description, input schema, annotations and usage rules of named Platform tools. Names you may not use come back in not_found.",
			InputSchema: describePlatformToolSchema,
			Annotations: readOnly,
		},
		{
			Name:        callPlatformToolName,
			Title:       "Call a Platform tool",
			Description: "Run one Platform tool by exact name with arguments matching its input schema. The tool keeps its own authorization, confirmation, idempotency and audit rules, and returns its own result.",
			InputSchema: callPlatformToolSchema,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(true)},
		},
	}
	if mode == discoveryCode {
		tools = append(tools, &mcp.Tool{
			Name:        executePlatformCodeName,
			Title:       "Execute Python against Platform tools",
			Description: executePlatformCodeDescription,
			InputSchema: executePlatformCodeSchema,
			Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(true)},
		})
	}
	return tools
}

// discoverySurface serves one meta-tool call.
type discoverySurface struct {
	runtime   *Runtime
	logger    *slog.Logger
	principal Principal
	request   *mcp.CallToolRequest
	next      mcp.MethodHandler
}

// discoverable returns the catalogue tools this caller's grants make
// discoverable, keyed by name. The meta-tools never confirm or describe a tool
// outside it, so a hidden tool reads exactly like one that does not exist.
func (s *discoverySurface) discoverable(ctx context.Context) map[string]Descriptor {
	grants, grantsLoaded := authz.GrantsFromContext(ctx)
	visible := make(map[string]Descriptor)
	for _, descriptor := range s.runtime.registrar.For(AudienceExternal) {
		if externalToolDiscoverable(grants, grantsLoaded, s.principal, descriptor.Meta) {
			visible[descriptor.Name] = descriptor
		}
	}
	return visible
}

type searchPlatformToolsInput struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type platformToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type searchPlatformToolsOutput struct {
	Items      []platformToolSummary `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func (s *discoverySurface) search(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	var input searchPlatformToolsInput
	if err := decodeDiscoveryArguments(arguments, &input); err != nil {
		return discoveryErrorResult(err.Error()), nil
	}
	tools := s.discoverable(ctx)
	documents := make([]codemode.Document, 0, len(tools))
	for _, descriptor := range tools {
		documents = append(documents, codemode.Document{
			Candidate: codemode.Candidate{Path: descriptor.Name, Description: descriptor.Description},
			Name:      descriptor.Name,
			// Titles are written for people, so they rank like a display name.
			ServerName:  descriptor.Title,
			Fingerprint: descriptor.Name,
		})
	}
	page, err := codemode.Rank(s.principal.ConnectionID, codemode.SearchArgs{Query: input.Query, Limit: input.Limit, Cursor: input.Cursor}, documents, nil)
	if err != nil {
		return discoveryErrorResult(err.Error()), nil
	}
	output := searchPlatformToolsOutput{Items: make([]platformToolSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		output.Items = append(output.Items, platformToolSummary{Name: item.Path, Description: truncateUTF8(item.Description, maxSearchDescriptionBytes)})
	}
	return discoveryResult(output)
}

type describePlatformToolInput struct {
	Names []string `json:"names"`
}

type platformToolDescription struct {
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description"`
	InputSchema json.RawMessage      `json:"inputSchema"`
	Annotations *mcp.ToolAnnotations `json:"annotations,omitempty"`
	// Guidance is every server-instruction paragraph that names the tool:
	// the rules a full-catalogue caller reads up front.
	Guidance []string `json:"guidance,omitempty"`
}

type describePlatformToolOutput struct {
	Tools    []platformToolDescription `json:"tools"`
	NotFound []string                  `json:"not_found,omitempty"`
}

func (s *discoverySurface) describe(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	var input describePlatformToolInput
	if err := decodeDiscoveryArguments(arguments, &input); err != nil {
		return discoveryErrorResult(err.Error()), nil
	}
	if len(input.Names) == 0 || len(input.Names) > maxDescribeTools {
		return discoveryErrorResult(fmt.Sprintf("names must list 1 to %d tools", maxDescribeTools)), nil
	}
	tools := s.discoverable(ctx)
	output := describePlatformToolOutput{Tools: make([]platformToolDescription, 0, len(input.Names)), NotFound: nil}
	for _, name := range input.Names {
		descriptor, ok := tools[name]
		if !ok {
			output.NotFound = append(output.NotFound, name)
			continue
		}
		output.Tools = append(output.Tools, platformToolDescription{
			Name:        descriptor.Name,
			Title:       descriptor.Title,
			Description: descriptor.Description,
			InputSchema: descriptor.InputSchema,
			Annotations: descriptor.Annotations,
			Guidance:    s.runtime.registrar.guidanceFor(descriptor.Name),
		})
	}
	return discoveryResult(output)
}

// guidanceFor returns the instruction paragraphs that name a tool.
func (r *Registrar) guidanceFor(name string) []string {
	var guidance []string
	for paragraph := range strings.SplitSeq(r.instructions, "\n\n") {
		if slices.Contains(instructionToolNamePattern.FindAllString(paragraph, -1), name) {
			guidance = append(guidance, paragraph)
		}
	}
	return guidance
}

type callPlatformToolInput struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *discoverySurface) call(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	var input callPlatformToolInput
	if err := decodeDiscoveryArguments(arguments, &input); err != nil {
		return discoveryErrorResult(err.Error()), nil
	}
	result, rejection, err := s.dispatch(ctx, input.Name, input.Arguments)
	if err != nil {
		return nil, err
	}
	if rejection != "" {
		return discoveryErrorResult(rejection), nil
	}
	return result, nil
}

// dispatch runs one catalogue tool as an ordinary tools/call on the rest of
// the middleware chain and the SDK server. A rejection is the refusal a
// caller can fix — an unknown or hidden tool, or invalid arguments.
func (s *discoverySurface) dispatch(ctx context.Context, name string, arguments json.RawMessage) (*mcp.CallToolResult, string, error) {
	if _, ok := s.discoverable(ctx)[name]; !ok {
		return nil, fmt.Sprintf("unknown tool %q; find tools with %s", name, searchPlatformToolsName), nil
	}
	if len(bytes.TrimSpace(arguments)) == 0 || bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
		arguments = json.RawMessage(`{}`)
	}
	params := &mcp.CallToolParamsRaw{Name: name, Arguments: arguments}
	if s.request.Params != nil {
		params.Meta = s.request.Params.Meta
	}
	result, err := s.next(ctx, "tools/call", &mcp.CallToolRequest{Session: s.request.Session, Params: params, Extra: s.request.Extra})
	if err != nil {
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) && rpcErr.Code == jsonrpc.CodeInvalidParams {
			return nil, rpcErr.Message, nil
		}
		return nil, "", err
	}
	callResult, ok := result.(*mcp.CallToolResult)
	if !ok {
		return nil, "", ErrUnavailable
	}
	return callResult, "", nil
}

type executePlatformCodeInput struct {
	Code  string          `json:"code"`
	Tools json.RawMessage `json:"tools"`
}

func (s *discoverySurface) execute(ctx context.Context, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	var input executePlatformCodeInput
	if err := decodeDiscoveryArguments(arguments, &input); err != nil || len(input.Code) == 0 || len(input.Code) > codemode.MaxSourceBytes || !utf8.ValidString(input.Code) {
		return discoveryErrorResult("execute requires valid Python source of 1 to 65536 bytes and an optional tools allowlist"), nil
	}
	var selected []string
	if len(input.Tools) > 0 {
		if bytes.Equal(bytes.TrimSpace(input.Tools), []byte("null")) || json.Unmarshal(input.Tools, &selected) != nil {
			return discoveryErrorResult("tools must be an array of exact platform--<tool> paths"), nil
		}
	}
	// The endpoint is stateless, so no later request can name this one to
	// cancel it; a fresh ID keeps every execution distinct to the coordinator.
	requestID, err := json.Marshal(uuid.NewString())
	if err != nil {
		return nil, fmt.Errorf("encode execution request ID: %w", err)
	}
	scope := codemode.Scope{
		Organization:     s.principal.OrganizationID,
		OrganizationWide: true,
		Gateway:          codeGatewayID,
		Caller:           []string{s.principal.UserID, s.principal.ConnectionID, s.principal.ClientID},
		Session:          s.principal.ConnectionID + ":" + s.principal.Generation,
		Request:          requestID,
	}
	backend := &codeBackend{surface: s}
	factory := codemode.HostFactory(func(runCtx context.Context) (codemode.Host, error) {
		return codemode.NewHost(runCtx, backend, codeGatewayID.String(), selected)
	})
	execution, err := s.runtime.discovery.CodeExecutor.Execute(ctx, scope, input.Code, factory)
	if err != nil {
		return discoveryErrorResult(codeAdmissionMessage(ctx, s.logger, err)), nil
	}
	structured, err := json.Marshal(execution)
	if err != nil {
		return nil, fmt.Errorf("encode code execution: %w", err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(structured)}},
		StructuredContent: json.RawMessage(structured),
		IsError:           execution.Error != nil,
	}, nil
}

// codeAdmissionMessage explains why a program was never submitted. Every one
// of these leaves nothing to undo, so the caller may retry.
func codeAdmissionMessage(ctx context.Context, logger *slog.Logger, err error) string {
	switch {
	case errors.Is(err, codemode.ErrCapacity):
		return "code_runtime_busy: too many programs are running; retry shortly"
	case errors.Is(err, codemode.ErrRuntimeUnavailable):
		return "code_runtime_unavailable: use search_platform_tools and call_platform_tool instead"
	case errors.Is(err, codemode.ErrCancelledBeforeSubmit):
		return "code_execution_cancelled: the program never started"
	default:
		logger.WarnContext(ctx, "platform mcp code execution was not admitted", attr.SlogError(err))
		return "code_runtime_unavailable: use search_platform_tools and call_platform_tool instead"
	}
}

// codeBackend serves Code Mode the caller's discoverable catalogue as one
// member and dispatches its calls through the same path call_platform_tool
// uses. Helper callbacks run on contexts derived from the request's, so each
// call carries the request's identity, grants and audit attribution.
type codeBackend struct {
	surface *discoverySurface
}

var _ codemode.Backend = (*codeBackend)(nil)

func (b *codeBackend) Members(context.Context) ([]codemode.Server, codemode.Lister, error) {
	return []codemode.Server{{Slug: codeToolServer, Name: "Speakeasy Platform"}}, b.List, nil
}

func (b *codeBackend) List(ctx context.Context, server string) (*codemode.Catalog, error) {
	if server != codeToolServer {
		return nil, codemode.ErrToolUnavailable
	}
	tools := b.surface.discoverable(ctx)
	catalog := &codemode.Catalog{Tools: make([]codemode.Tool, 0, len(tools))}
	for _, descriptor := range tools {
		tool, err := codeTool(descriptor, b.surface.runtime.registrar.guidanceFor(descriptor.Name))
		if err != nil {
			return nil, err
		}
		catalog.Tools = append(catalog.Tools, tool)
	}
	return catalog, nil
}

func (b *codeBackend) Describe(ctx context.Context, path string) (*codemode.Tool, error) {
	name, ok := strings.CutPrefix(path, codeToolPrefix)
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	descriptor, ok := b.surface.discoverable(ctx)[name]
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	tool, err := codeTool(descriptor, b.surface.runtime.registrar.guidanceFor(name))
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

func (b *codeBackend) Call(ctx context.Context, path string, arguments json.RawMessage) (json.RawMessage, error) {
	name, ok := strings.CutPrefix(path, codeToolPrefix)
	if !ok {
		return nil, &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable"}
	}
	result, rejection, err := b.surface.dispatch(ctx, name, arguments)
	if err != nil {
		return nil, &codemode.DispatchError{Code: "tool_call_failed"}
	}
	if rejection != "" {
		return nil, &codemode.DispatchError{Rejection: rejection}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, &codemode.DispatchError{Code: "tool_result_unavailable"}
	}
	return raw, nil
}

// codeTool projects a descriptor as a Code Mode tool. The definition is the
// MCP tool shape describe returns, with guidance for the tool alongside.
func codeTool(descriptor Descriptor, guidance []string) (codemode.Tool, error) {
	definition, err := json.Marshal(struct {
		Name        string               `json:"name"`
		Title       string               `json:"title,omitempty"`
		Description string               `json:"description"`
		InputSchema json.RawMessage      `json:"inputSchema"`
		Annotations *mcp.ToolAnnotations `json:"annotations,omitempty"`
		Guidance    []string             `json:"guidance,omitempty"`
	}{Name: codeToolPrefix + descriptor.Name, Title: descriptor.Title, Description: descriptor.Description, InputSchema: descriptor.InputSchema, Annotations: descriptor.Annotations, Guidance: guidance})
	if err != nil {
		return codemode.Tool{}, fmt.Errorf("encode %s definition: %w", descriptor.Name, err)
	}
	return codemode.Tool{
		Path:        codeToolPrefix + descriptor.Name,
		Name:        descriptor.Name,
		Description: descriptor.Description,
		Definition:  definition,
		Fingerprint: fmt.Sprintf("%x", sha256.Sum256(definition)),
	}, nil
}

func decodeDiscoveryArguments(arguments json.RawMessage, target any) error {
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("invalid arguments: trailing data")
	}
	return nil
}

func discoveryResult(output any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("encode discovery result: %w", err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: json.RawMessage(raw),
	}, nil
}

func discoveryErrorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}, IsError: true}
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + "…"
}
