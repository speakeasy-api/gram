// The meta-server MCP surface: protocol termination for meta-MCP-backed
// /mcp/{slug} endpoints. This surface answers MCP 2026-07-28 — including the
// sessionless server/discover method and per-request protocol-version
// declarations — and exposes the fixed gateway tool contract (list_servers,
// describe_server, describe_tools, execute_tool). Member session
// orchestration and execution routing land with the meta-server runtime
// (AGE-3291); until then the discovery drill-down tools beyond list_servers
// answer with a deterministic not-implemented error.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const metaServerMaxBodyBytes = 1 << 20

const (
	listServersToolName    = "list_servers"
	describeServerToolName = "describe_server"
)

var metaListServersSchema = json.RawMessage(`{
	"type": "object",
	"properties": {},
	"additionalProperties": false
}`)

var metaDescribeServerSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"server": {
			"type": "string",
			"description": "Slug of the member server to describe, as returned by list_servers."
		}
	},
	"required": ["server"],
	"additionalProperties": false
}`)

var metaDescribeToolsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tools": {
			"type": "array",
			"items": {
				"type": "string",
				"description": "Qualified tool name (serverslug--toolname) to fetch the full input schema for."
			},
			"description": "Qualified tool names to describe."
		}
	},
	"required": ["tools"],
	"additionalProperties": false
}`)

// serveResolvedMetaMCPEndpoint terminates MCP for a meta-MCP-backed
// endpoint: it runs the issuer gate when the meta server is issuer-gated,
// then dispatches the JSON-RPC request. Only POST reaches here — GET/DELETE
// on /mcp/{slug} stay with their existing handlers, which treat meta-backed
// endpoints as having no proxied stream and no upstream session.
func (s *Service) serveResolvedMetaMCPEndpoint(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, logger, func() error {
		return r.Body.Close()
	})

	logger = logger.With(attr.SlogMetaMcpServerID(metaServer.ID.String()))

	// The version in effect for this exchange is stable regardless of
	// outcome — this surface always answers ServedMetaServer — so the header
	// is stamped before the issuer gate and body parsing can bail out.
	w.Header().Set(mcpversions.HTTPHeader, mcpversions.ServedMetaServer)

	if metaServer.UserSessionIssuerID.Valid {
		resolvedEndpoint, err := s.BuildResolvedMcpEndpointForMetaServer(ctx, logger, mcpEndpoint, metaServer, "mcp")
		if err != nil {
			return err
		}
		// Upstream member tokens and per-session tool selection are runtime
		// concerns (AGE-3291); the gate's authentication outcome is all this
		// surface consumes today.
		newCtx, _, _, err := s.ApplyIssuerGate(ctx, w, httpheaders.AuthorizationBearerToken(r), s.BaseURLForRequest(r), resolvedEndpoint)
		if err != nil {
			return fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		r = r.WithContext(ctx)
	}

	r.Body = http.MaxBytesReader(w, r.Body, metaServerMaxBodyBytes)

	bodyBytes, err := io.ReadAll(r.Body)
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.Is(err, io.EOF) || len(bodyBytes) == 0:
		return nil
	case errors.As(err, &maxBytesErr):
		return oops.E(oops.CodeRequestTooLarge, err, "meta mcp request body exceeds 1 MiB").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").LogError(ctx, logger)
	}

	if bodyBytes[0] == '[' {
		return oops.E(oops.CodeBadRequest, nil, "batch requests are not supported").LogError(ctx, logger)
	}

	var req rawRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").LogError(ctx, logger)
	}
	if req.JSONRPC != "2.0" {
		return oops.E(oops.CodeBadRequest, errInvalidJSONRPCVersion, "unsupported JSON-RPC version").LogError(ctx, logger)
	}

	body, err := s.handleMetaMCPRequest(ctx, logger, mcpEndpoint, metaServer, &req, r.Header.Get(mcpversions.HTTPHeader))

	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		bs, merr := json.Marshal(oops.NewMCPErrorFromCause(req.ID, err))
		if merr != nil {
			return oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").LogError(ctx, logger)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(bs); writeErr != nil {
			return oops.E(oops.CodeUnexpected, writeErr, "failed to write error response body").LogError(ctx, logger)
		}
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(body); writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}
	return nil
}

