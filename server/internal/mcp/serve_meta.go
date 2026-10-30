// The meta-server MCP surface: protocol termination for meta-MCP-backed
// /mcp/{slug} endpoints. This surface answers MCP 2026-07-28 — including the
// sessionless server/discover method and per-request protocol-version
// declarations. Discovery exposes direct tools, progressive drill-down tools,
// or Code Mode's execute tool. The connection_status MCP Apps resource
// accompanies progressive discovery. Hosted (toolset-backed)
// members serve the full drill-down through the in-process tool dispatch;
// proxied (remote/tunneled) members through their own upstream sessions.

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
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
	agentID uuid.UUID
	// organizationGateway is set only when this request is an organization
	// gateway, which derives its members from the signed-in user's access
	// across the organization instead of stored membership rows.
	organizationGateway bool
	organizationID      string
	// tokens is the upstream token map every member routes from, unless
	// projectTokens is set.
	tokens map[uuid.UUID]remotesessions.UpstreamToken
	// projectTokens resolves upstream tokens per member project instead of
	// tokens, for an organization gateway whose members span projects. Read
	// both through memberTokens.
	projectTokens *projectTokenCache
	// memberChoices, when serving reads credential choices, routes each
	// proxied member through the client the credential chose for it, in
	// place of tokens and projectTokens.
	memberChoices *memberChoices
	// userSessionIssuerID is the gated endpoint's issuer; uuid.Nil when ungated.
	userSessionIssuerID uuid.UUID
	// connectRoot is the gateway's root URL, under which each member has a
	// connect page a signed-in person opens to connect it. Empty when nobody
	// signs in to the gateway in a browser: ungated and agent gateways.
	connectRoot string
	// chainUpstream acquires a member credential at consent or live dispatch.
	// Cached discovery leaves it unset.
	chainUpstream func(ctx context.Context, member metaMember, upstreamResource string) (string, error)
	// catalog is the immutable consent record; nil for connections without materialized approval.
	catalog *connectionCatalog
	// consentDiagnostics is shared only within one Code Mode execution.
	consentDiagnostics *consentDiagnostics
	// catalogRef is the authenticated consent reference and discovery mode.
	catalogRef *toolfilter.CatalogReference
	// authentication retains validated endpoint identity for lazy catalogue loading.
	authentication *issuerGateAuthentication
	// unapprovedMembers are current members the catalog did not approve. They
	// are reported by connection_status only, never discovered or executed.
	unapprovedMembers []metaMember
	// hiddenMembers are members the caller hid, reported but never served.
	hiddenMembers []metaMember
	// execute permits live credential resolution for a cached tool call.
	execute         bool
	discoveryMode   metamcp.DiscoveryMode
	authenticated   bool
	sessionID       string
	chatID          string
	userID          string
	externalUserID  string
	apiKeyID        string
	protocolVersion mcpversions.Resolution
	// connectURL is the gateway's browser connect page; empty when ungated.
	connectURL string
}

// metaMembership names where a meta request's members come from. The zero
// value is a stored gateway, whose members are its meta_mcp_server_members
// rows.
type metaMembership struct {
	// agentID is set for an agent gateway: its members are what the agent's
	// delegated policy reaches.
	agentID uuid.UUID

	// organization is set for an organization gateway: its members are what
	// the signed-in user can reach across the organization.
	organization bool
}

// derived reports whether members come from the caller's access rather than
// from stored membership rows.
func (m metaMembership) derived() bool {
	return m.agentID != uuid.Nil || m.organization
}

