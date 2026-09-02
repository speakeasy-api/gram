// Per-member upstream calls for the meta MCP runtime. Each call is one
// synthesized JSON-RPC exchange driven through the same proxy machinery a
// direct proxied endpoint uses (SSRF policy, billing, telemetry identity, and
// per-tool RBAC ride along via ProxyManager.BuildTarget), with the response
// captured and parsed by the meta MCP rather than relayed to the client.
//
// Handshake-first: session-ful upstreams reject bare requests only in-band,
// indistinguishable from a real tool error, so every member session opens
// with initialize. A stateless upstream answers it and returns no session id.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// metaMemberUpstreamProtocolVersion is the version the meta MCP speaks to
// member upstreams; independent of the version negotiated with the client.
const metaMemberUpstreamProtocolVersion = "2025-06-18"

// memberSessionCloseTimeout bounds the best-effort session DELETE, on a
// detached context so a member-call timeout cannot strand the session.
const memberSessionCloseTimeout = 5 * time.Second

// routeMetaMemberToken selects the bearer forwarded to one meta MCP member.
// No lone-token fallback on either arm: mcp:write can attach a member
// pointing anywhere, and forwarding an unmatched credential would hand it a
// sibling's bearer. No match means an anonymous call, never a mismatched one.
//
// A remote member matches on recorded RFC 8707 resource across the whole map,
// because its routing key is its upstream URL — the same address the proxy
// dials, so a credential that matches is being returned to the audience it
// names.
//
// A tunneled member is routed by identity alone: only the entry keyed by its
// own derived remote_session_issuer (mcpserverissuersync.go), accepted when
// that grant is unqualified or names this member's recorded resource
// identifier. The identifier cannot select across issuers the way a remote
// URL does, because a tunnel's dial target is decoupled from the resource it
// claims — an operator-supplied identifier that collided with a sibling's
// upstream would otherwise deliver that sibling's bearer to the tunnel.
func routeMetaMemberToken(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember, upstreamResource string) (string, error) {
	want := strings.TrimRight(upstreamResource, "/")
	if member.tunneledServerID.Valid {
		return tunneledIssuerToken(tokens, member.remoteSessionIssuerID, want), nil
	}
	if want == "" {
		return "", nil
	}
	matched := ""
	found := 0
	for _, entry := range tokens {
		if grantRoutesToUpstream(entry.Resource, want, false) {
			matched = entry.Token
			found++
		}
	}
	switch found {
	case 0:
		return "", nil
	case 1:
		return matched, nil
	default:
		// Several credentials claim the same upstream, so forwarding any one
		// would be a guess. Name the duplication rather than the symptom.
		return "", &metaMemberError{message: fmt.Sprintf("server %q has %d upstream credentials recorded for the same upstream, so none can be chosen; disconnect the duplicates from this gateway's sign-in and reconnect once", member.slug, found)}
	}
}

// memberProxyBuilder yields a fresh proxy per upstream exchange, since a
// Proxy is a one-request value. It takes the exchange's own context so a
// detached close is not built on an expired call context.
type memberProxyBuilder func(ctx context.Context) (*proxy.Proxy, error)

// memberDial is a routed member's proxy builder plus whether routing found
// no credential, so a 401 can name the gateway's gap, not a rejected token.
type memberDial struct {
	build     memberProxyBuilder
	anonymous bool
}

// memberAuthFailure names the member-scoped meaning of an upstream 401/403.
func memberAuthFailure(member metaMember, anonymous bool) error {
	if anonymous {
		return &metaMemberError{message: fmt.Sprintf("server %q requires authentication and this gateway holds no credential that routes to it; connect it from this gateway's sign-in page", member.slug)}
	}
	return &metaMemberError{message: fmt.Sprintf("server %q rejected the stored credential; reconnect it from this gateway's sign-in page", member.slug)}
}

