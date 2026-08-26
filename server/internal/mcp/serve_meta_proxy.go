// Per-member upstream calls for the gateway runtime. Each call is one
// synthesized JSON-RPC exchange driven through the same proxy machinery a
// direct proxied endpoint uses (SSRF policy, billing, telemetry identity, and
// per-tool RBAC ride along via ProxyManager.BuildTarget), with the response
// captured and parsed by the gateway rather than relayed to the client.
//
// Stateless-first: the target request goes out with no upstream session; a
// session-required rejection triggers an inline initialize handshake and one
// replay, then a best-effort DELETE so per-call sessions do not accumulate.

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

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// metaMemberUpstreamProtocolVersion is the version the gateway speaks to
// member upstreams; independent of the version negotiated with the client.
const metaMemberUpstreamProtocolVersion = "2025-06-18"

// routeGatewayMemberToken selects the bearer forwarded to one gateway member,
// strictly: a remote member gets only a token whose recorded RFC 8707
// resource names its upstream. There is deliberately no lone-token fallback —
// unlike routeUpstreamToken's single-entry rule — because project mcp:write
// suffices to attach a member pointing anywhere, and a fallback would forward
// a sibling's credential there. No match means an anonymous call, never a
// mismatched bearer.
//
// A tunneled member records no resource, so only a lone unqualified token can
// be its credential; several tokens are unroutable and fail member-scoped.
func routeGatewayMemberToken(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember, upstreamResource string) (string, error) {
	if member.tunneledServerID.Valid {
		if len(tokens) > 1 {
			return "", &metaMemberError{message: fmt.Sprintf("server %q upstream credentials are not configured unambiguously for this gateway", member.slug)}
		}
		for _, entry := range tokens {
			if entry.Resource == "" {
				return entry.Token, nil
			}
		}
		return "", nil
	}

	want := strings.TrimRight(upstreamResource, "/")
	if want == "" {
		return "", nil
	}
	matched := ""
	found := 0
	for _, entry := range tokens {
		if strings.TrimRight(entry.Resource, "/") == want {
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
		return "", &metaMemberError{message: fmt.Sprintf("server %q upstream credentials are not configured unambiguously for this gateway", member.slug)}
	}
}

// metaMemberDial is one member's ready-to-call upstream: a proxy builder
// (fresh proxy per exchange, since a Proxy is a one-request value) plus the
// routed credential.
type metaMemberDial struct {
	build func() (*proxy.Proxy, error)
}

// dialGatewayMember loads the member's backend rows, routes its credential
// strictly, and returns a per-exchange proxy builder. The snapshot already
// enforced mcp:connect for private members with the same key
// authorizeProxyBackendAccess uses, so there is no second server-level check
// here; per-tool RBAC for private members attaches inside the proxy build.
func (s *Service) dialGatewayMember(
	ctx context.Context,
	logger *slog.Logger,
	gate metaGateContext,
	member metaMember,
	callerIdentity string,
) (*metaMemberDial, error) {
	serverRow, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        member.serverID,
		ProjectID: gate.projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load gateway member server: %w", err)
	}

	switch {
	case member.remoteServerID.Valid:
		remoteServer, rerr := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{
			ID:        member.remoteServerID.UUID,
			ProjectID: gate.projectID,
		})
		if rerr != nil {
			return nil, fmt.Errorf("load gateway member upstream: %w", rerr)
		}
		headers, herr := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, remoteServer.ID, false)
		if herr != nil {
			return nil, fmt.Errorf("load gateway member upstream headers: %w", herr)
		}
		upstreamToken, terr := routeGatewayMemberToken(gate.tokens, member, strings.TrimRight(remoteServer.Url, "/"))
		if terr != nil {
			return nil, terr
		}
		return &metaMemberDial{build: func() (*proxy.Proxy, error) {
			// No WWW-Authenticate relay: a member's auth challenge must not
			// invite the client to re-authenticate against the gateway.
			return s.remoteProxyManager.Build(logger, &remoteServer, member.serverID.String(), headers, member.visibility, gate.projectID.String(), upstreamToken, "", gate.toolSelection), nil
		}}, nil

	case member.tunneledServerID.Valid:
		upstreamToken, terr := routeGatewayMemberToken(gate.tokens, member, "")
		if terr != nil {
			return nil, terr
		}
		// Per-member namespace so one caller's handshake, replay, and DELETE
		// land on one tunnel gateway.
		affinity := tunnelrouting.HashedClientAffinityKey("meta:"+member.serverID.String(), callerIdentity)
		return &metaMemberDial{build: func() (*proxy.Proxy, error) {
			p, berr := s.tunnelManager.buildProxy(ctx, affinity, logger, gate.projectID, &serverRow, upstreamToken, "", gate.toolSelection)
			if berr != nil {
				return nil, fmt.Errorf("build tunnel proxy: %w", berr)
			}
			return p, nil
		}}, nil

	default:
		return nil, &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug)}
	}
}

