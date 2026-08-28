// Consent-scoped MCP transport. The consent island is an ordinary MCP client
// (official TS SDK) speaking Streamable HTTP against
// `POST|DELETE /{routeBase}/{slug}/connect/mcp`, authorized pre-mint by the
// opaque challenge state plus consent CSRF token in headers — never by the
// issuer gate, which stays untouched. The method surface is a hard
// allowlist; every tools/list page the transport relays is captured into the
// attempt's inventory snapshot before relay, so approval binds to exactly
// what the island displayed.

package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Consent transport request headers. Header-borne so request bodies stay
// pure MCP messages. The attempt id is non-secret; state + CSRF are the
// credential.
const (
	consentStateHeader   = "Gram-Consent-State"
	consentCSRFHeader    = "Gram-Consent-Csrf"
	consentAttemptHeader = "Gram-Consent-Inventory-Attempt"
)

// consentMCPBodyLimit bounds inbound consent transport bodies.
const consentMCPBodyLimit = 64 << 10

// consentUpstreamTimeout bounds one proxied consent request end to end.
const consentUpstreamTimeout = 20 * time.Second

// consentAllowedMethods is the whole consent-credential method surface.
// Everything else is rejected before any backend dispatch, on both backend
// families. Extend deliberately; every addition widens what a pre-mint
// credential can reach.
var consentAllowedMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"ping":                      true,
	"tools/list":                true,
}

// HandleConsentMCP serves `POST|DELETE /mcp/{mcpSlug}/connect/mcp`.
func (s *Service) HandleConsentMCP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, "mcp")
	if err != nil {
		return err
	}
	return s.ServeConsentMCP(w, r, endpoint)
}

// ServeConsentMCP is the post-resolution handler, shared with /x/mcp.
func (s *Service) ServeConsentMCP(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		return oops.E(oops.CodeMethodNotAllowed, nil, "method not allowed").LogWarn(ctx, logger)
	}
	r.Body = http.MaxBytesReader(w, r.Body, consentMCPBodyLimit)

	// The consent transport enumerates live tool inventories, so it follows
	// the runtime MCP dispatch's custom-domain lockdown. The consent HTML page
	// stays reachable like the install page, but hides its picker island on a
	// locked-down platform origin; on the custom domain the island's same-host
	// relative fetch has already passed the ingress allowlist.
	if err := s.enforceCustomDomainLockdown(ctx, logger, endpoint.ProjectID); err != nil {
		return err
	}

	// The transport exists only while the organization's admin opt-in is
	// enabled; an unavailable checker reads as off. 404 matches how the
	// island's absence hides the surface entirely.
	if !s.consentToolFilteringEnabled(ctx, logger, endpoint.OrganizationID) {
		return oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger)
	}
	eligible, err := s.consentToolPickerEligible(ctx, endpoint)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
	}
	if !eligible {
		return oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger)
	}
	// Mixed credentials are a confusion smell: the consent transport never
	// authenticates with Gram bearer tokens.
	if r.Header.Get("Authorization") != "" {
		return oops.E(oops.CodeBadRequest, nil, "consent requests must not carry an Authorization header").LogWarn(ctx, logger)
	}

	stateID := r.Header.Get(consentStateHeader)
	csrfToken := r.Header.Get(consentCSRFHeader)
	rawAttempt := r.Header.Get(consentAttemptHeader)
	if stateID == "" || csrfToken == "" || rawAttempt == "" {
		return oops.E(oops.CodeBadRequest, nil, "consent state, csrf, and inventory attempt headers are required").LogWarn(ctx, logger)
	}
	attempt, err := consentAttemptID(rawAttempt)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid consent inventory attempt id").LogWarn(ctx, logger)
	}

	// Plain Get: consent transport calls are idempotent within the challenge
	// TTL and must not consume the state the approve POST needs.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogWarn(ctx, logger)
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if err := endpoint.ValidateRef(challengeState.Endpoint); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server").LogWarn(ctx, logger)
	}
	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(csrfToken), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeUnauthorized, nil, "invalid consent csrf token").LogWarn(ctx, logger)
	}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogWarn(ctx, logger)
	}
	if challengeState.FirstParty {
		return oops.E(oops.CodeBadRequest, nil, "first-party connect challenges have no tool picker").LogWarn(ctx, logger)
	}
	if _, err := s.resolveUserSessionClient(ctx, logger, endpoint, challengeState.ClientID, lookupClientOnly); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}

	var serverRow *mcpservers_repo.McpServer
	if endpoint.McpServerID.Valid {
		row, rerr := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
			ID:        endpoint.McpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if rerr != nil {
			return oops.E(oops.CodeUnexpected, rerr, "load mcp server for consent transport").LogError(ctx, logger)
		}
		serverRow = &row
	}

	draft, err := s.loadConsentInventoryDraft(ctx, endpoint, stateID, attempt)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
	}

	// The consent credential must never travel upstream.
	r.Header.Del(consentStateHeader)
	r.Header.Del(consentCSRFHeader)
	r.Header.Del(consentAttemptHeader)
	r.Header.Del("Cookie")

	if endpoint.ToolsetID.Valid {
		return s.serveConsentToolsetMCP(w, r, endpoint, challengeState, draft)
	}
	if serverRow != nil {
		return s.serveConsentProxiedMCP(w, r, endpoint, challengeState, draft, serverRow)
	}
	return oops.E(oops.CodeUnexpected, nil, "mcp endpoint has neither toolset nor mcp server backend").LogError(ctx, logger)
}