// dialMetaMember loads the member's backend rows, routes its credential
// strictly, and returns the per-exchange proxy builder with the routing
// outcome. The snapshot already
// enforced mcp:connect for private members with the same key
// authorizeProxyBackendAccess uses; per-tool RBAC for private members
// attaches inside the proxy build.
func (s *Service) dialMetaMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
) (memberDial, error) {
	serverRow, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        member.serverID,
		ProjectID: gate.projectID,
	})
	if err != nil {
		return memberDial{}, fmt.Errorf("load meta MCP member server: %w", err)
	}

	// gate.toolSelection is provably nil today: meta endpoints mint no tool
	// selections. If they ever do, its names are meta-MCP-qualified and would
	// have to be translated before reaching a member proxy's strict filter.
	switch {
	case member.remoteServerID.Valid:
		remoteServer, rerr := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{
			ID:        member.remoteServerID.UUID,
			ProjectID: gate.projectID,
		})
		if errors.Is(rerr, pgx.ErrNoRows) {
			// The snapshot query does not join the backend source tables, so a
			// soft-deleted upstream still yields a member. Isolate it rather
			// than failing every member of the gateway.
			return memberDial{}, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
		}
		if rerr != nil {
			return memberDial{}, fmt.Errorf("load meta MCP member upstream: %w", rerr)
		}
		headers, herr := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, remoteServer.ID, false)
		if herr != nil {
			return memberDial{}, fmt.Errorf("load meta MCP member upstream headers: %w", herr)
		}
		upstreamToken, terr := routeMetaMemberToken(gate.tokens, member, strings.TrimRight(remoteServer.Url, "/"))
		if terr != nil {
			s.metrics.RecordMetaMemberDispatch(ctx, "remote", mcpmetrics.MetaDispatchAmbiguous)
			return memberDial{}, terr
		}
		s.metrics.RecordMetaMemberDispatch(ctx, "remote", dispatchOutcome(upstreamToken))
		return memberDial{anonymous: upstreamToken == "", build: func(context.Context) (*proxy.Proxy, error) {
			// No WWW-Authenticate relay: a member's auth challenge must not
			// invite the client to re-authenticate against the meta MCP.
			p := s.remoteProxyManager.Build(logger, &remoteServer, member.serverID.String(), headers, member.visibility, gate.organizationID, gate.projectID.String(), upstreamToken, "", gate.toolSelection, remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()))
			// Meta-MCP-synthesized initializes are not client sessions.
			p.InitializeRequestInterceptors = nil
			return p, nil
		}}, nil

	case member.tunneledServerID.Valid:
		upstreamToken, terr := routeMetaMemberToken(gate.tokens, member, strings.TrimRight(member.tunneledResourceIdentifier, "/"))
		if terr != nil {
			s.metrics.RecordMetaMemberDispatch(ctx, "tunneled", mcpmetrics.MetaDispatchAmbiguous)
			return memberDial{}, terr
		}
		s.metrics.RecordMetaMemberDispatch(ctx, "tunneled", dispatchOutcome(upstreamToken))
		// Per-member namespace so one caller's handshake, calls, and DELETE
		// land on one tunnel gateway.
		affinity := tunnelrouting.HashedClientAffinityKey("meta:"+member.serverID.String(), callerIdentity)
		return memberDial{anonymous: upstreamToken == "", build: func(ctx context.Context) (*proxy.Proxy, error) {
			p, berr := s.tunnelManager.buildProxy(ctx, affinity, logger, gate.projectID, gate.organizationID, &serverRow, upstreamToken, "", gate.toolSelection, remotemcp.WithoutToolsCallIdentityCoverage(), remotemcp.WithMetaMCPServerID(gate.metaServerID.String()))
			if berr != nil {
				return nil, fmt.Errorf("build tunnel proxy: %w", berr)
			}
			p.InitializeRequestInterceptors = nil
			return p, nil
		}}, nil

	default:
		return memberDial{}, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	}
}