// serveResolvedMetaMCPEndpoint terminates MCP for a meta-MCP-backed
// endpoint: it runs the issuer gate when the meta server is issuer-gated,
// then dispatches the JSON-RPC request. Only POST reaches here — GET/DELETE
// on /mcp/{slug} stay with their existing handlers, which treat meta-backed
// endpoints as having no proxied stream and no upstream session.
// membership says where the members come from. resolvedEndpoint is the
// issuer-gate endpoint for a gateway that has no mcp_endpoints row behind it,
// and nil for one that does.
func (s *Service) serveResolvedMetaMCPEndpoint(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	membership metaMembership,
	resolvedEndpoint *ResolvedMcpEndpoint,
) error {
	ctx := r.Context()

	// Derived gateways are synthetic and have no network access panel to feed.
	if !membership.derived() {
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

	admission := metaGateAdmission{bearer: httpheaders.AuthorizationBearerToken(r), baseURL: s.BaseURLForRequest(r), sessionID: parseMcpSessionID(r.Header), clientSessionID: r.Header.Get("Mcp-Session-Id"), chatID: r.Header.Get("Gram-Chat-ID"), resolution: resolution}
	ctx, gate, err := s.admitMetaGate(ctx, w, logger, mcpEndpoint, metaServer, membership, resolvedEndpoint, admission)
	if err != nil {
		return err
	}
	r = r.WithContext(ctx)
	if prepared.empty() {
		return nil
	}
	if err := validateMCPRequestEnvelope(ctx, logger, prepared, oops.CodeRequestTooLarge, "meta mcp request body exceeds 1 MiB"); err != nil {
		return err
	}
	if gate.catalogRef != nil && (req.Method == mcpversions.MethodToolsCall || (req.Method == mcpversions.MethodToolsList && (gate.discoveryMode == metamcp.DiscoveryModeDirect || gate.discoveryMode == metamcp.DiscoveryModeCode))) {
		authentication := gate.authentication
		catalog, err := s.loadConnectionCatalog(ctx, authentication.endpoint, authentication.subject, gate.catalogRef, time.Now())
		if errors.Is(err, errConnectionCatalogExpired) {
			return WriteAuthenticateChallenge(w, authentication.protectedResourceURL, err.Error())
		}
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "could not load approved tools; retry shortly").LogError(ctx, logger)
		}
		gate.catalog = catalog
	}
	if !gate.discoveryMode.Valid() {
		return oops.E(oops.CodeInvalid, nil, "unsupported gateway discovery mode")
	}
	if gate.discoveryMode == metamcp.DiscoveryModeCode {
		endpoint := *mcpEndpoint
		refresh := metaCodeGateRefresher(func(callbackCtx context.Context, current *metamcprepo.MetaMcpServer) (context.Context, *metaGateContext, error) {
			return s.admitMetaGate(callbackCtx, &metaCodeSilentResponse{header: make(http.Header)}, logger, &endpoint, current, metaMembership{agentID: uuid.Nil, organization: false}, nil, admission)
		})
		if membership.organization {
			// The organization gateway has no stored rows to re-read: reloading
			// it re-checks the organization and its gateway's availability.
			organizationSlug, admittedIssuer := mcpEndpoint.Slug, metaServer.UserSessionIssuerID
			refresh = func(callbackCtx context.Context, _ *metamcprepo.MetaMcpServer) (context.Context, *metaGateContext, error) {
				fresh, err := s.loadOrganizationGateway(callbackCtx, organizationSlug)
				if err != nil {
					return callbackCtx, nil, err
				}
				if fresh.metaServer.UserSessionIssuerID != admittedIssuer {
					return callbackCtx, nil, nil
				}
				return s.admitMetaGate(callbackCtx, &metaCodeSilentResponse{header: make(http.Header)}, logger, fresh.endpoint, fresh.metaServer, membership, fresh.resolved, admission)
			}
		}
		ctx = context.WithValue(ctx, metaCodeRefreshKey{}, refresh)
		ctx = context.WithValue(ctx, liveSessionPolicyKey{}, true)
		// admitMetaGate serves Code Mode only to callers the issuer gate
		// signed in, so the credential identifies a verified caller.
		credential := sha256.Sum256([]byte(admission.bearer))
		ctx = context.WithValue(ctx, metaCodeCredentialKey{}, hex.EncodeToString(credential[:]))
		sessionScope := "credential"
		if admission.clientSessionID != "" {
			sessionScope = "session:" + admission.clientSessionID
		}
		ctx = context.WithValue(ctx, metaCodeSessionKey{}, sessionScope)
	}

	// Hand back the session the handshake was recorded under, as the hosted
	// surface does. Without it a client that sent no Mcp-Session-Id gets a
	// freshly minted id on every request, and nothing it does later can be
	// tied back to the identity it reported at initialize.
	if initializeNegotiable(&req, resolution) {
		w.Header().Set("Mcp-Session-Id", gate.sessionID)
	}

	// Every member and toolset check this request shadows shares one
	// admission of its caller.
	ctx = withMandatoryAdmission(ctx)
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
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, "", time.Since(start))
		}()
	}

	if !methodAvailable(req, gate.protocolVersion, mcpversions.SupportedMetaServer(), nil) {
		return nil, unavailableMethod(req)
	}

	switch req.Method {
	case mcpversions.MethodPing:
		return handlePing(ctx, logger, req.ID, serverInfoMetaServer)
	case mcpversions.MethodInitialize:
		return s.handleMetaInitialize(ctx, logger, metaServer, gate, req, gate.protocolVersion.InEffect)
	case mcpversions.MethodServerDiscover:
		return handleServerDiscover(ctx, logger, req.ID, describeMetaServer(metaServer, gate.discoveryMode), mcpversions.SupportedMetaServer())
	case mcpversions.MethodNotificationsCancelled:
		if gate.discoveryMode == metamcp.DiscoveryModeCode {
			return nil, s.cancelMetaCode(ctx, gate, req)
		}
		return nil, nil
	case mcpversions.MethodNotificationsInitialized:
		return nil, nil
	case mcpversions.MethodToolsList:
		if gate.discoveryMode == metamcp.DiscoveryModeCode {
			return s.listMetaCodeTools(ctx, logger, metaServer, gate, req)
		}
		if gate.discoveryMode == metamcp.DiscoveryModeDirect {
			return s.listDirectGatewayTools(ctx, logger, mcpEndpoint, metaServer, gate, req)
		}
		return s.listMetaServerTools(ctx, logger, req)
	case mcpversions.MethodToolsCall:
		return s.callMetaServerTool(ctx, logger, mcpEndpoint, metaServer, gate, req)
	case mcpversions.MethodResourcesList:
		return handleMetaResourcesList(ctx, logger, req)
	case mcpversions.MethodResourcesRead:
		return handleMetaResourcesRead(ctx, logger, req)
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
		Result:         describeMetaServer(metaServer, gate.discoveryMode).initializeResult(negotiated),
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
			Title: "", OutputSchema: nil, Icons: nil, Execution: nil,
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Annotations: nil,
			Meta:        nil,
		})
		// Only inspection tools are read-only; execute_tool can dispatch writes.
		switch tool.Name {
		case metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools, metamcp.ToolConnectionStatus:
			tools[len(tools)-1].Annotations = &externalmcp.ToolAnnotations{
				Title:           "",
				ReadOnlyHint:    new(true),
				DestructiveHint: nil,
				IdempotentHint:  nil,
				OpenWorldHint:   nil,
			}
		}
		if tool.Name == metamcp.ToolConnectionStatus {
			tools[len(tools)-1].Meta = map[string]any{
				"ui":             map[string]any{"resourceUri": metamcp.ConnectionStatusResourceURI},
				"ui/resourceUri": metamcp.ConnectionStatusResourceURI,
			}
		}
	}

	bs, err := json.Marshal(&result[toolsListResultTools]{
		ID:             req.ID,
		Result:         toolsListResultTools{Tools: tools},
		serverIdentity: serverInfoMetaServer,
		// The gateway tool contract is fixed and consults neither the endpoint
		// nor the meta server, so every caller receives the same tools.
		cacheHints: cacheHintsCallerUniform,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").LogError(ctx, logger)
	}
	return bs, nil
}

