package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

const (
	// maxRequestBytes bounds any single request body.
	maxRequestBytes = 65536
	protocolVersion = mcpversions.Version20260728

	// metaKeyServerInfo is the _meta key MCP 2026-07-28 reserves for the
	// server's identity on every result of its stateless protocol.
	metaKeyServerInfo = "io.modelcontextprotocol/serverInfo"

	// resultTypeComplete is the resultType MCP 2026-07-28 requires on every
	// ordinary result. Channels has no multi round-trip requests.
	resultTypeComplete = "complete"
)

// SupportedProtocolVersions lists the MCP revisions the Channels endpoint
// serves, oldest first. server/discover reports it and the transport refuses
// any other declared version.
var SupportedProtocolVersions = []string{
	mcpversions.Version20241105,
	mcpversions.Version20250326,
	mcpversions.Version20250618,
	mcpversions.Version20251125,
	mcpversions.Version20260728,
}

// serverIdentity is the Channels server's name on initialize, server/discover
// and every result's _meta. It is display information only.
var serverIdentity = json.RawMessage(`{"name":"Speakeasy Channels","version":"1.0.0"}`)

// cacheableMethods are the methods MCP 2026-07-28 requires caching members on.
// Every Channels result is computed for an authenticated caller, so each one
// is marked private, and its zero TTL marks it immediately stale because
// nothing advertises a list-changed notification.
var cacheableMethods = map[string]bool{
	"server/discover": true,
	"tools/list":      true,
	"resources/list":  true,
	"resources/read":  true,
}

func (h *Handler) principal(r *http.Request) (Principal, error) {
	if h.authenticate == nil {
		return Principal{}, errors.New("authentication unavailable")
	}
	p, err := h.authenticate(r)
	if err != nil {
		return p, fmt.Errorf("authenticate channels request: %w", err)
	}
	if p.OrganizationID == "" || p.SubjectID == "" {
		return p, errors.New("organization and subject required")
	}
	return p, nil
}

// statusFor maps an operation outcome onto a status a client can act on: a
// rejected payload or an exhausted burst must not look like lost access,
// because clients blank protected content on 401 and 403.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, ErrLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrDenied):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) restResult(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	status := statusFor(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		h.logger.ErrorContext(r.Context(), "channels operation failed", attr.SlogError(err))
		message = "channels request failed"
	}
	writeJSON(w, status, map[string]any{"error": message})
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResult wraps a result in its JSON-RPC envelope and adds the members MCP
// 2026-07-28 defines on every result: resultType and the server identity under
// _meta, plus the caching members for a cacheable method. Earlier revisions
// permit extra result members, so they are emitted unconditionally. Existing
// resultType and _meta entries are kept.
func rpcResult(id json.RawMessage, method string, v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode channels result: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("channels result is not an object: %w", err)
	}
	if _, ok := fields["resultType"]; !ok {
		fields["resultType"] = json.RawMessage(strconv.Quote(resultTypeComplete))
	}
	var meta map[string]json.RawMessage
	if raw, ok := fields["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	if _, ok := meta[metaKeyServerInfo]; !ok {
		meta[metaKeyServerInfo] = serverIdentity
	}
	if fields["_meta"], err = json.Marshal(meta); err != nil {
		return nil, fmt.Errorf("encode channels result _meta: %w", err)
	}
	if cacheableMethods[method] {
		fields["ttlMs"] = json.RawMessage("0")
		fields["cacheScope"] = json.RawMessage(`"private"`)
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": fields}, nil
}