func dispatchOutcome(token string) mcpmetrics.MetaDispatchOutcome {
	if token == "" {
		return mcpmetrics.MetaDispatchAnonymous
	}
	return mcpmetrics.MetaDispatchCredentialed
}

// memberAttributionContext gives member exchanges the identity billing and
// telemetry read: the caller's own when authenticated, else the endpoint's
// organization. Best effort: attribution must never fail the call.
func (s *Service) memberAttributionContext(ctx context.Context, logger *slog.Logger, gate *metaGateContext) context.Context {
	if _, ok := contextvalues.GetAuthContext(ctx); ok {
		return ctx
	}
	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgsRepo, s.billingRepository, gate.organizationID)
	if err != nil {
		logger.WarnContext(ctx, "attribute meta MCP member call", attr.SlogError(err))
		return ctx
	}
	projectID := gate.projectID
	sessionID := gate.sessionID
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  gate.organizationID,
		ProjectID:             &projectID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             &sessionID,
		OrganizationSlug:      orgMetadata.Slug,
		Email:                 nil,
		AccountType:           orgMetadata.GramAccountType,
		HasActiveSubscription: orgMetadata.HasActiveSubscription,
		Whitelisted:           orgMetadata.Whitelisted,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
		SupportOrganizationID: "",
	})
}

// memberResponseRecorder captures a member upstream exchange instead of
// relaying it. Flush is the no-op the proxy's SSE relay requires; the byte
// cap turns an oversized member response into a member-scoped failure.
type memberResponseRecorder struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	truncated bool
}

func newMemberResponseRecorder() *memberResponseRecorder {
	return &memberResponseRecorder{
		header:    make(http.Header),
		status:    http.StatusOK,
		body:      bytes.Buffer{},
		truncated: false,
	}
}

func (r *memberResponseRecorder) Header() http.Header { return r.header }

func (r *memberResponseRecorder) WriteHeader(status int) { r.status = status }

func (r *memberResponseRecorder) Write(p []byte) (int, error) {
	remaining := metamcp.MaxMemberResponseBytes - r.body.Len()
	if remaining <= 0 {
		r.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		r.truncated = true
		p = p[:remaining]
	}
	r.body.Write(p)
	return len(p), nil
}

func (r *memberResponseRecorder) Flush() {}

// memberSession is one open exchange scope with a proxied member: the
// handshake ran, and every call rides the same upstream session — which is
// what keeps pagination cursors valid across pages.
type memberSession struct {
	svc       *Service
	logger    *slog.Logger
	dial      memberDial
	member    metaMember
	sessionID string
}

// openMemberSession runs the initialize handshake. On any failure after a
// session was minted, the session is closed before returning.
func (s *Service) openMemberSession(ctx context.Context, logger *slog.Logger, dial memberDial, member metaMember) (*memberSession, error) {
	initBody, err := marshalUpstreamRequest(mcpjsonrpc.StringID("gram-gateway-init"), "initialize", map[string]any{
		"protocolVersion": metaMemberUpstreamProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "gram-gateway", "version": "1"},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal upstream initialize: %w", err)
	}
	initRec, err := s.memberExchange(ctx, dial.build, member, initBody, "")
	if err != nil {
		return nil, err
	}
	if initRec.status == http.StatusUnauthorized || initRec.status == http.StatusForbidden {
		return nil, memberAuthFailure(member, dial.anonymous)
	}
	if initRec.status < http.StatusOK || initRec.status >= http.StatusMultipleChoices {
		return nil, memberUpstreamFailure(member, initRec.status)
	}

	sess := &memberSession{svc: s, logger: logger, dial: dial, member: member, sessionID: initRec.header.Get(proxy.McpSessionIDHeader)}
	if sess.sessionID != "" {
		ackBody := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
		ackRec, aerr := s.memberExchange(ctx, dial.build, member, ackBody, sess.sessionID)
		if aerr != nil || ackRec.status < http.StatusOK || ackRec.status >= http.StatusMultipleChoices {
			sess.close(ctx)
			if aerr != nil {
				return nil, aerr
			}
			return nil, memberUpstreamFailure(member, ackRec.status)
		}
	}
	return sess, nil
}