// memberResponseRecorder captures a member upstream exchange instead of
// relaying it. Flush is a no-op the proxy's SSE relay requires; the byte cap
// turns an oversized member response into a member-scoped failure rather
// than unbounded buffering.
type memberResponseRecorder struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	truncated bool
	limit     int
}

func newMemberResponseRecorder() *memberResponseRecorder {
	return &memberResponseRecorder{
		header:    make(http.Header),
		status:    http.StatusOK,
		body:      bytes.Buffer{},
		truncated: false,
		limit:     metamcp.MaxMemberResponseBytes,
	}
}

func (r *memberResponseRecorder) Header() http.Header { return r.header }

func (r *memberResponseRecorder) WriteHeader(status int) { r.status = status }

func (r *memberResponseRecorder) Write(p []byte) (int, error) {
	remaining := r.limit - r.body.Len()
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

// callProxiedMember performs one JSON-RPC request against a proxied member
// and returns the upstream's result or error object. Every upstream-side
// failure comes back as *metaMemberError so callers degrade member-scoped.
func (s *Service) callProxiedMember(
	ctx context.Context,
	logger *slog.Logger,
	dial *metaMemberDial,
	member metaMember,
	method string,
	params any,
) (json.RawMessage, *upstreamRPCError, error) {
	ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
	defer cancel()

	requestID := mcpjsonrpc.StringID("gram-gateway-" + method)
	target, err := marshalUpstreamRequest(requestID, method, params)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal upstream %s request: %w", method, err)
	}

	rec, err := s.memberExchange(ctx, dial, member, target, "")
	if err != nil {
		return nil, nil, err
	}

	sessionID := ""
	if isSessionRequired(rec) {
		// Inline handshake: initialize, acknowledge, replay — all inside the
		// same member deadline.
		initBody, ierr := marshalUpstreamRequest(mcpjsonrpc.StringID("gram-gateway-init"), "initialize", map[string]any{
			"protocolVersion": metaMemberUpstreamProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "gram-gateway", "version": "1"},
		})
		if ierr != nil {
			return nil, nil, fmt.Errorf("marshal upstream initialize: %w", ierr)
		}
		initRec, ierr2 := s.memberExchange(ctx, dial, member, initBody, "")
		if ierr2 != nil {
			return nil, nil, ierr2
		}
		if initRec.status < http.StatusOK || initRec.status >= http.StatusMultipleChoices {
			return nil, nil, memberUpstreamFailure(member, initRec.status)
		}
		sessionID = initRec.header.Get(proxy.McpSessionIDHeader)
		if sessionID != "" {
			ackBody := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
			if _, aerr := s.memberExchange(ctx, dial, member, ackBody, sessionID); aerr != nil {
				return nil, nil, aerr
			}
			defer s.closeMemberSession(ctx, logger, dial, member, sessionID)
		}
		rec, err = s.memberExchange(ctx, dial, member, target, sessionID)
		if err != nil {
			return nil, nil, err
		}
	}

	if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
		return nil, nil, &metaMemberError{message: fmt.Sprintf("server %q requires authentication; connect it before calling its tools", member.slug)}
	}
	if rec.status < http.StatusOK || rec.status >= http.StatusMultipleChoices {
		return nil, nil, memberUpstreamFailure(member, rec.status)
	}
	if rec.truncated {
		return nil, nil, &metaMemberError{message: fmt.Sprintf("server %q returned a response too large for the gateway", member.slug)}
	}

	envelope, perr := parseUpstreamResponse(rec, requestID)
	if perr != nil {
		logger.WarnContext(ctx, "unparseable gateway member response", attr.SlogError(perr), attr.SlogMcpServerID(member.serverID.String()))
		return nil, nil, &metaMemberError{message: fmt.Sprintf("server %q returned a response the gateway could not read", member.slug)}
	}
	return envelope.Result, envelope.Error, nil
}

// memberExchange drives one HTTP exchange through a fresh member proxy.
func (s *Service) memberExchange(ctx context.Context, dial *metaMemberDial, member metaMember, body []byte, sessionID string) (*memberResponseRecorder, error) {
	p, err := dial.build()
	if err != nil {
		var memberErr *metaMemberError
		if errors.As(err, &memberErr) {
			return nil, err
		}
		// A tunnel with no live route is a member outage, not a gateway bug.
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
		// are the member's outage, not the gateway's.
		return nil, &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
	}
	return rec, nil
}