func (s *Service) handleMetaMCPRequest(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	req *rawRequest,
	protocolVersionHeader string,
) (json.RawMessage, error) {
	// Census parity with the hosted and platform dispatches.
	s.metrics.RecordMCPRequest(ctx, mcprequests.DeclaredProtocolVersion(protocolVersionHeader, req.Params), req.Method, mcpmetrics.SurfaceMeta)

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	if err := validateMetaDeclaredProtocolVersion(req, protocolVersionHeader); err != nil {
		return nil, err
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, logger, req.ID, serverInfoMetaServer)
	case "initialize":
		return s.handleMetaInitialize(ctx, logger, mcpEndpoint, metaServer, req)
	case "server/discover":
		return s.handleMetaServerDiscover(ctx, logger, mcpEndpoint, metaServer, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return s.listMetaServerTools(ctx, logger, req)
	case "tools/call":
		return s.callMetaServerTool(ctx, logger, mcpEndpoint, metaServer, req)
	default:
		return nil, oops.E(oops.CodeNotImplemented, nil, "%s: %s", req.Method, oops.MCPCodeMethodNotFound.Message())
	}
}

// unparseableVersionPlaceholder stands in for a declared protocol version
// whose raw bytes did not survive sanitization: error messages must never
// echo hostile bytes back to the client.
const unparseableVersionPlaceholder = "(unparseable)"

// metaProtocolVersionMetaKey is the params-level `_meta` member carrying MCP
// 2026-07-28's per-request protocol-version declaration.
const metaProtocolVersionMetaKey = "io.modelcontextprotocol/protocolVersion"

// validateMetaDeclaredProtocolVersion enforces MCP 2026-07-28's per-request
// version declaration on the meta surface. A declaration may arrive in the
// MCP-Protocol-Version header, the params-level
// io.modelcontextprotocol/protocolVersion _meta key, or both; conflicting,
// unrecognized, or unparseable declarations produce deterministic structured
// errors naming the supported set. Only a genuinely absent declaration is
// accepted, for backward compatibility with handshake-based clients per the
// specification's versioning rules — a declaration that is present but
// unsanitizable (or not a string at all) is a malformed value, not an
// absent one.
func validateMetaDeclaredProtocolVersion(req *rawRequest, headerValue string) error {
	headerDeclared := strings.TrimSpace(headerValue) != ""
	headerVersion := mcpversions.Sanitize(headerValue)

	// The _meta member is decoded raw rather than via mcprequests.WireMeta:
	// telling "absent" from "present but malformed" needs the member's raw
	// bytes, and WireMeta's tolerant decode zeroes a mis-typed member, which
	// would silently read here as absent. Non-object params or _meta still
	// leave the map nil, matching ParseMeta's tolerance.
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}
	var metaRaw string
	if raw, ok := params.Meta[metaProtocolVersionMetaKey]; ok {
		if err := json.Unmarshal(raw, &metaRaw); err != nil {
			// Present but not a string: a malformed declaration. JSON null
			// decodes as a no-op and stays "absent".
			return unsupportedMetaProtocolVersionError(req, unparseableVersionPlaceholder)
		}
	}
	metaDeclared := strings.TrimSpace(metaRaw) != ""
	metaVersion := mcpversions.Sanitize(metaRaw)

	if headerDeclared && headerVersion == "" {
		return unsupportedMetaProtocolVersionError(req, unparseableVersionPlaceholder)
	}
	if metaDeclared && metaVersion == "" {
		return unsupportedMetaProtocolVersionError(req, unparseableVersionPlaceholder)
	}

	if headerDeclared && metaDeclared && headerVersion != metaVersion {
		return &oops.MCPError{
			ID:      req.ID,
			Code:    oops.MCPCodeInvalidRequest,
			Message: fmt.Sprintf("conflicting protocol version declarations: MCP-Protocol-Version header %q does not match the request _meta declaration %q", headerVersion, metaVersion),
		}
	}

	declared := conv.Default(headerVersion, metaVersion)
	if declared != "" && !mcpversions.Known(declared) {
		return unsupportedMetaProtocolVersionError(req, declared)
	}

	return nil
}