// call performs one JSON-RPC request in this session and returns the
// upstream's result or error object.
func (sess *memberSession) call(ctx context.Context, method string, params any) (json.RawMessage, *upstreamRPCError, error) {
	requestID := mcpjsonrpc.StringID("gram-gateway-" + method)
	body, err := marshalUpstreamRequest(requestID, method, params)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal upstream %s request: %w", method, err)
	}
	rec, err := sess.svc.memberExchange(ctx, sess.dial.build, sess.member, body, sess.sessionID)
	if err != nil {
		return nil, nil, err
	}
	if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
		return nil, nil, memberAuthFailure(sess.member, sess.dial.anonymous)
	}
	if rec.status < http.StatusOK || rec.status >= http.StatusMultipleChoices {
		return nil, nil, memberUpstreamFailure(sess.member, rec.status)
	}
	if rec.truncated {
		return nil, nil, &metaMemberError{message: fmt.Sprintf("server %q returned a response too large for the meta MCP", sess.member.slug)}
	}
	envelope, perr := parseUpstreamResponse(rec, requestID)
	if perr != nil {
		sess.logger.WarnContext(ctx, "unparseable meta MCP member response", attr.SlogError(perr), attr.SlogMcpServerID(sess.member.serverID.String()))
		return nil, nil, &metaMemberError{message: fmt.Sprintf("server %q returned a response the meta MCP could not read", sess.member.slug)}
	}
	return envelope.Result, envelope.Error, nil
}

// close best-effort terminates the upstream session, on a detached context so
// an expired member-call deadline cannot strand it.
func (sess *memberSession) close(ctx context.Context) {
	if sess.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), memberSessionCloseTimeout)
	defer cancel()
	p, err := sess.dial.build(ctx)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/", nil)
	if err != nil {
		return
	}
	req.Header.Set(proxy.McpSessionIDHeader, sess.sessionID)
	if err := serveProxyBackend(httptest.NewRecorder(), req, p); err != nil {
		sess.logger.DebugContext(ctx, "close meta MCP member session", attr.SlogError(err), attr.SlogMcpServerID(sess.member.serverID.String()))
	}
}

// callProxiedMember performs one JSON-RPC request against a proxied member
// inside its own session and deadline. Every upstream-side failure comes
// back as *metaMemberError so callers degrade member-scoped.
func (s *Service) callProxiedMember(
	ctx context.Context,
	logger *slog.Logger,
	dial memberDial,
	member metaMember,
	method string,
	params any,
) (json.RawMessage, *upstreamRPCError, error) {
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()

	sess, err := s.openMemberSession(ctx, logger, dial, member)
	if err != nil {
		return nil, nil, err
	}
	defer sess.close(ctx)
	return sess.call(ctx, method, params)
}

