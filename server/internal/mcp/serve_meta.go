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
	"log/slog"
	"net/http"
	"slices"
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
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
)

// metaGateContext carries the per-request state the meta MCP tools need:
// what the issuer gate produced, the caller's identity/authentication
// outcome, and the surface-resolved protocol version. Assembled once in
// serveResolvedMetaMCPEndpoint and threaded through dispatch.
type metaGateContext struct {
	projectID uuid.UUID
	// metaServerID is a stored meta_mcp_servers row for an operator-built
	// gateway, and a synthetic per-agent id for an agent gateway — see
	// agentGatewayID. Treat it as a telemetry grouping key, never as
	// something to read a row back by.
	metaServerID uuid.UUID
	// agentID is set only when this request is an agent gateway, which
	// derives its members from the caller's delegated policy instead of
	// stored membership rows.
	agentID        uuid.UUID
	organizationID string
	tokens         map[uuid.UUID]remotesessions.UpstreamToken
	// userSessionIssuerID is the gated endpoint's issuer; uuid.Nil when ungated.
	userSessionIssuerID uuid.UUID
	// chainUpstream acquires a member's upstream token by identity chaining
	// when routing finds none. Set only on the tools/call dispatch copy, so
	// describe and consent probes never exchange; nil otherwise.
	chainUpstream   func(ctx context.Context, upstreamResource string) (string, error)
	toolSelection   *toolfilter.SessionSelection
	authenticated   bool
	sessionID       string
	chatID          string
	userID          string
	externalUserID  string
	apiKeyID        string
	protocolVersion mcpversions.Resolution
}

func shouldRecordMetaMCPNetworkRequest(agentID uuid.UUID) bool {
	// Agent gateways are synthetic and have no network access panel to feed.
	return agentID == uuid.Nil
}