// serveConsentToolsetMCP answers the consent method surface locally for
// toolset-backed endpoints: Gram is the MCP server, so there is no upstream
// handshake and the whole inventory is one page, snapshotted before the
// response is written.
func (s *Service) serveConsentToolsetMCP(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, draft consentToolInventory) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusOK)
		return nil
	}

	req, err := decodeConsentJSONRPCRequest(w, r)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid jsonrpc request").LogWarn(ctx, logger)
	}
	if !consentAllowedMethods[req.Method] {
		return writeConsentJSONRPCError(w, req.ID, proxy.RejectCodeMethodNotFound, "method is not available on the consent transport")
	}

	switch req.Method {
	case "initialize":
		sessionID := uuid.NewString()
		draft.McpSessionID = sessionID
		if err := s.consentToolInventoryCache.Store(ctx, draft); err != nil {
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		w.Header().Set(proxy.McpSessionIDHeader, sessionID)
		return writeConsentJSONRPCResult(w, req.ID, map[string]any{
			"protocolVersion": consentProtocolVersion(req.Params),
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "gram", "version": "1.0.0"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return nil
	case "ping":
		return writeConsentJSONRPCResult(w, req.ID, map[string]any{})
	case "tools/list":
		toolset, terr := toolsets_repo.New(s.db).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
			ID:        endpoint.ToolsetID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if terr != nil {
			return oops.E(oops.CodeUnexpected, terr, "load toolset for consent authorization").LogError(ctx, logger)
		}
		private := !endpoint.IsPublic || !toolset.McpIsPublic
		if private && challengeState.Subject.Kind == urn.SessionSubjectKindAnonymous {
			return oops.E(oops.CodeUnauthorized, nil, "anonymous subject cannot enumerate a private MCP server").LogWarn(ctx, logger)
		}
		// The RBAC gate keys off the consent subject, exactly like the
		// runtime request the minted session will make.
		authzCtx, cerr := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
		if cerr != nil {
			return oops.E(oops.CodeUnexpected, cerr, "stamp consent subject context").LogError(ctx, logger)
		}
		if private {
			if _, ok := contextvalues.GetAuthContext(authzCtx); !ok {
				return oops.E(oops.CodeUnauthorized, nil, "consent subject has no authenticated context").LogWarn(ctx, logger)
			}
		}
		if s.authz != nil && private {
			if authzCtx, cerr = s.authz.PrepareContext(authzCtx); cerr != nil {
				return oops.E(oops.CodeUnexpected, cerr, "load access grants for consent inventory").LogError(ctx, logger)
			}
			if cerr = s.authz.Require(authzCtx, authz.MCPCheck(authz.ScopeMCPConnect, endpoint.ToolsetID.UUID.String(), endpoint.ProjectID.String())); cerr != nil {
				return fmt.Errorf("authorize consent inventory access: %w", mcpaccess.ServerPermissionDenied(cerr, s.requestAccessURL(authzCtx, endpoint.ToolsetID.UUID.String(), toolset.Name)))
			}
		}
		tools, roleHidden, terr := s.enumerateToolsetConsentInventory(authzCtx, endpoint)
		if terr != nil {
			return oops.E(oops.CodeUnexpected, terr, "resolve toolset for consent inventory").LogError(ctx, logger)
		}
		if _, aerr := s.appendConsentInventoryPage(ctx, draft, consentRequestCursor(req.Params), tools, ""); aerr != nil {
			if errors.Is(aerr, errConsentInventoryUnavailable) {
				return oops.E(oops.CodeUnavailable, aerr, "service temporarily unavailable").LogError(ctx, logger)
			}
			return oops.E(oops.CodeInvalid, aerr, "the tool inventory could not be captured").LogWarn(ctx, logger)
		}
		result := map[string]any{
			"tools": consentWireTools(tools),
		}
		if len(roleHidden) > 0 {
			result["_meta"] = map[string]any{"gram.dev/roleHiddenTools": consentRoleHiddenMeta(roleHidden)}
		}
		return writeConsentJSONRPCResult(w, req.ID, result)
	default:
		return writeConsentJSONRPCError(w, req.ID, proxy.RejectCodeMethodNotFound, "method is not available on the consent transport")
	}
}

// serveConsentProxiedMCP relays the consent method surface through the same
// proxy stack the runtime uses for remote/tunneled backends: guardian
// policy, configured headers, tunnel routing/retry, and RBAC list filtering
// all apply, with a nil session selection so consent sees the full
// RBAC-allowed inventory. tools/list pages are captured post-RBAC, before
// relay.
func (s *Service) serveConsentProxiedMCP(
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	draft consentToolInventory,
	serverRow *mcpservers_repo.McpServer,
) error {
	ctx, cancel := context.WithTimeout(r.Context(), consentUpstreamTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	logger := endpoint.LogWith(s.logger)

	if !serverRow.RemoteMcpServerID.Valid && !serverRow.TunneledMcpServerID.Valid {
		return oops.E(oops.CodeUnexpected, nil, "mcp server backend is not proxied").LogError(ctx, logger)
	}

	// DELETE terminates the upstream session the attempt's handshake
	// established — and only that session.
	if r.Method == http.MethodDelete {
		if draft.McpSessionID == "" || r.Header.Get(proxy.McpSessionIDHeader) != draft.McpSessionID {
			return oops.E(oops.CodeBadRequest, nil, "unknown consent transport session").LogWarn(ctx, logger)
		}
	}

	subject := *challengeState.Subject
	if serverRow.Visibility == mcpservers.VisibilityPrivate && subject.Kind == urn.SessionSubjectKindAnonymous {
		return oops.E(oops.CodeUnauthorized, nil, "anonymous subject cannot enumerate a private MCP server").LogWarn(ctx, logger)
	}
	tokens, err := s.remoteChallengeMgr.ResolveAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject)
	if err != nil {
		if errors.Is(err, remotesessions.ErrNoValidToken) {
			return oops.E(oops.CodeConflict, err, "connect the upstream service before choosing tools").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for consent transport").LogError(ctx, logger)
	}
	upstreamToken, err := routeUpstreamToken(ctx, logger, tokens, endpoint.UpstreamResource)
	var routeErr *upstreamRoutingError
	switch {
	case errors.As(err, &routeErr):
		// routeUpstreamToken already logged the structured detail.
		return oops.E(oops.CodeFailedPrecondition, err, "this MCP server's upstream credentials are not configured unambiguously")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "resolve upstream token for consent transport").LogError(ctx, logger)
	}

	ctx, err = s.contextForSessionSubject(ctx, endpoint, subject, "consent:"+challengeState.ID, challengeState.ClientID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "stamp consent subject context").LogError(ctx, logger)
	}
	if serverRow.Visibility == mcpservers.VisibilityPrivate {
		if _, ok := contextvalues.GetAuthContext(ctx); !ok {
			return oops.E(oops.CodeUnauthorized, nil, "consent subject has no authenticated context").LogWarn(ctx, logger)
		}
	}
	ctx, err = s.authorizeProxyBackendAccess(ctx, logger, endpoint.ProjectID, serverRow)
	if err != nil {
		return fmt.Errorf("authorize consent transport access: %w", err)
	}
	r = r.WithContext(ctx)

	var p *proxy.Proxy
	if serverRow.RemoteMcpServerID.Valid {
		if s.remoteProxyManager == nil {
			return oops.E(oops.CodeUnexpected, nil, "remote MCP proxy manager is unavailable").LogError(ctx, logger)
		}
		remoteServer, rerr := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{
			ID:        serverRow.RemoteMcpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if rerr != nil {
			return oops.E(oops.CodeUnexpected, rerr, "load remote mcp server for consent transport").LogError(ctx, logger)
		}
		headers, herr := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, remoteServer.ID, false)
		if herr != nil {
			return oops.E(oops.CodeUnexpected, herr, "load remote mcp server headers for consent transport").LogError(ctx, logger)
		}
		p = s.remoteProxyManager.Build(logger, &remoteServer, serverRow.ID.String(), headers, serverRow.Visibility, endpoint.OrganizationID, endpoint.ProjectID.String(), upstreamToken, "", nil)
	} else {
		// One state-derived affinity key pins the whole consent session
		// (initialize, list pages, DELETE) to a single gateway.
		affinity := tunnelrouting.HashedClientAffinityKey("consent", challengeState.ID)
		p, err = s.tunnelManager.buildProxy(ctx, affinity, logger, endpoint.ProjectID, endpoint.OrganizationID, serverRow, upstreamToken, "", nil)
		if err != nil {
			return err
		}
	}

	p.UserRequestInterceptors = append([]proxy.UserRequestInterceptor{consentMethodAllowlistInterceptor{}}, p.UserRequestInterceptors...)
	p.ToolsListResponseInterceptors = append(p.ToolsListResponseInterceptors, &consentInventoryCaptureInterceptor{service: s, draft: &draft})

	// The build above passes a nil selection (consent must see the full
	// RBAC-allowed inventory), which leaves StrictToolSelection off. Turn it
	// on explicitly: the allowlist authorizes from the decoded message while
	// the proxy forwards the original bytes, so this transport needs the
	// strict path's single-unambiguous-message guarantee, and a tools/list
	// page the capture interceptor cannot decode must fail closed rather
	// than relay.
	p.StrictToolSelection = true

	sessionWriter := &consentSessionCaptureWriter{ResponseWriter: w, sessionID: ""}
	if err := serveProxyBackend(sessionWriter, r, p); err != nil {
		return err
	}

	// Record the handshake's session id once. The island sends its next
	// request only after this response completes, so the DELETE check above
	// reads a stored value.
	if len(sessionWriter.sessionID) > consentInventoryMaxSessionIDBytes {
		sessionWriter.sessionID = ""
	}
	if sessionWriter.sessionID != "" && draft.McpSessionID == "" {
		// An SSE response can be flushed to the island before this handler
		// returns, allowing its next tools/list request to capture a page in
		// parallel. Re-read before storing the session id so a stale initialize
		// draft cannot overwrite that newer capture.
		freshDraft, ferr := s.loadConsentInventoryDraft(ctx, endpoint, challengeState.ID, draft.Attempt)
		if ferr != nil {
			s.logger.WarnContext(ctx, "reload consent inventory before recording session id", attr.SlogError(ferr))
		} else if freshDraft.McpSessionID == "" {
			freshDraft.McpSessionID = sessionWriter.sessionID
			if serr := s.consentToolInventoryCache.Store(ctx, freshDraft); serr != nil {
				s.logger.WarnContext(ctx, "record consent transport session id", attr.SlogError(serr))
			}
		}
	}
	return nil
}

// consentMethodAllowlistInterceptor rejects every JSON-RPC message outside
// the consent method surface before it reaches upstream.
type consentMethodAllowlistInterceptor struct{}

var _ proxy.UserRequestInterceptor = consentMethodAllowlistInterceptor{}

// Name implements proxy.UserRequestInterceptor.
func (consentMethodAllowlistInterceptor) Name() string { return "consent-method-allowlist" }

// InterceptUserRequest implements proxy.UserRequestInterceptor. Client-to-
// server responses are rejected too: nothing on the consent surface issues
// server-initiated requests.
func (consentMethodAllowlistInterceptor) InterceptUserRequest(_ context.Context, req *proxy.UserRequest) error {
	if req == nil {
		return fmt.Errorf("consent transport received no parsed request")
	}
	for _, msg := range req.JSONRPCMessages {
		rpcReq, ok := msg.(*jsonrpc.Request)
		if !ok || !consentAllowedMethods[rpcReq.Method] {
			return &proxy.RejectError{
				Code:    proxy.RejectCodeMethodNotFound,
				Message: "method is not available on the consent transport",
				Data:    nil,
			}
		}
	}
	return nil
}

// consentInventoryCaptureInterceptor snapshots every relayed tools/list page
// into the attempt draft before the proxy relays it, keeping the displayed
// and approval-bound inventories identical. Upstream JSON-RPC errors pass
// through uncaptured; a capture failure fails the relay closed.
type consentInventoryCaptureInterceptor struct {
	service *Service
	draft   *consentToolInventory
}

var _ proxy.ToolsListResponseInterceptor = (*consentInventoryCaptureInterceptor)(nil)

// Name implements proxy.ToolsListResponseInterceptor.
func (i *consentInventoryCaptureInterceptor) Name() string { return "consent-inventory-capture" }

// InterceptToolsListResponse implements proxy.ToolsListResponseInterceptor.
func (i *consentInventoryCaptureInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Error != nil {
		return nil
	}
	if list.Result == nil {
		return fmt.Errorf("consent capture: tools/list response carries neither result nor error")
	}
	requestCursor := ""
	if list.Request != nil && list.Request.Params != nil {
		requestCursor = list.Request.Params.Cursor
	}
	pageTools := make([]consentInventoryTool, 0, len(list.Result.Tools))
	for _, tool := range list.Result.Tools {
		if tool == nil {
			continue
		}
		pageTools = append(pageTools, consentInventoryTool{
			Name:        tool.Name,
			Annotations: consentAnnotationsFromSDK(tool.Annotations),
		})
	}
	updated, err := i.service.appendConsentInventoryPage(ctx, *i.draft, requestCursor, pageTools, list.Result.NextCursor)
	if err != nil {
		return fmt.Errorf("capture consent tool inventory page: %w", err)
	}
	*i.draft = updated

	var sanitized []*mcp.Tool
	for idx, tool := range list.Result.Tools {
		if tool == nil || tool.OutputSchema == nil {
			continue
		}
		if sanitized == nil {
			sanitized = append([]*mcp.Tool(nil), list.Result.Tools...)
		}
		clone := *tool
		clone.OutputSchema = nil
		sanitized[idx] = &clone
	}
	if sanitized != nil {
		// The browser SDK eagerly compiles output schemas with eval, which the
		// consent page's CSP intentionally forbids.
		if err := list.SetTools(sanitized); err != nil {
			return fmt.Errorf("strip output schemas from consent tool inventory: %w", err)
		}
	}
	return nil
}

// consentSessionCaptureWriter tees the Mcp-Session-Id response header the
// upstream handshake assigns.
type consentSessionCaptureWriter struct {
	http.ResponseWriter
	sessionID string
}

var _ http.Flusher = (*consentSessionCaptureWriter)(nil)

func (w *consentSessionCaptureWriter) WriteHeader(status int) {
	if w.sessionID == "" {
		w.sessionID = w.Header().Get(proxy.McpSessionIDHeader)
	}
	w.ResponseWriter.WriteHeader(status)
}

// Flush implements http.Flusher so the proxy's SSE relay path detects
// flushing support through the wrapper.
func (w *consentSessionCaptureWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// consentJSONRPCRequest is the locally served JSON-RPC request shape.
type consentJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func decodeConsentJSONRPCRequest(w http.ResponseWriter, r *http.Request) (*consentJSONRPCRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read jsonrpc request body: %w", err)
	}
	if err := proxy.ValidateStrictJSONRPCBody(body); err != nil {
		return nil, fmt.Errorf("validate jsonrpc request: %w", err)
	}
	var req consentJSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode jsonrpc request: %w", err)
	}
	if req.JSONRPC != "2.0" {
		return nil, fmt.Errorf(`jsonrpc version must be "2.0"`)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("jsonrpc method is required")
	}
	return &req, nil
}