// progressiveGatewayTools are the fixed tools Progressive discovery serves.
var progressiveGatewayTools = []string{metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools, metamcp.ToolExecuteTool, metamcp.ToolConnectionStatus}

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

	// A name another mode serves usually comes from a tools/list the client
	// cached before this connection's mode changed, so the refusal says to
	// reconnect. Direct serves only server-qualified member tools.
	var served bool
	switch gate.discoveryMode {
	case metamcp.DiscoveryModeCode:
		served = params.Name == metamcp.ToolCodeExecute
	case metamcp.DiscoveryModeDirect:
		served = params.Name != metamcp.ToolCodeExecute && !slices.Contains(progressiveGatewayTools, params.Name)
	default:
		served = slices.Contains(progressiveGatewayTools, params.Name)
	}
	if !served {
		return nil, oops.E(oops.CodeNotFound, nil, "tool %q is not available on this connection; the gateway's tools may have changed, so reconnect to refresh the tool list", params.Name).LogWarn(ctx, logger)
	}
	gate.execute = gate.discoveryMode == metamcp.DiscoveryModeDirect || params.Name == metamcp.ToolExecuteTool
	start := time.Now()

	// One snapshot per request: every meta MCP tool answers from the same
	// member set, so a membership mutation lands between requests, never
	// inside one. A derived gateway has no stored membership — its members
	// are whatever the caller's access still reaches at this instant — so a
	// revoked grant lands between requests the same way.
	ctx, members, err := s.resolveGateMemberSnapshot(ctx, logger, gate, metaServer.ID, mcpEndpoint.ProjectID)
	if err != nil {
		if params.Name != metamcp.ToolExecuteTool {
			s.logMetaDiscovery(ctx, gate, params.Name, start, err)
		}
		return nil, err
	}

	if gate.discoveryMode == metamcp.DiscoveryModeDirect {
		return s.executeMetaMemberTool(ctx, logger, gate, members, req, params.Name, params.Arguments, params.Meta)
	}
	if gate.discoveryMode == metamcp.DiscoveryModeCode {
		return s.executeMetaCode(ctx, logger, mcpEndpoint, metaServer, gate, members, req, params)
	}
	var body json.RawMessage
	switch params.Name {
	case metamcp.ToolListServers:
		body, err = s.handleMetaListServersCall(ctx, logger, gate, catalogSelectedMembers(gate, members), req)
	case metamcp.ToolDescribeServer:
		body, err = s.handleMetaDescribeServerCall(ctx, logger, gate, catalogSelectedMembers(gate, members), req, params.Arguments)
	case metamcp.ToolDescribeTools:
		body, err = s.handleMetaDescribeToolsCall(ctx, logger, gate, members, req, params.Arguments)
	case metamcp.ToolConnectionStatus:
		body, err = s.handleMetaConnectionStatusCall(ctx, logger, gate, members, req)
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

// catalogSelectedMembers keeps the members with at least one tool a Specific
// tools consent approved, so Progressive discovery and Code Mode name the same
// servers that Direct listing exposes.
func catalogSelectedMembers(gate *metaGateContext, members []metaMember) []metaMember {
	if gate.catalog == nil || gate.catalogRef.AllTools {
		return members
	}
	selected := make(map[string]bool)
	for _, tool := range gate.catalog.Tools {
		if slug, _, err := metamcp.SplitQualifiedName(tool.Name); err == nil {
			selected[slug] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(members), func(member metaMember) bool { return !selected[member.slug] })
}

func (s *Service) handleMetaListServersCall(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
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

	result := metamcp.ListServersResult{Servers: servers, Hidden: nil, ConnectURL: "", Note: ""}
	for _, member := range catalogSelectedMembers(gate, gate.hiddenMembers) {
		result.Hidden = append(result.Hidden, metamcp.HiddenServer{Slug: member.slug, Name: member.name})
	}
	notes := make([]string, 0, 2)
	if len(result.Hidden) > 0 && gate.connectURL != "" {
		result.ConnectURL = gate.connectURL
		notes = append(notes, metamcp.HiddenNote)
	}
	if slices.ContainsFunc(servers, func(server metamcp.ListedServer) bool { return server.Status == metamcp.StatusUnknown }) {
		notes = append(notes, metamcp.UnknownNote)
	}
	result.Note = strings.Join(notes, " ")
	structured, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize list_servers result").LogError(ctx, logger)
	}
	return marshalMetaToolCallResult(ctx, logger, req.ID, structured)
}

// metaGateAdmission retains only the original credentials and transport identity.
// The same admission code runs at the HTTP boundary and before every code callback.
type metaGateAdmission struct {
	bearer          string
	baseURL         string
	sessionID       string
	clientSessionID string
	chatID          string
	resolution      mcpversions.Resolution
}

// membership says where the members come from. resolvedEndpoint is the
// issuer-gate endpoint for a gateway that has no mcp_endpoints row behind it,
// and nil for one that does.
func (s *Service) admitMetaGate(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, mcpEndpoint *mcpendpointsrepo.McpEndpoint, metaServer *metamcprepo.MetaMcpServer, membership metaMembership, resolvedEndpoint *ResolvedMcpEndpoint, request metaGateAdmission) (context.Context, *metaGateContext, error) {
	var gateTokens map[uuid.UUID]remotesessions.UpstreamToken
	var gateProjectTokens *projectTokenCache
	var gateMemberChoices *memberChoices
	agentKeyChoicesRead := false
	var gatePolicy *toolfilter.SessionPolicy
	var gateAuthentication *issuerGateAuthentication
	switch {
	case !metaServer.UserSessionIssuerID.Valid:
	case membership.organization:
		// Members span projects, so credentials resolve per member project
		// when a member first needs them rather than once for the gateway.
		newCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, request.bearer, request.baseURL, resolvedEndpoint)
		if err != nil {
			return ctx, nil, fmt.Errorf("authenticate organization gateway: %w", err)
		}
		ctx = newCtx
		gateProjectTokens = s.newOrganizationGatewayTokens(authentication.endpoint, authentication.subject)
		gateMemberChoices, err = s.newMemberChoices(ctx, authentication)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "load upstream client choices").LogError(ctx, logger)
		}
		gatePolicy = toolSelection
		gateAuthentication = authentication
	default:
		if resolvedEndpoint == nil {
			built, err := s.BuildResolvedMcpEndpointForMetaServer(ctx, logger, mcpEndpoint, metaServer, mcproute.MCP)
			if err != nil {
				return ctx, nil, err
			}
			resolvedEndpoint = built
		}
		newCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, request.bearer, request.baseURL, resolvedEndpoint)
		if err != nil {
			return ctx, nil, fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		gateMemberChoices, err = s.newMemberChoices(ctx, authentication)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "load upstream client choices").LogError(ctx, logger)
		}
		if gateMemberChoices == nil && membership.agentID != uuid.Nil {
			// Decided before the token path, so members the key has a choice
			// for resolve lazily like any other chosen member.
			agentKeyChoicesRead = true
			gateMemberChoices, err = s.agentKeyMemberChoices(ctx, metaServer.OrganizationID, membership.agentID)
			if err != nil {
				return ctx, nil, oops.E(oops.CodeUnexpected, err, "load agent key credential choices").LogError(ctx, logger)
			}
		}
		if gateMemberChoices != nil || (toolSelection != nil && toolSelection.Catalog != nil) {
			// Proxied members resolve only their chosen clients, so the
			// issuer's other clients resolve lazily, for hosted members that
			// need them, and an unrelated broken one blocks nothing else.
			gateProjectTokens = s.newOrganizationGatewayTokens(authentication.endpoint, authentication.subject)
		} else {
			tokens, err := s.resolveIssuerGateAccessTokens(ctx, w, authentication)
			if err != nil {
				return ctx, nil, fmt.Errorf("apply issuer gate: %w", err)
			}
			gateTokens = tokens
		}
		gatePolicy = toolSelection
		gateAuthentication = authentication
	}
	if gateMemberChoices == nil && membership.agentID != uuid.Nil && !agentKeyChoicesRead {
		agentKeyChoices, err := s.agentKeyMemberChoices(ctx, metaServer.OrganizationID, membership.agentID)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "load agent key credential choices").LogError(ctx, logger)
		}
		gateMemberChoices = agentKeyChoices
	}
	connectURL, connectRoot := "", ""
	if metaServer.UserSessionIssuerID.Valid && resolvedEndpoint != nil {
		root, err := resolvedEndpoint.RootURL(request.baseURL)
		if err != nil {
			logger.WarnContext(ctx, "build gateway connect url", attr.SlogError(err))
		} else {
			connectURL = root + "/connect/first-party"
			if membership.agentID == uuid.Nil {
				connectRoot = root
			}
		}
	}
	gate := &metaGateContext{
		projectID:    mcpEndpoint.ProjectID,
		metaServerID: metaServer.ID,
		// Both unset for a stored gateway, whose members come from
		// meta_mcp_server_members rather than from a caller's grants.
		agentID:             membership.agentID,
		organizationGateway: membership.organization,
		organizationID:      metaServer.OrganizationID,
		tokens:              gateTokens,
		projectTokens:       gateProjectTokens,
		memberChoices:       gateMemberChoices,
		// Uninitialized when ungated, which disables identity chaining.
		userSessionIssuerID: metaServer.UserSessionIssuerID.UUID,
		connectRoot:         connectRoot,
		chainUpstream:       nil,
		catalog:             nil,
		consentDiagnostics:  nil,
		catalogRef:          nil,
		authentication:      gateAuthentication,
		unapprovedMembers:   nil,
		hiddenMembers:       nil,
		execute:             false,
		discoveryMode:       metamcp.ResolveDiscoveryMode(metaServer.DiscoveryMode),
		authenticated:       false,
		sessionID:           request.sessionID,
		chatID:              request.chatID,
		userID:              "",
		externalUserID:      "",
		apiKeyID:            "",
		// Member dispatch carries this InEffect verbatim; nothing on the
		// tools/call path reads it (upstream dials pin their own version).
		protocolVersion: request.resolution,
		connectURL:      connectURL,
	}
	if gatePolicy != nil {
		gate.catalogRef = gatePolicy.Catalog
		if gatePolicy.Gateway != nil && gatePolicy.Gateway.DiscoveryMode != nil {
			gate.discoveryMode = *gatePolicy.Gateway.DiscoveryMode
		}
	}
	// An agent gateway serves its delegated members progressively; the
	// organization gateway follows the connection's choice.
	if membership.agentID != uuid.Nil {
		gate.discoveryMode = metamcp.DiscoveryModeProgressive
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
		// A derived gateway has no endpoint project to own, and its caller was
		// already authenticated against this organization before dispatch. It
		// is authenticated by construction; leaving it false would list a
		// granted private member and then fail to drill into it.
		if membership.derived() {
			gate.authenticated = authCtx.ActiveOrganizationID == metaServer.OrganizationID
		} else if authCtx.ActiveOrganizationID != "" {
			projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ctx, nil, oops.E(oops.CodeUnexpected, err, "error checking project access").LogError(ctx, logger)
			}
			for _, project := range projects {
				if project.ID == mcpEndpoint.ProjectID {
					gate.authenticated = true
					break
				}
			}
		}
	}
	if gate.discoveryMode == metamcp.DiscoveryModeCode {
		switch {
		case !codeModeSignedIn(ctx, metaServer):
			// Code Mode costs a runner per execution, so only signed-in
			// callers get it. The API refuses Code Mode on a gateway without
			// sign-in; this covers rows written outside it.
			logger.WarnContext(ctx, "code mode requires sign-in, serving gateway in progressive mode")
			gate.discoveryMode = metamcp.DiscoveryModeProgressive
		case !s.codeExecutor.Available():
			logger.WarnContext(ctx, "code mode runtime is unavailable, serving gateway in progressive mode")
			gate.discoveryMode = metamcp.DiscoveryModeProgressive
		case !s.CodeModeAvailable(ctx, gate.organizationID):
			// Code Mode is rolled out per organization; a gateway kept in Code
			// Mode after its organization lost the entitlement keeps working.
			logger.DebugContext(ctx, "organization is not entitled to code mode, serving gateway in progressive mode")
			gate.discoveryMode = metamcp.DiscoveryModeProgressive
		}
	}

	return ctx, gate, nil
}

// codeModeSignedIn reports whether the issuer gate signed this caller in.
// Gateways without an issuer serve anonymous callers, who never get Code Mode.
func codeModeSignedIn(ctx context.Context, metaServer *metamcprepo.MetaMcpServer) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	return metaServer.UserSessionIssuerID.Valid && ok && authCtx != nil
}
