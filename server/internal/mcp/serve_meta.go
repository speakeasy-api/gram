// The meta-server MCP surface: protocol termination for meta-MCP-backed
// /mcp/{slug} endpoints. This surface answers MCP 2026-07-28 — including the
// sessionless server/discover method and per-request protocol-version
// declarations — and exposes the fixed meta MCP tool contract (list_servers,
// describe_server, describe_tools, execute_tool). Hosted (toolset-backed)
// members serve the full drill-down through the in-process tool dispatch;
// proxied (remote/tunneled) members through their own upstream sessions.

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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// metaGateContext carries the per-request state the meta MCP tools need:
// what the issuer gate produced, the caller's identity/authentication
// outcome, and the surface-resolved protocol version. Assembled once in
// serveResolvedMetaMCPEndpoint and threaded through dispatch.
type metaGateContext struct {
	projectID       uuid.UUID
	organizationID  string
	tokens          map[uuid.UUID]remotesessions.UpstreamToken
	toolSelection   *toolfilter.SessionSelection
	authenticated   bool
	sessionID       string
	chatID          string
	userID          string
	externalUserID  string
	apiKeyID        string
	protocolVersion mcpversions.Resolution
}

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

	var gateTokens map[uuid.UUID]remotesessions.UpstreamToken
	var gateToolSelection *toolfilter.SessionSelection
	if metaServer.UserSessionIssuerID.Valid {
		resolvedEndpoint, err := s.BuildResolvedMcpEndpointForMetaServer(ctx, logger, mcpEndpoint, metaServer, "mcp")
		if err != nil {
			return err
		}
		newCtx, tokens, toolSelection, err := s.ApplyIssuerGate(ctx, w, httpheaders.AuthorizationBearerToken(r), s.BaseURLForRequest(r), resolvedEndpoint)
		if err != nil {
			return fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		r = r.WithContext(ctx)
		gateTokens = tokens
		gateToolSelection = toolSelection
	}

	r.Body = http.MaxBytesReader(w, r.Body, metamcp.MaxBodyBytes)

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

	gate := &metaGateContext{
		projectID:      mcpEndpoint.ProjectID,
		organizationID: metaServer.OrganizationID,
		tokens:         gateTokens,
		toolSelection:  gateToolSelection,
		authenticated:  false,
		sessionID:      parseMcpSessionID(r.Header),
		chatID:         r.Header.Get("Gram-Chat-ID"),
		userID:         "",
		externalUserID: "",
		apiKeyID:       "",
		// The meta surface serves one revision; InEffect is fixed while
		// Declared keeps what the request actually said, for telemetry.
		// Member dispatch carries this InEffect verbatim even though the
		// hosted set excludes it; nothing on the tools/call path reads it.
		protocolVersion: mcpversions.Resolution{
			Declared: mcprequests.DeclaredProtocolVersion(r.Header.Get(mcpversions.HTTPHeader), req.Params),
			InEffect: mcpversions.ServedMetaServer,
		},
	}
	// Identity comes from the issuer gate alone: this surface runs no
	// identity-auth ladder, so ungated meta endpoints serve anonymously —
	// private-toolset members stay invisible and gram environments never
	// load, regardless of Authorization header.
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		gate.userID = authCtx.UserID
		gate.externalUserID = authCtx.ExternalUserID
		gate.apiKeyID = authCtx.APIKeyID
		// authenticated = the caller's org owns the endpoint's project,
		// unlocking gram environments for hosted-member execution.
		if authCtx.ActiveOrganizationID != "" {
			projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeUnexpected, err, "error checking project access").LogError(ctx, logger)
			}
			for _, project := range projects {
				if project.ID == mcpEndpoint.ProjectID {
					gate.authenticated = true
					break
				}
			}
		}
	}

	body, err := s.handleMetaMCPRequest(ctx, logger, mcpEndpoint, metaServer, gate, &req, r.Header.Get(mcpversions.HTTPHeader))

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
	gate *metaGateContext,
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
		if !req.ID.IsSet() {
			// JSON-RPC 2.0 forbids responding to notifications, even with an
			// error: a notification carrying a bad declaration is dropped.
			return nil, nil
		}
		return nil, err
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, logger, req.ID, serverInfoMetaServer)
	case "initialize":
		return s.handleMetaInitialize(ctx, logger, req)
	case "server/discover":
		return s.handleMetaServerDiscover(ctx, logger, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return s.listMetaServerTools(ctx, logger, req)
	case "tools/call":
		return s.callMetaServerTool(ctx, logger, mcpEndpoint, metaServer, gate, req)
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
// unparseable, or unserved declarations — anything but the served revision,
// including older revisions this package recognizes, matching the set
// server/discover advertises — produce deterministic structured errors
// naming the served set. Only a genuinely absent declaration is
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
	if declared != "" && declared != mcpversions.ServedMetaServer {
		return unsupportedMetaProtocolVersionError(req, declared)
	}

	return nil
}