// memberExchange drives one HTTP exchange through a fresh member proxy.
func (s *Service) memberExchange(ctx context.Context, build memberProxyBuilder, member metaMember, body []byte, sessionID string) (*memberResponseRecorder, error) {
	p, err := build(ctx)
	if err != nil {
		// A tunnel with no live route is a member outage, not a meta MCP bug.
		return nil, &metaMemberError{message: fmt.Sprintf("server %q is not reachable right now", member.slug)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", metaMemberUpstreamProtocolVersion)
	if sessionID != "" {
		req.Header.Set(proxy.McpSessionIDHeader, sessionID)
	}

	rec := newMemberResponseRecorder()
	if err := serveProxyBackend(rec, req, p); err != nil {
		// Timeouts, unreachable upstreams, and relayed protocol rejections
		// are the member's outage, not the meta MCP's.
		return nil, &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
	}
	return rec, nil
}

func memberUpstreamFailure(member metaMember, status int) error {
	return &metaMemberError{message: fmt.Sprintf("server %q upstream call failed with status %d", member.slug, status)}
}

// upstreamRPCError is the JSON-RPC error object a member upstream returned.
type upstreamRPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type upstreamEnvelope struct {
	ID     mcpjsonrpc.ID     `json:"id"`
	Result json.RawMessage   `json:"result"`
	Error  *upstreamRPCError `json:"error"`
}

// answers reports whether an envelope is the response to requestID: a
// matching id, or any error object — a null-id error is the spec's shape for
// a request the upstream could not attribute, and it is still the answer.
func (e *upstreamEnvelope) answers(requestID mcpjsonrpc.ID) bool {
	return e.Error != nil || (e.ID.IsSet() && e.ID.Value() == requestID.Value())
}

func marshalUpstreamRequest(id mcpjsonrpc.ID, method string, params any) ([]byte, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	bs, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc request: %w", err)
	}
	return bs, nil
}

// parseUpstreamResponse extracts the response envelope answering requestID
// from either a JSON body or the data frames of an SSE body; notifications
// and unrelated messages are skipped.
func parseUpstreamResponse(rec *memberResponseRecorder, requestID mcpjsonrpc.ID) (*upstreamEnvelope, error) {
	contentType := rec.header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		for _, frame := range sseDataFrames(rec.body.Bytes()) {
			var envelope upstreamEnvelope
			if err := json.Unmarshal(frame, &envelope); err != nil {
				continue
			}
			if envelope.answers(requestID) {
				return &envelope, nil
			}
		}
		return nil, fmt.Errorf("no matching response frame in event stream")
	}

	var envelope upstreamEnvelope
	if err := json.Unmarshal(rec.body.Bytes(), &envelope); err != nil {
		return nil, fmt.Errorf("decode upstream response: %w", err)
	}
	if !envelope.answers(requestID) {
		return nil, fmt.Errorf("upstream response answers a different request")
	}
	return &envelope, nil
}

// sseDataFrames joins each event's data lines with newlines per the SSE
// framing rules and returns one payload per event.
func sseDataFrames(body []byte) [][]byte {
	var frames [][]byte
	var lines [][]byte
	flush := func() {
		if len(lines) > 0 {
			frames = append(frames, bytes.Join(lines, []byte("\n")))
			lines = nil
		}
	}
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			flush()
			continue
		}
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			lines = append(lines, bytes.TrimPrefix(data, []byte(" ")))
		}
	}
	flush()
	return frames
}

// callerIdentity is the stable per-caller key member dispatch pins tunnel
// affinity on; falls back through the identities the gate resolved.
func (g *metaGateContext) callerIdentity() string {
	for _, id := range []string{g.sessionID, g.userID, g.externalUserID, g.apiKeyID} {
		if id != "" {
			return id
		}
	}
	return "anon"
}

// executeProxiedMemberTool forwards one tool call to a proxied member with
// the unqualified tool name — qualification is a meta MCP concept — and
// re-wraps the upstream result under the outer request id.
func (s *Service) executeProxiedMemberTool(
	ctx context.Context,
	logger *slog.Logger,
	gate *metaGateContext,
	member metaMember,
	req *rawRequest,
	toolName string,
	arguments json.RawMessage,
	meta *mcprequests.WireMeta,
) (json.RawMessage, error) {
	ctx = s.memberAttributionContext(ctx, logger, gate)
	dial, err := s.dialMetaMember(ctx, logger, *gate, member, gate.callerIdentity())
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dial meta MCP member").LogError(ctx, logger)
	}

	// The caller's _meta stays on our side of the wire: WireMeta is a lossy
	// observability parse (re-serializing it emits empty/null fields that
	// strict vendors reject with 400), and the per-call handshake already
	// declares this proxy's identity and protocol version to the upstream.
	upstreamResult, rpcErr, err := s.callProxiedMember(ctx, logger, dial, member, "tools/call", toolsCallParams{
		Name:      toolName,
		Arguments: arguments,
		Meta:      nil,
	})
	if err != nil {
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "call meta MCP member tool").LogError(ctx, logger)
	}
	if rpcErr != nil {
		return marshalMetaToolError(ctx, logger, req.ID,
			fmt.Sprintf("server %q rejected the call: %s", member.slug, rpcErr.Message))
	}
	if len(upstreamResult) == 0 {
		return marshalMetaToolError(ctx, logger, req.ID,
			fmt.Sprintf("server %q returned a response the meta MCP could not read", member.slug))
	}

	bs, err := json.Marshal(&result[json.RawMessage]{
		ID:     req.ID,
		Result: upstreamResult,
		// The result's _meta identity stays the member's, as on hosted.
		serverIdentity: serverInfo{Name: member.slug, Version: "0.0.0"},
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize member tool result").LogError(ctx, logger)
	}
	return bs, nil
}