func rpcError(id json.RawMessage, code int, msg string) any {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

// isDashboardAPI reports whether the request arrived on the browser API, which
// authenticates with the operator's existing dashboard session.
func isDashboardAPI(path string) bool {
	return path == "/channels/api" || path == "/channels/api/"
}

func (h *Handler) execute(r *http.Request, op string, a arguments) (any, error) {
	p, err := h.principal(r)
	if err != nil {
		return nil, err
	}
	a.Surface = ""
	if isDashboardAPI(r.URL.Path) {
		a.Surface = surfaceDashboard
	}
	return h.operate(r.Context(), p, op, a)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, err := h.principal(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
		return
	}
	if isDashboardAPI(r.URL.Path) {
		h.serveDashboardAPI(w, r)
		return
	}
	if r.URL.Path != "/channels/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serveMCP(w, r, p)
}

func (h *Handler) serveDashboardAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, err := h.execute(r, "snapshot", noArguments)
		h.restResult(w, r, v, err)
	case http.MethodPost:
		var body struct {
			Operation string    `json:"operation"`
			Arguments arguments `json:"arguments"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		v, err := h.execute(r, body.Operation, body.Arguments)
		h.restResult(w, r, v, err)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) serveMCP(w http.ResponseWriter, r *http.Request, p Principal) {
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil || req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "invalid JSON-RPC request"))
		return
	}
	if req.Method == "notifications/cancelled" {
		h.cancelStream(p, req.Params)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if req.Method == "subscriptions/listen" {
		h.listen(w, r, p, req)
		return
	}
	result, err := h.callMCP(r, req)
	if errors.Is(err, errUnknownMethod) {
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method not found"))
		return
	}
	if err != nil {
		if statusFor(err) == http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), "channels MCP call failed", attr.SlogError(err))
			writeJSON(w, http.StatusOK, rpcError(req.ID, -32603, "channels request failed"))
			return
		}
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32000, err.Error()))
		return
	}
	envelope, err := rpcResult(req.ID, req.Method, result)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "channels MCP result encoding failed", attr.SlogError(err))
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32603, "channels request failed"))
		return
	}
	writeJSON(w, http.StatusOK, envelope)
}

var errUnknownMethod = errors.New("method not found")

func (h *Handler) callMCP(r *http.Request, req rpcRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo":      serverIdentity,
			"capabilities":    serverCapabilities(),
			"instructions":    serverInstructions,
		}, nil
	case "server/discover":
		return map[string]any{
			"supportedVersions": SupportedProtocolVersions,
			"capabilities":      serverCapabilities(),
			"instructions":      serverInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		var params struct {
			Name      string    `json:"name"`
			Arguments arguments `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("%w: invalid tool arguments", ErrInvalid)
		}
		v, err := h.execute(r, params.Name, params.Arguments)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode channels tool result: %w", err)
		}
		return map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": string(encoded)}},
			"structuredContent": v,
		}, nil
	case "resources/list":
		return map[string]any{"resources": []any{
			map[string]any{"uri": "channels://presence", "name": "Presence", "mimeType": "application/json"},
			map[string]any{"uri": "channels://threads", "name": "Threads", "mimeType": "application/json"},
			map[string]any{"uri": "channels://messages", "name": "Messages", "mimeType": "application/json"},
		}}, nil
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("%w: invalid resource parameters", ErrInvalid)
		}
		value, err := h.readResource(r, params.URI)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode channels resource: %w", err)
		}
		return map[string]any{"contents": []any{map[string]any{
			"uri":      params.URI,
			"mimeType": "application/json",
			"text":     string(encoded),
		}}}, nil
	default:
		return nil, errUnknownMethod
	}
}

func serverCapabilities() map[string]any {
	return map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}
}

func resourceField(uri string) string {
	switch uri {
	case "channels://presence":
		return "instances"
	case "channels://threads":
		return "threads"
	case "channels://messages":
		return "messages"
	default:
		return ""
	}
}

func (h *Handler) readResource(r *http.Request, uri string) (any, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid resource URI", ErrInvalid)
	}
	base := *parsed
	base.RawQuery = ""
	base.ForceQuery = false
	field := resourceField(base.String())
	if field == "" {
		return nil, fmt.Errorf("%w: unknown resource", ErrInvalid)
	}
	if field == "messages" {
		args, err := messageResourceArguments(parsed.RawQuery, h.limits.HistoryLimit)
		if err != nil {
			return nil, err
		}
		return h.execute(r, "channels_read", args)
	}
	if parsed.RawQuery != "" {
		return nil, fmt.Errorf("%w: query unsupported for this resource", ErrInvalid)
	}
	snapshot, err := h.execute(r, "snapshot", noArguments)
	if err != nil {
		return nil, err
	}
	view, ok := snapshot.(snapshotView)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected snapshot shape", ErrUnavailable)
	}
	if field == "instances" {
		return view.Instances, nil
	}
	return view.Threads, nil
}