// consentProtocolVersion echoes the client's requested protocol version so
// the SDK accepts the handshake; a missing value falls back to the current
// spec revision.
func consentProtocolVersion(params json.RawMessage) string {
	var decoded struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &decoded); err == nil && decoded.ProtocolVersion != "" {
			return decoded.ProtocolVersion
		}
	}
	return "2025-06-18"
}

// consentRequestCursor extracts params.cursor from a locally served
// tools/list request.
func consentRequestCursor(params json.RawMessage) string {
	var decoded struct {
		Cursor string `json:"cursor"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &decoded); err == nil {
			return decoded.Cursor
		}
	}
	return ""
}

// consentRoleHiddenNamesCap bounds how many RBAC-hidden tool names ride the
// _meta payload; the count always reflects the full total.
const consentRoleHiddenNamesCap = 100

// consentRoleHiddenMeta shapes the RBAC-hidden disclosure for the island:
// the full count plus at most consentRoleHiddenNamesCap names, so a huge
// narrowed toolset cannot bloat the listing response.
func consentRoleHiddenMeta(hidden []string) map[string]any {
	names := hidden
	if len(names) > consentRoleHiddenNamesCap {
		names = names[:consentRoleHiddenNamesCap]
	}
	return map[string]any{"count": len(hidden), "names": names}
}

// consentWireTools maps captured tools to the standard MCP wire shape:
// names plus explicitly-true annotation hints. Consent renders nothing
// else, so descriptions and input schemas are deliberately replaced by a
// permissive placeholder schema.
func consentWireTools(tools []consentInventoryTool) []map[string]any {
	wire := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		entry := map[string]any{
			"name":        tool.Name,
			"inputSchema": map[string]any{"type": "object"},
		}
		if hints := consentHintsFromAnnotations(tool.Annotations); hints != nil {
			entry["annotations"] = hints
		}
		wire = append(wire, entry)
	}
	return wire
}

// consentHintsFromAnnotations converts vocabulary values back to raw MCP
// hint booleans. Nil when no hint is explicitly true, so the wire omits the
// annotations object exactly like an unannotated upstream tool.
func consentHintsFromAnnotations(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	hints := map[string]bool{}
	for _, value := range values {
		switch value {
		case toolfilter.AnnotationReadOnly:
			hints["readOnlyHint"] = true
		case toolfilter.AnnotationDestructive:
			hints["destructiveHint"] = true
		case toolfilter.AnnotationIdempotent:
			hints["idempotentHint"] = true
		case toolfilter.AnnotationOpenWorld:
			hints["openWorldHint"] = true
		}
	}
	return hints
}

// consentAnnotationsFromSDK maps SDK annotation hints to the consent
// vocabulary, keeping only hints that are explicitly true — the same
// fail-closed axis toolfilter applies. The SDK models readOnly/idempotent
// as plain bools and destructive/openWorld as *bool (their spec defaults
// are true); a nil pointer is not an explicit true.
func consentAnnotationsFromSDK(annotations *mcp.ToolAnnotations) []string {
	if annotations == nil {
		return nil
	}
	var values []string
	if annotations.ReadOnlyHint {
		values = append(values, toolfilter.AnnotationReadOnly)
	}
	if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
		values = append(values, toolfilter.AnnotationDestructive)
	}
	if annotations.IdempotentHint {
		values = append(values, toolfilter.AnnotationIdempotent)
	}
	if annotations.OpenWorldHint != nil && *annotations.OpenWorldHint {
		values = append(values, toolfilter.AnnotationOpenWorld)
	}
	return values
}

func writeConsentJSONRPCResult(w http.ResponseWriter, id json.RawMessage, result any) error {
	return writeConsentJSONRPCEnvelope(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      consentJSONRPCID(id),
		"result":  result,
	})
}

func writeConsentJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) error {
	return writeConsentJSONRPCEnvelope(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      consentJSONRPCID(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}

func writeConsentJSONRPCEnvelope(w http.ResponseWriter, envelope map[string]any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(envelope); err != nil {
		return fmt.Errorf("encode consent jsonrpc envelope: %w", err)
	}
	return nil
}

func consentJSONRPCID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}