// unsupportedMetaProtocolVersionError is the structured error for a declared
// protocol version this surface does not serve. declared must be sanitized
// (or a placeholder) — it is echoed to the client.
func unsupportedMetaProtocolVersionError(req *rawRequest, declared string) *oops.MCPError {
	return &oops.MCPError{
		ID:      req.ID,
		Code:    oops.MCPCodeInvalidRequest,
		Message: fmt.Sprintf("unsupported protocol version %q; supported versions: %s", declared, strings.Join(mcpversions.All(), ", ")),
	}
}

func (s *Service) handleMetaInitialize(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	req *rawRequest,
) (json.RawMessage, error) {
	// Parsed purely for telemetry: this surface answers ServedMetaServer
	// unconditionally, and malformed params must not fail the handshake.
	params, _, err := parseInitializeParams(req.Params)
	if err != nil {
		logger.WarnContext(ctx, "failed to parse meta mcp initialize params", attr.SlogError(err))
	}

	recordMCPProtocolVersionSpan(ctx, params.ProtocolVersion, mcpversions.ServedMetaServer)
	s.metrics.RecordMCPInitialize(ctx, params.ProtocolVersion, mcpversions.ServedMetaServer)

	instructions, err := s.buildMetaServerInstructions(ctx, logger, mcpEndpoint, metaServer)
	if err != nil {
		return nil, err
	}

	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: mcpversions.ServedMetaServer,
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoMetaServer,
			Instructions: instructions,
		},
		serverIdentity: serverInfoMetaServer,
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
	}
	return bs, nil
}

// metaServerDiscoverResult is the response shape for MCP 2026-07-28's
// sessionless server/discover method: the same self-description initialize
// answers, minus any session establishment, plus the set of protocol
// revisions the surface can serve.
type metaServerDiscoverResult struct {
	ProtocolVersions []string                   `json:"protocolVersions"`
	Capabilities     map[string]json.RawMessage `json:"capabilities"`
	ServerInfo       serverInfo                 `json:"serverInfo"`
	Instructions     string                     `json:"instructions,omitempty"`
}

func (s *Service) handleMetaServerDiscover(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	req *rawRequest,
) (json.RawMessage, error) {
	instructions, err := s.buildMetaServerInstructions(ctx, logger, mcpEndpoint, metaServer)
	if err != nil {
		return nil, err
	}

	result := &result[metaServerDiscoverResult]{
		ID: req.ID,
		Result: metaServerDiscoverResult{
			ProtocolVersions: []string{mcpversions.ServedMetaServer},
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoMetaServer,
			Instructions: instructions,
		},
		serverIdentity: serverInfoMetaServer,
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize server/discover response").LogError(ctx, logger)
	}
	return bs, nil
}