func messageResourceArguments(rawQuery string, maxLimit int) (arguments, error) {
	var args arguments
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return args, fmt.Errorf("%w: invalid cursor query", ErrInvalid)
	}
	for key := range query {
		if key != "cursor" && key != "limit" && key != "threadId" {
			return args, fmt.Errorf("%w: unknown cursor query parameter", ErrInvalid)
		}
	}
	if query.Has("cursor") {
		args.Cursor, err = strconv.ParseInt(query.Get("cursor"), 10, 64)
		if err != nil || args.Cursor < 0 {
			return args, fmt.Errorf("%w: invalid cursor", ErrInvalid)
		}
	}
	if query.Has("limit") {
		args.Limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || args.Limit < 1 || args.Limit > maxLimit {
			return args, fmt.Errorf("%w: invalid limit", ErrInvalid)
		}
	}
	args.ThreadID = query.Get("threadId")
	return args, nil
}

// subscriptionTokenMeta carries the server-issued handle for a stream. A
// JSON-RPC request ID identifies a request on one connection, not a stream on
// a shared account, so it cannot be the identity of a long-lived subscription:
// two clients signed in as the same person both using request id 1 would
// collide, and one could cancel the other. The token is unguessable and names
// exactly one stream.
const subscriptionTokenMeta = "ai.speakeasy.channels/subscriptionToken"

// stream is one live subscription. requestID is kept for cancellation by the
// protocol's own mechanism, which is honored only when it is unambiguous.
type stream struct {
	cancel    context.CancelFunc
	requestID string
	subject   string
}

// cancelStream honors notifications/cancelled.
//
// The protocol addresses a stream by its original request ID, which this
// server cannot tie to a transport connection: there is no session. So a
// request ID cancels a stream only when the caller's identity has exactly one
// stream under it. When two do, the request is ambiguous and nothing is
// cancelled — terminating a sibling client's stream is worse than ignoring a
// cancellation, and both unambiguous routes remain open: close the HTTP
// stream, or name the stream with the token from its acknowledgment.
func (h *Handler) cancelStream(p Principal, params json.RawMessage) {
	var args struct {
		RequestID json.RawMessage `json:"requestId"`
		Meta      map[string]any  `json:"_meta"`
	}
	if json.Unmarshal(params, &args) != nil {
		return
	}
	subject := subjectKey(p)
	if token, ok := args.Meta[subscriptionTokenMeta].(string); ok && token != "" {
		h.mu.Lock()
		held, found := h.streams[token]
		h.mu.Unlock()
		// The token names a stream, but it still has to belong to the caller.
		if found && held.subject == subject {
			held.cancel()
		}
		return
	}
	requestID := string(args.RequestID)
	h.mu.Lock()
	var matched []*stream
	for _, held := range h.streams {
		if held.subject == subject && held.requestID == requestID {
			matched = append(matched, held)
		}
	}
	h.mu.Unlock()
	if len(matched) == 1 {
		matched[0].cancel()
	}
}

func subjectKey(p Principal) string {
	return p.OrganizationID + "\x00" + p.SubjectID
}

// registerStream admits one subscription under a server-issued token,
// enforcing both the per-handler and per-identity stream caps.
func (h *Handler) registerStream(token string, requestID json.RawMessage, p Principal, cancel context.CancelFunc) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("%w: channels is shutting down", ErrUnavailable)
	}
	if len(h.streams) >= h.limits.Streams {
		return nil, fmt.Errorf("%w: this server already holds %d subscriptions", ErrLimited, len(h.streams))
	}
	subject := subjectKey(p)
	if h.perSubject[subject] >= h.limits.StreamsPerSubject {
		return nil, fmt.Errorf("%w: at most %d concurrent subscriptions per identity", ErrLimited, h.limits.StreamsPerSubject)
	}
	h.streams[token] = &stream{cancel: cancel, requestID: string(requestID), subject: subject}
	h.perSubject[subject]++
	h.wg.Add(1)
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.streams, token)
		h.perSubject[subject]--
		if h.perSubject[subject] <= 0 {
			delete(h.perSubject, subject)
		}
		h.wg.Done()
	}, nil
}