// closeMemberSession best-effort terminates a per-call upstream session.
func (s *Service) closeMemberSession(ctx context.Context, logger *slog.Logger, dial *metaMemberDial, member metaMember, sessionID string) {
	p, err := dial.build()
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/", nil)
	if err != nil {
		return
	}
	req.Header.Set(proxy.McpSessionIDHeader, sessionID)
	if err := serveProxyBackend(httptest.NewRecorder(), req, p); err != nil {
		logger.DebugContext(ctx, "close gateway member session", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	}
}

// isSessionRequired reports the SDK-shaped session-required rejection of a
// sessionless request: 400/404 before any session was presented.
func isSessionRequired(rec *memberResponseRecorder) bool {
	return rec.status == http.StatusBadRequest || rec.status == http.StatusNotFound
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

// parseUpstreamResponse extracts the response envelope matching requestID
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
			if envelope.ID.IsSet() && envelope.ID.Value() == requestID.Value() {
				return &envelope, nil
			}
		}
		return nil, fmt.Errorf("no matching response frame in event stream")
	}

	var envelope upstreamEnvelope
	if err := json.Unmarshal(rec.body.Bytes(), &envelope); err != nil {
		return nil, fmt.Errorf("decode upstream response: %w", err)
	}
	return &envelope, nil
}

// sseDataFrames concatenates each event's data lines per the SSE framing
// rules and returns one payload per event.
func sseDataFrames(body []byte) [][]byte {
	var frames [][]byte
	var current bytes.Buffer
	flush := func() {
		if current.Len() > 0 {
			frames = append(frames, append([]byte(nil), current.Bytes()...))
			current.Reset()
		}
	}
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			flush()
			continue
		}
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			current.Write(bytes.TrimPrefix(data, []byte(" ")))
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
// the unqualified tool name — qualification is a gateway concept — and
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
	dial, err := s.dialGatewayMember(ctx, logger, *gate, member, gate.callerIdentity())
	if err != nil {
		var memberErr *metaMemberError
		if errors.As(err, &memberErr) {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "dial gateway member").LogError(ctx, logger)
	}

	upstreamResult, rpcErr, err := s.callProxiedMember(ctx, logger, dial, member, "tools/call", toolsCallParams{
		Name:      toolName,
		Arguments: arguments,
		Meta:      meta,
	})
	if err != nil {
		var memberErr *metaMemberError
		if errors.As(err, &memberErr) {
			return marshalMetaToolError(ctx, logger, req.ID, memberErr.message)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "call gateway member tool").LogError(ctx, logger)
	}
	if rpcErr != nil {
		return marshalMetaToolError(ctx, logger, req.ID,
			fmt.Sprintf("server %q rejected the call: %s", member.slug, rpcErr.Message))
	}

	bs, err := json.Marshal(&result[json.RawMessage]{
		ID:             req.ID,
		Result:         upstreamResult,
		serverIdentity: serverInfoMetaServer,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize member tool result").LogError(ctx, logger)
	}
	return bs, nil
}

// maxProxiedListPages bounds cursor-following on a member's tools/list.
const maxProxiedListPages = 8

// describeGatewayMember reads one member's tool catalog through whichever
// path serves it: the hosted model view, or the member's own tools/list.
func (s *Service) describeGatewayMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	if member.backend == metaMemberBackendHosted {
		return s.describeMemberToolset(ctx, logger, gate, member)
	}
	return s.describeProxiedMember(ctx, logger, gate, member)
}

// describeProxiedMember pages a proxied member's tools/list into a catalog.
// RBAC and session tool filtering already applied inside the proxy's
// tools/list interceptors.
func (s *Service) describeProxiedMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) (*memberCatalog, error) {
	dial, err := s.dialGatewayMember(ctx, logger, *gate, member, gate.callerIdentity())
	if err != nil {
		var memberErr *metaMemberError
		if errors.As(err, &memberErr) {
			return nil, err
		}
		return nil, fmt.Errorf("dial gateway member: %w", err)
	}

	catalog := &memberCatalog{entries: nil, byName: map[string]*toolListEntry{}}
	dropped := map[string]struct{}{}
	cursor := ""
	for range maxProxiedListPages {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		upstreamResult, rpcErr, cerr := s.callProxiedMember(ctx, logger, dial, member, "tools/list", params)
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
			return nil, &metaMemberError{message: fmt.Sprintf("server %q returned a tool listing the gateway could not read", member.slug)}
		}
		for _, entry := range listing.Tools {
			if entry == nil || entry.Name == "" {
				continue
			}
			catalog.entries = append(catalog.entries, entry)
			// Duplicate names drop from byName so they fail deterministically,
			// matching the hosted catalog's rule.
			if _, gone := dropped[entry.Name]; gone {
				continue
			}
			if _, dup := catalog.byName[entry.Name]; dup {
				delete(catalog.byName, entry.Name)
				dropped[entry.Name] = struct{}{}
				continue
			}
			catalog.byName[entry.Name] = entry
		}
		if listing.NextCursor == "" || listing.NextCursor == cursor {
			return catalog, nil
		}
		cursor = listing.NextCursor
	}
	return catalog, nil
}