// serveResolvedMetaMCPEndpoint terminates MCP for a meta-MCP-backed
// endpoint: it runs the issuer gate when the meta server is issuer-gated,
// then dispatches the JSON-RPC request. Only POST reaches here — GET/DELETE
// on /mcp/{slug} stay with their existing handlers, which treat meta-backed
// endpoints as having no proxied stream and no upstream session.
// agentID is the agent whose grants define membership, or uuid.Nil for a
// stored gateway whose members come from meta_mcp_server_members.
func (s *Service) serveResolvedMetaMCPEndpoint(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	agentID uuid.UUID,
) error {
	ctx := r.Context()

	if shouldRecordMetaMCPNetworkRequest(agentID) {
		s.recordMCPNetworkRequest(ctx, mcpEndpoint.ProjectID, uuid.Nil, metaServer.ID, metaServer.OrganizationID)
	}
	logger = logger.With(attr.SlogMetaMcpServerID(metaServer.ID.String()))

	supportedMeta := mcpversions.SupportedMetaServer()

	prepared := prepareMCPRequest(w, r, metamcp.MaxBodyBytes, supportedMeta)

	req := prepared.request
	resolution := prepared.protocolVersion
	if initializeNegotiable(&req, resolution) {
		// A conforming initialize declares nothing, so Resolve lands on the
		// default; the negotiated answer is what actually governs the
		// exchange (the write-back Resolution sanctions). Without a handshake
		// revision the provisional value stands and dispatch rejects the
		// method.
		params, _, _ := parseInitializeParams(req.Params)
		if negotiated, ok := mcpversions.Negotiate(params.ProtocolVersion, supportedMeta); ok {
			resolution.InEffect = negotiated
		}
	}
	// Both halves the error wrapper needs, published together: an error
	// escaping this handler has to echo the id the client sent and be encoded
	// on the revision governing the request. Publishing only one leaves the
	// wrapper answering with a modern wire code under a null id.
	if rpcCtx, ok := contextvalues.GetRPCContext(ctx); ok {
		rpcCtx.ProtocolVersion = resolution.InEffect
		if req.ID.IsSet() {
			rpcCtx.ID = req.ID
		}
	}

	if prepared.readyForProtocolVersionValidation() {
		validationErr := validateMetaDeclaredProtocolVersion(&req, r.Header.Get(mcpversions.HTTPHeader))
		if validationErr == nil {
			validationErr = validateRequestMetadata(r.Header, &req, resolution)
		}
		handled, err := s.handleProtocolVersionValidation(
			r,
			logger,
			w,
			&req,
			resolution,
			mcpmetrics.SurfaceMeta,
			validationErr,
		)
		if err != nil || handled {
			return err
		}
	}

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
	if prepared.empty() {
		return nil
	}
	if err := validateMCPRequestEnvelope(ctx, logger, prepared, oops.CodeRequestTooLarge, "meta mcp request body exceeds 1 MiB"); err != nil {
		return err
	}
	gate := &metaGateContext{
		projectID:    mcpEndpoint.ProjectID,
		metaServerID: metaServer.ID,
		// Nil for a stored gateway, whose members come from
		// meta_mcp_server_members rather than from a caller's grants.
		agentID:        agentID,
		organizationID: metaServer.OrganizationID,
		tokens:         gateTokens,
		// Uninitialized when ungated, which disables identity chaining.
		userSessionIssuerID: metaServer.UserSessionIssuerID.UUID,
		chainUpstream:       nil,
		toolSelection:       gateToolSelection,
		authenticated:       false,
		sessionID:           parseMcpSessionID(r.Header),
		chatID:              r.Header.Get("Gram-Chat-ID"),
		userID:              "",
		externalUserID:      "",
		apiKeyID:            "",
		// Member dispatch carries this InEffect verbatim; nothing on the
		// tools/call path reads it (upstream dials pin their own version).
		protocolVersion: resolution,
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
		//
		// An agent gateway has no endpoint project to own, and its key was
		// already authenticated against this organization before dispatch. It
		// is authenticated by construction; leaving it false would list a
		// granted private member and then fail to drill into it.
		if agentID != uuid.Nil {
			gate.authenticated = authCtx.ActiveOrganizationID == metaServer.OrganizationID
		} else if authCtx.ActiveOrganizationID != "" {
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

	// Hand back the session the handshake was recorded under, as the hosted
	// surface does. Without it a client that sent no Mcp-Session-Id gets a
	// freshly minted id on every request, and nothing it does later can be
	// tied back to the identity it reported at initialize.
	if initializeNegotiable(&req, resolution) {
		w.Header().Set("Mcp-Session-Id", gate.sessionID)
	}

	body, err := s.handleMetaMCPRequest(ctx, logger, mcpEndpoint, metaServer, gate, &req, r.Header.Get(mcpversions.HTTPHeader))

	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		return writeMCPError(ctx, logger, w, req.ID, resolution.InEffect, err)
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

	if !methodAvailable(req, gate.protocolVersion, mcpversions.SupportedMetaServer()) {
		return nil, unavailableMethod(req)
	}

	switch req.Method {
	case mcpversions.MethodPing:
		return handlePing(ctx, logger, req.ID, serverInfoMetaServer)
	case mcpversions.MethodInitialize:
		return s.handleMetaInitialize(ctx, logger, metaServer, gate, req, gate.protocolVersion.InEffect)
	case mcpversions.MethodServerDiscover:
		return handleServerDiscover(ctx, logger, req.ID, describeMetaServer(metaServer), mcpversions.SupportedMetaServer())
	case mcpversions.MethodNotificationsInitialized, mcpversions.MethodNotificationsCancelled:
		return nil, nil
	case mcpversions.MethodToolsList:
		return s.listMetaServerTools(ctx, logger, req)
	case mcpversions.MethodToolsCall:
		return s.callMetaServerTool(ctx, logger, mcpEndpoint, metaServer, gate, req)
	default:
		return nil, unavailableMethod(req)
	}
}

// validateMetaDeclaredProtocolVersion enforces MCP 2026-07-28's per-request
// version declaration on the meta surface. A declaration may arrive in the
// MCP-Protocol-Version header, the params-level
// io.modelcontextprotocol/protocolVersion _meta member, or both; conflicting,
// malformed, or unserved declarations — anything outside the served set,
// matching what server/discover advertises — produce deterministic
// structured errors. Only a genuinely absent declaration is accepted, for
// backward compatibility with handshake-based clients per the
// specification's versioning rules — a declaration that is present but
// unsanitizable (or not a string at all) is a malformed value, not an
// absent one.
//
// A malformed declaration follows the MCP 2026-07-28 rules for its carrier:
// HeaderMismatch (-32020) for a header with invalid characters,
// InvalidParams (-32602) for a malformed _meta member. The response follows
// the other declaration when it names a recognized revision, and
// [mcpversions.Latest] otherwise.
func validateMetaDeclaredProtocolVersion(req *rawRequest, headerValue string) error {
	if initializeNegotiable(req, mcpversions.Resolve(headerValue, mcpversions.SupportedMetaServer())) {
		return nil
	}

	headerDeclared := strings.TrimSpace(headerValue) != ""
	headerVersion := mcpversions.Sanitize(headerValue)

	// The _meta member is decoded raw rather than via mcprequests.WireMeta:
	// telling "absent" from "present but malformed" needs the member's raw
	// bytes, and WireMeta's tolerant decode zeroes a mis-typed member, which
	// would silently read here as absent. Non-object params or _meta still
	// leave the members nil, matching ParseMeta's tolerance.
	meta, _ := rawRequestMeta(req.Params)
	var metaRaw string
	metaMalformed := false
	if raw, ok := meta[metaProtocolVersionKey]; ok {
		// Present but not a string is malformed. JSON null decodes as a
		// no-op and stays "absent".
		metaMalformed = json.Unmarshal(raw, &metaRaw) != nil
	}
	metaDeclared := strings.TrimSpace(metaRaw) != ""
	metaVersion := mcpversions.Sanitize(metaRaw)
	metaMalformed = metaMalformed || (metaDeclared && metaVersion == "")

	if headerDeclared && headerVersion == "" {
		return &declarationError{
			revision: declarationRevision(metaVersion),
			err:      headerMismatchError(req.ID, fmt.Sprintf("%s header is malformed", mcpversions.HTTPHeader)),
		}
	}
	if metaMalformed {
		return &declarationError{
			revision: declarationRevision(headerVersion),
			err:      invalidMetaError(req.ID, fmt.Sprintf("_meta member %q must be a protocol version string", metaProtocolVersionKey)),
		}
	}

	if headerDeclared && metaDeclared && headerVersion != metaVersion {
		return conflictingProtocolVersionError(req.ID, headerVersion, metaVersion)
	}

	declared := conv.Default(headerVersion, metaVersion)
	if declared != "" && !slices.Contains(mcpversions.SupportedMetaServer(), declared) {
		return unsupportedProtocolVersionError(req.ID, declared, mcpversions.SupportedMetaServer())
	}

	return nil
}

func (s *Service) handleMetaInitialize(
	ctx context.Context,
	logger *slog.Logger,
	metaServer *metamcprepo.MetaMcpServer,
	gate *metaGateContext,
	req *rawRequest,
	negotiated string,
) (json.RawMessage, error) {
	// Parsed purely for telemetry — negotiation already ran at gate
	// construction — and malformed params must not fail the handshake.
	params, _, err := parseInitializeParams(req.Params)
	if err != nil {
		logger.WarnContext(ctx, "failed to parse meta mcp initialize params", attr.SlogError(err))
	}

	// Record who handshaked so every member dispatch in this session can
	// attribute its tool calls to a client. Scoped to the gateway, not to a
	// toolset: members each carry their own slug.
	storeSessionClientInfo(ctx, logger, s.sessionClientInfo, &mcpInputs{ //nolint:exhaustruct // only the record's identity fields matter here
		projectID:       gate.projectID,
		sessionID:       gate.sessionID,
		clientInfoScope: metaClientInfoScope(gate.metaServerID),
	}, params.ClientInfo.Name, params.ClientInfo.Version, params.ProtocolVersion)

	recordMCPProtocolVersionSpan(ctx, params.ProtocolVersion, negotiated)
	s.metrics.RecordMCPInitialize(ctx, params.ProtocolVersion, negotiated)

	result := &result[initializeResult]{
		ID:             req.ID,
		Result:         describeMetaServer(metaServer).initializeResult(negotiated),
		serverIdentity: serverInfoMetaServer,
		cacheHints:     nil,
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
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
		// The gateway tool contract is fixed and consults neither the endpoint
		// nor the meta server, so every caller receives the same four tools.
		cacheHints: cacheHintsCallerUniform,
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

	start := time.Now()

	// One snapshot per request: every meta MCP tool answers from the same
	// member set, so a membership mutation lands between requests, never
	// inside one. An agent gateway has no stored membership — its members are
	// whatever its delegated policy still reaches at this instant — so a
	// revoked grant lands between requests the same way.
	var (
		members []metaMember
		err     error
	)
	if gate.agentID != uuid.Nil {
		ctx, members, err = s.resolveAgentMemberSnapshot(ctx, logger, gate.organizationID)
	} else {
		ctx, members, err = s.resolveMetaMemberSnapshot(ctx, logger, metaServer.ID, mcpEndpoint.ProjectID)
	}
	if err != nil {
		if params.Name != metamcp.ToolExecuteTool {
			s.logMetaDiscovery(ctx, gate, params.Name, start, err)
		}
		return nil, err
	}

	var body json.RawMessage
	switch params.Name {
	case metamcp.ToolListServers:
		body, err = s.handleMetaListServersCall(ctx, logger, members, req)
	case metamcp.ToolDescribeServer:
		body, err = s.handleMetaDescribeServerCall(ctx, logger, gate, members, req, params.Arguments)
	case metamcp.ToolDescribeTools:
		body, err = s.handleMetaDescribeToolsCall(ctx, logger, gate, members, req, params.Arguments)
	default:
		// execute_tool is deliberately not logged here: the member dispatch
		// writes the single tool_call row, stamped with this gateway's id.
		return s.handleMetaExecuteToolCall(ctx, logger, gate, members, req, params.Arguments, params.Meta)
	}
	s.logMetaDiscovery(ctx, gate, params.Name, start, err)
	return body, err
}

// logMetaDiscovery writes one meta_discovery telemetry row for a gateway
// discovery call. Failures record their oops status code.
func (s *Service) logMetaDiscovery(ctx context.Context, gate *metaGateContext, toolName string, start time.Time, handlerErr error) {
	logAttrs := tm.HTTPLogAttributes{
		attr.EventSourceKey:     string(tm.EventSourceMetaDiscovery),
		attr.MetaMcpServerIDKey: gate.metaServerID.String(),
	}
	logAttrs.RecordDuration(time.Since(start).Seconds())
	statusCode := http.StatusOK
	if handlerErr != nil {
		statusCode = http.StatusInternalServerError
		if oopsErr, ok := errors.AsType[*oops.ShareableError](handlerErr); ok {
			statusCode = oopsErr.HTTPStatus(ctx)
		}
	}
	logAttrs.RecordStatusCode(statusCode)
	logAttrs.RecordTraceContext(ctx)
	logAttrs.RecordAuthenticatedActor(ctx)
	if gate.chatID != "" {
		logAttrs[attr.GenAIConversationIDKey] = gate.chatID
	}
	if gate.externalUserID != "" {
		logAttrs[attr.ExternalUserIDKey] = gate.externalUserID
	}
	if gate.apiKeyID != "" {
		logAttrs[attr.APIKeyIDKey] = gate.apiKeyID
	}
	s.telemLogger.Log(ctx, tm.LogParams{
		Timestamp: time.Now(),
		ToolInfo: tm.ToolInfo{
			ID: gate.metaServerID.String(),
			// Not "tools:"-prefixed: that prefix is the query layer's tool-call classifier.
			URN:            "metamcp:" + gate.metaServerID.String() + ":" + toolName,
			Name:           toolName,
			ProjectID:      gate.projectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: gate.organizationID,
		},
		UserInfo:   tm.UserInfoByID(gate.userID),
		Attributes: logAttrs,
	})
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