func (h *Handler) listen(w http.ResponseWriter, r *http.Request, p Principal, req rpcRequest) {
	var params struct {
		Notifications struct {
			ResourceSubscriptions []string `json:"resourceSubscriptions"`
		} `json:"notifications"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) != nil {
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32602, "invalid subscription parameters"))
		return
	}
	wanted := map[string]bool{}
	for _, uri := range params.Notifications.ResourceSubscriptions {
		if resourceField(uri) == "" {
			writeJSON(w, http.StatusOK, rpcError(req.ID, -32602, "unknown resource"))
			return
		}
		wanted[uri] = true
	}
	// Admission runs the same live authorization every later reconciliation
	// does; a subscription never grants access by existing.
	baseline, err := h.revisions(r.Context(), p, wanted, 0)
	if err != nil {
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32000, err.Error()))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, rpcError(req.ID, -32603, "streaming unavailable"))
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	token := identifier()
	release, err := h.registerStream(token, req.ID, p, cancel)
	if err != nil {
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32000, err.Error()))
		return
	}
	defer release()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(v any) bool {
		encoded, err := json.Marshal(v)
		if err != nil {
			return false
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, writeErr := fmt.Fprintf(w, "event: message\ndata: %s\n\n", encoded)
		flusher.Flush()
		return writeErr == nil
	}
	honored := map[string]any{}
	if params.Notifications.ResourceSubscriptions != nil {
		honored["resourceSubscriptions"] = params.Notifications.ResourceSubscriptions
	}
	// The protocol's subscription ID is echoed unchanged. The token beside it
	// is this server's own handle for the stream, and is what makes
	// cancellation unambiguous when one account holds several.
	meta := map[string]any{
		"io.modelcontextprotocol/subscriptionId": req.ID,
		subscriptionTokenMeta:                    token,
	}
	if !send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/subscriptions/acknowledged",
		"params":  map[string]any{"notifications": honored, "_meta": meta},
	}) {
		return
	}
	complete := func() {
		envelope, err := rpcResult(req.ID, req.Method, map[string]any{"_meta": meta})
		if err != nil {
			return
		}
		_ = send(envelope)
	}

	wake, unsubscribe := h.events.subscribe(p.OrganizationID)
	defer unsubscribe()
	lifetime := time.NewTimer(h.limits.StreamLifetime)
	defer lifetime.Stop()
	// Hints carry latency; this interval is the safety net that recovers a
	// lost hint and rechecks live authorization.
	ticker := time.NewTicker(h.limits.ReconcileInterval)
	defer ticker.Stop()
	current := baseline
	for {
		select {
		case <-lifetime.C:
			complete()
			return
		case <-ctx.Done():
			complete()
			return
		case <-wake:
			if !h.settle(ctx) {
				complete()
				return
			}
		case <-ticker.C:
		}
		live, err := h.principal(r)
		if err != nil || live.OrganizationID != p.OrganizationID || live.SubjectID != p.SubjectID {
			complete()
			return
		}
		next, err := h.revisions(ctx, live, wanted, current.messageSeq)
		if err != nil {
			complete()
			return
		}
		for _, uri := range params.Notifications.ResourceSubscriptions {
			if !current.changed(next, uri) {
				continue
			}
			if !send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/resources/updated",
				"params":  map[string]any{"uri": uri, "_meta": meta},
			}) {
				return
			}
		}
		current = next
	}
}

// settle coalesces a burst of hints into a single reconciliation.
func (h *Handler) settle(ctx context.Context) bool {
	timer := time.NewTimer(h.limits.HintDebounce)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