// unsupportedMetaProtocolVersionError is the structured error for a declared
// protocol version this surface does not serve. The named set is the served
// set — exactly [mcpversions.ServedMetaServer], matching what server/discover
// advertises — not the wider set of recognized revisions. declared must be
// sanitized (or a placeholder) — it is echoed to the client.
func unsupportedMetaProtocolVersionError(req *rawRequest, declared string) *oops.MCPError {
	return &oops.MCPError{
		ID:      req.ID,
		Code:    oops.MCPCodeInvalidRequest,
		Message: fmt.Sprintf("unsupported protocol version %q; supported versions: %s", declared, mcpversions.ServedMetaServer),
	}
}

func (s *Service) handleMetaInitialize(
	ctx context.Context,
	logger *slog.Logger,
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

	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: mcpversions.ServedMetaServer,
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoMetaServer,
			Instructions: metamcp.Instructions,
		},
		serverIdentity: serverInfoMetaServer,
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
	}
	return bs, nil
}

func (s *Service) handleMetaServerDiscover(
	ctx context.Context,
	logger *slog.Logger,
	req *rawRequest,
) (json.RawMessage, error) {
	result := &result[metamcp.DiscoverResult]{
		ID: req.ID,
		Result: metamcp.DiscoverResult{
			ProtocolVersions: []string{mcpversions.ServedMetaServer},
			Capabilities: map[string]json.RawMessage{
				"tools": json.RawMessage("{}"),
			},
			ServerInfo:   serverInfoMetaServer,
			Instructions: metamcp.Instructions,
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
	contract := metamcp.Tools(dynamicExecuteToolSchema)
	tools := make([]*toolListEntry, 0, len(contract))
	for _, tool := range contract {
		tools = append(tools, &toolListEntry{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Annotations: nil,
			Meta:        nil,
		})
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
	gate *metaGateContext,
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
	case metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools, metamcp.ToolExecuteTool:
	default:
		return nil, oops.E(oops.CodeNotFound, nil, "unknown tool %q", params.Name).LogError(ctx, logger)
	}

	// One snapshot per request: every meta MCP tool answers from the same
	// member set, so a membership mutation lands between requests, never
	// inside one.
	ctx, members, err := s.resolveMetaMemberSnapshot(ctx, logger, metaServer.ID, mcpEndpoint.ProjectID)
	if err != nil {
		return nil, err
	}

	switch params.Name {
	case metamcp.ToolListServers:
		return s.handleMetaListServersCall(ctx, logger, members, req)
	case metamcp.ToolDescribeServer:
		return s.handleMetaDescribeServerCall(ctx, logger, gate, members, req, params.Arguments)
	case metamcp.ToolDescribeTools:
		return s.handleMetaDescribeToolsCall(ctx, logger, gate, members, req, params.Arguments)
	default:
		return s.handleMetaExecuteToolCall(ctx, logger, gate, members, req, params.Arguments, params.Meta)
	}
}

func (s *Service) handleMetaListServersCall(
	ctx context.Context,
	logger *slog.Logger,
	members []metaMember,
	req *rawRequest,
) (json.RawMessage, error) {
	servers := make([]metamcp.ListedServer, 0, len(members))
	for _, member := range members {
		servers = append(servers, metamcp.ListedServer{
			Slug:      member.slug,
			Name:      member.name,
			SortOrder: int(member.sortOrder),
			Status:    s.memberStatus(ctx, member),
		})
	}

	structured, err := json.Marshal(metamcp.ListServersResult{Servers: servers})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize list_servers result").LogError(ctx, logger)
	}
	return marshalMetaToolCallResult(ctx, logger, req.ID, structured)
}