func (s *Service) listMetaServerTools(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	tools := []*toolListEntry{
		{
			Name:        listServersToolName,
			Description: "List the member MCP servers this gateway fronts: which systems are reachable, their ordering, and their connection state. Start here to orient before drilling into a specific server.",
			InputSchema: metaListServersSchema,
			Annotations: nil,
			Meta:        nil,
		},
		{
			Name:        describeServerToolName,
			Description: "Describe one member server's tool catalog: qualified tool names and descriptions, without input schemas. Call describe_tools for the schemas of the specific tools you intend to use.",
			InputSchema: metaDescribeServerSchema,
			Annotations: nil,
			Meta:        nil,
		},
		{
			Name:        describeToolsToolName,
			Description: "Fetch full input schemas for named tools. Do not call a tool without first describing it to get its input schema.",
			InputSchema: metaDescribeToolsSchema,
			Annotations: nil,
			Meta:        nil,
		},
		{
			Name:        executeToolToolName,
			Description: "Execute a specific tool by qualified name (serverslug--toolname), passing arguments that match that tool's schema.",
			InputSchema: dynamicExecuteToolSchema,
			Annotations: nil,
			Meta:        nil,
		},
	}

	bs, err := json.Marshal(&result[toolsListResultTools]{
		ID:             req.ID,
		Result:         toolsListResultTools{Tools: tools},
		serverIdentity: serverInfoMetaServer,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").LogError(ctx, logger)
	}
	return bs, nil
}

func (s *Service) callMetaServerTool(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	req *rawRequest,
) (json.RawMessage, error) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").LogError(ctx, logger)
	}
	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").LogError(ctx, logger)
	}

	switch params.Name {
	case listServersToolName:
		return s.handleMetaListServersCall(ctx, logger, mcpEndpoint, metaServer, req)
	case describeServerToolName, describeToolsToolName, executeToolToolName:
		// Member tool catalogs and execution routing require the meta-server
		// runtime (AGE-3291). The tools are part of the fixed contract, so
		// they answer deterministically rather than as unknown tools.
		return nil, oops.E(oops.CodeNotImplemented, nil, "%s is not yet available on this endpoint", params.Name)
	default:
		return nil, oops.E(oops.CodeNotFound, nil, "unknown tool %q", params.Name).LogError(ctx, logger)
	}
}

// metaListedServer is one member entry in a list_servers result. Status is
// fixed at "unknown" until the meta-server runtime (AGE-3291) holds live
// member sessions to report on.
type metaListedServer struct {
	Slug      string `json:"slug"`
	Name      string `json:"name,omitempty"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

func (s *Service) handleMetaListServersCall(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	req *rawRequest,
) (json.RawMessage, error) {
	members, err := metamcprepo.New(s.db).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{
		MetaMcpServerID: metaServer.ID,
		ProjectID:       mcpEndpoint.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list meta mcp members").LogError(ctx, logger)
	}

	servers := make([]metaListedServer, 0, len(members))
	for _, member := range members {
		servers = append(servers, metaListedServer{
			Slug:      conv.PtrValOr(conv.FromPGText[string](member.McpServerSlug), ""),
			Name:      conv.PtrValOr(conv.FromPGText[string](member.McpServerName), ""),
			SortOrder: int(member.SortOrder),
			Status:    "unknown",
		})
	}

	structured, err := json.Marshal(map[string]any{"servers": servers})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize list_servers result").LogError(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		MimeType: nil,
		Text:     string(structured),
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize list_servers content").LogError(ctx, logger)
	}

	bs, err := json.Marshal(&result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: structured,
			IsError:           false,
		},
		serverIdentity: serverInfoMetaServer,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call response").LogError(ctx, logger)
	}
	return bs, nil
}

// buildMetaServerInstructions generates the server-instructions block from
// the meta server's member set: it names the fronted systems and states the
// drill-down order so a cold agent orients in one turn. Meta MCP servers
// carry no stored instructions; the block is always generated.
func (s *Service) buildMetaServerInstructions(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
) (string, error) {
	members, err := metamcprepo.New(s.db).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{
		MetaMcpServerID: metaServer.ID,
		ProjectID:       mcpEndpoint.ProjectID,
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "list meta mcp members for instructions").LogError(ctx, logger)
	}

	names := make([]string, 0, len(members))
	for _, member := range members {
		name := conv.PtrValOr(conv.FromPGText[string](member.McpServerSlug), "")
		if name == "" {
			name = conv.PtrValOr(conv.FromPGText[string](member.McpServerName), "")
		}
		if name != "" {
			names = append(names, name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s is a gateway fronting %d member MCP servers.", metaServer.Name, len(members))
	if len(names) > 0 {
		fmt.Fprintf(&b, " Members: %s.", strings.Join(names, ", "))
	}
	b.WriteString(" Discovery is a drill-down: call list_servers to see the member inventory, describe_server for one member's tool catalog, describe_tools for the full input schemas of named tools, then execute_tool to run one.")
	return b.String(), nil
}