// maxProxiedListPages bounds cursor-following on a member's tools/list.
const maxProxiedListPages = 8

// describeMetaMember reads one member's tool catalog through whichever
// path serves it: the hosted model view, or the member's own tools/list.
func (s *Service) describeMetaMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	if member.backend == metaMemberBackendHosted {
		return s.describeMemberToolset(ctx, logger, gate, member)
	}
	return s.describeProxiedMember(ctx, logger, gate, member)
}

// describeProxiedMember pages a proxied member's tools/list into a catalog,
// holding one upstream session across pages so session-scoped cursors stay
// valid. RBAC and session tool filtering already applied inside the proxy's
// tools/list interceptors.
func (s *Service) describeProxiedMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	ctx = s.memberAttributionContext(ctx, logger, gate)
	dial, err := s.dialMetaMember(ctx, logger, *gate, member, gate.callerIdentity())
	if err != nil {
		// Member-scoped errors stay detectable through the %w chain.
		return nil, fmt.Errorf("dial meta MCP member: %w", err)
	}

	// One deadline and one upstream session cover the whole pagination.
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()
	sess, err := s.openMemberSession(ctx, logger, dial, member)
	if err != nil {
		return nil, err
	}
	defer sess.close(ctx)

	entries := []*toolListEntry{}
	byName := map[string]*toolListEntry{}
	dropped := map[string]struct{}{}
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxProxiedListPages {
			logger.WarnContext(ctx, "meta MCP member tool listing truncated at the page cap", attr.SlogMcpServerID(member.serverID.String()))
			break
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		upstreamResult, rpcErr, cerr := sess.call(ctx, "tools/list", params)
		if cerr != nil {
			return nil, cerr
		}
		if rpcErr != nil {
			return nil, &metaMemberError{message: fmt.Sprintf("server %q rejected tools/list: %s", member.slug, rpcErr.Message)}
		}

		var listing struct {
			Tools      []*toolListEntry `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		}
		if uerr := json.Unmarshal(upstreamResult, &listing); uerr != nil {
			return nil, &metaMemberError{message: fmt.Sprintf("server %q returned a tool listing the meta MCP could not read", member.slug)}
		}
		for _, entry := range listing.Tools {
			if entry == nil || entry.Name == "" {
				continue
			}
			entries = append(entries, entry)
			// Duplicates drop entirely, matching the hosted catalog's rule.
			if _, gone := dropped[entry.Name]; gone {
				continue
			}
			if _, dup := byName[entry.Name]; dup {
				delete(byName, entry.Name)
				dropped[entry.Name] = struct{}{}
				continue
			}
			byName[entry.Name] = entry
		}
		if listing.NextCursor == "" || listing.NextCursor == cursor {
			break
		}
		cursor = listing.NextCursor
	}

	// Rebuild entries from the kept set, matching the hosted path's output.
	catalog := &memberCatalog{entries: make([]*toolListEntry, 0, len(byName)), byName: byName}
	for _, entry := range entries {
		if kept, ok := byName[entry.Name]; ok && kept == entry {
			catalog.entries = append(catalog.entries, entry)
		}
	}
	return catalog, nil
}
