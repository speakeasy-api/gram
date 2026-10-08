// Shared outbound MCP client for member dispatch and consent verification.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// memberSessionCloseTimeout bounds best-effort legacy session cleanup after a call.
const memberSessionCloseTimeout = 5 * time.Second

// errMemberResponseTooLarge marks a member answer past MaxMemberResponseBytes.
var errMemberResponseTooLarge = errors.New("member response exceeds the meta MCP response cap")

// errMemberUnroutable marks a member the proxy could not build a route to, such
// as a tunnel with no live connection.
var errMemberUnroutable = errors.New("member is not reachable")

// memberRoundTripper runs every SDK request through the member's proxy builder,
// so routing, tunnel affinity, configured headers, RBAC and visibility all
// apply, and records any authentication rejection the member answered with.
type memberRoundTripper struct {
	build memberProxyBuilder
	// deadline bounds the operation; a legacy DELETE after it still gets closeFloor.
	deadline   time.Time
	closeFloor time.Duration

	mu         sync.Mutex
	rejected   int
	lastStatus int
	// lastErr is the transport failure of the latest recorded exchange, if any.
	lastErr error

	// initialSessionID is captured even when the SDK rejects the response headers or body.
	initialSessionID string

	// deleteAttempted prevents fallback cleanup from repeating an SDK DELETE.
	deleteAttempted bool
}

// closeBudget bounds a session DELETE: the usual close timeout while the probe
// has that much left, never less than the floor once it has not.
func (rt *memberRoundTripper) closeBudget() time.Duration {
	return min(memberSessionCloseTimeout, max(time.Until(rt.deadline), 0)+rt.closeFloor)
}

func (rt *memberRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if req.Method == http.MethodDelete {
		rt.mu.Lock()
		if req.Header.Get(proxy.McpSessionIDHeader) == rt.initialSessionID {
			rt.deleteAttempted = true
		}
		rt.mu.Unlock()
		// The SDK closes on its own detached context; the DELETE gets its own budget.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), rt.closeBudget())
		defer cancel()
	}
	var requestBody []byte
	if req.Body != nil {
		var err error
		requestBody, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read member request: %w", err)
		}
	}
	var rpc struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(requestBody, &rpc)
	// The SDK falls back to initialize after any server/discover failure, and
	// cleanup must not overwrite the outcome of the call, so neither is recorded.
	record := req.Method != http.MethodDelete && rpc.Method != "server/discover"

	p, err := rt.build(ctx)
	if err != nil {
		err = fmt.Errorf("%w: %w", errMemberUnroutable, err)
		rt.recordFailure(record, err)
		return nil, err
	}
	if capture, ok := ctx.Value(memberToolResultKey{}).(*memberToolResult); ok {
		p.ToolsCallResponseInterceptors = append(p.ToolsCallResponseInterceptors, capture)
	}
	out, err := http.NewRequestWithContext(ctx, req.Method, "/", bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("build member request: %w", err)
	}
	out.Header = req.Header.Clone()
	rec := newMemberResponseRecorder()
	// Capture the session before the body is buffered: a member can mint one
	// and then fail or hang the body, and the SDK never sees such an answer.
	mintedSession := ""
	interceptResponse := p.UpstreamResponseInterceptor
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		mintedSession = resp.Header.Get(proxy.McpSessionIDHeader)
		if interceptResponse != nil {
			return interceptResponse(ctx, resp)
		}
		return nil
	}
	exchangeErr := serveProxyBackend(rec, out, p)
	if rpc.Method == "initialize" && mintedSession != "" {
		rt.mu.Lock()
		rt.initialSessionID = mintedSession
		rt.mu.Unlock()
	}
	if rec.truncated {
		exchangeErr = errMemberResponseTooLarge
	}
	if exchangeErr != nil {
		rt.recordFailure(record, exchangeErr)
		return nil, exchangeErr
	}
	rt.mu.Lock()
	if record {
		rt.lastStatus = rec.status
		rt.lastErr = nil
		if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
			rt.rejected = rec.status
		}
	}
	rt.mu.Unlock()
	return &http.Response{
		Status:           fmt.Sprintf("%d %s", rec.status, http.StatusText(rec.status)),
		StatusCode:       rec.status,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Header:           rec.header,
		Body:             io.NopCloser(bytes.NewReader(rec.body.Bytes())),
		ContentLength:    int64(rec.body.Len()),
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          req,
		TLS:              nil,
	}, nil
}

// recordFailure marks the latest exchange as unanswered, so classification
// does not report the status of an earlier exchange for it.
func (rt *memberRoundTripper) recordFailure(record bool, err error) {
	if !record {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.lastStatus = 0
	rt.lastErr = err
}

func (rt *memberRoundTripper) failure() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.lastErr
}

func (rt *memberRoundTripper) rejection() (int, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.rejected, rt.rejected != 0
}

func (rt *memberRoundTripper) status() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.lastStatus
}

// closeUnobservedMemberSession best-effort terminates a session on a detached context
// bounded by budget alone, so a session minted just as the exchange's own
// deadline expired is still closed.
func closeUnobservedMemberSession(ctx context.Context, logger *slog.Logger, build memberProxyBuilder, sessionID string, budget time.Duration) {
	if sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()
	p, err := build(ctx)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/", nil)
	if err != nil {
		return
	}
	req.Header.Set(proxy.McpSessionIDHeader, sessionID)
	if err := serveProxyBackend(httptest.NewRecorder(), req, p); err != nil {
		logger.DebugContext(ctx, "close unobserved member session", attr.SlogError(err))
	}
}

// connectMetaMember gives each operation its own SDK connection and transport state.
// The SDK owns negotiation and sessions; the proxy retains routing and enforcement.
func (s *Service) connectMetaMember(ctx context.Context, logger *slog.Logger, build memberProxyBuilder, closeFloor time.Duration) (*mcp.ClientSession, *memberRoundTripper, error) {
	deadline, _ := ctx.Deadline()
	rt := &memberRoundTripper{build: build, deadline: deadline, closeFloor: closeFloor, mu: sync.Mutex{}, rejected: 0, lastStatus: 0, lastErr: nil, initialSessionID: "", deleteAttempted: false}
	client := mcp.NewClient(&mcp.Implementation{Name: "gram-gateway", Version: "1", Title: "", Description: "", WebsiteURL: "", Icons: nil}, nil)
	// Actual network requests remain inside the member proxy and its policy.
	// No retry layer is added here; the operation context bounds all calls.
	httpClient := s.guardianPolicy.Client()
	httpClient.Transport = rt
	transport := &memberClientTransport{
		StreamableClientTransport: &mcp.StreamableClientTransport{Endpoint: "http://member.invalid/", HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true, OAuthHandler: nil},
		connection:                nil,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		// SDK v1.7.0 does not close every failed Connect (for example an
		// unsupported initialize revision). Close the original connection to
		// release its goroutines and any session it learned about.
		if transport.connection != nil {
			if cerr := transport.connection.Close(); cerr != nil {
				logger.DebugContext(ctx, "close failed member connection", attr.SlogError(cerr))
			}
		}
		// HTTP errors and failed buffered bodies can hide the session from
		// the SDK. Only these unclaimed sessions need a fallback DELETE.
		rt.mu.Lock()
		orphan := rt.initialSessionID
		if rt.deleteAttempted {
			orphan = ""
		}
		rt.deleteAttempted = true
		rt.mu.Unlock()
		closeUnobservedMemberSession(ctx, logger, build, orphan, rt.closeBudget())
		return nil, rt, fmt.Errorf("connect meta MCP member: %w", err)
	}
	return session, rt, nil
}

func upstreamStatusOK(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// memberClientFailure keeps transport failures scoped to a member and preserves
// the distinction between an absent credential and a rejected routed credential.
// Failures it cannot attribute to the member are logged, since they may be ours.
func memberClientFailure(ctx context.Context, logger *slog.Logger, rt *memberRoundTripper, dial memberDial, member metaMember, err error) error {
	// The SDK need not wrap a transport error, so consult the recorded
	// exchange before reducing a credential failure to an upstream status.
	exchangeErr := rt.failure()
	if errors.Is(exchangeErr, remotesessions.ErrClientCredentialUnavailable) || errors.Is(exchangeErr, remotesessions.ErrClientCredentialMisconfigured) {
		return metaMemberClientCredentialError(member, exchangeErr)
	}
	if _, rejected := rt.rejection(); rejected {
		return memberAuthFailure(member, dial)
	}
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
	case errors.Is(err, errMemberResponseTooLarge) || errors.Is(exchangeErr, errMemberResponseTooLarge):
		return &metaMemberError{message: fmt.Sprintf("server %q returned a response too large for the meta MCP", member.slug)}
	case errors.Is(err, errMemberUnroutable) || errors.Is(exchangeErr, errMemberUnroutable):
		// A tunnel with no live route is a member outage, not a meta MCP bug.
		return &metaMemberError{message: fmt.Sprintf("server %q is not reachable right now", member.slug)}
	case exchangeErr != nil:
		return &metaMemberError{message: fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug)}
	}
	if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok {
		return &metaMemberError{message: fmt.Sprintf("server %q rejected the call: %s", member.slug, rpcErr.Message)}
	}
	if status := rt.status(); status != 0 && !upstreamStatusOK(status) {
		return &metaMemberError{message: fmt.Sprintf("server %q upstream call failed with status %d", member.slug, status)}
	}
	logger.WarnContext(ctx, "unclassified meta MCP member failure", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	return &metaMemberError{message: fmt.Sprintf("server %q returned a response the meta MCP could not read", member.slug)}
}

// memberClientTransport retains the original connection so failed negotiation
// can close it. Returning it unchanged preserves the SDK's private session-state
// notification interface, which a wrapper around Connection would hide.
type memberClientTransport struct {
	*mcp.StreamableClientTransport

	// connection is owned by the SDK session after a successful Connect.
	connection mcp.Connection
}

func (t *memberClientTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	connection, err := t.StreamableClientTransport.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("open member transport: %w", err)
	}
	t.connection = connection
	return connection, nil
}

// memberToolResultKey attaches a single call's capture to its HTTP exchanges.
type memberToolResultKey struct{}

// memberToolResult keeps the accepted wire result before SDK decoding converts
// arbitrary JSON numbers to float64. The proxy already handles JSON/SSE framing
// and runs this observer after its other tools/call response interceptors.
type memberToolResult struct {
	mu sync.Mutex

	// raw is the latest successful response matching its exchange's request ID.
	// The SDK's multi round-trip middleware re-sends tools/call on the same
	// context after an input_required answer, and CallTool returns only the
	// last attempt, so later exchanges replace earlier ones.
	raw json.RawMessage
}

func (*memberToolResult) Name() string { return "meta_member_result" }

func (c *memberToolResult) InterceptToolsCallResponse(_ context.Context, call *proxy.ToolsCallResponse) error {
	if call.RemoteMessage == nil || call.Request == nil || call.Request.UserRequest == nil || len(call.Request.UserRequest.JSONRPCMessages) != 1 {
		return nil
	}
	response, ok := call.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok || response.Error != nil {
		return nil
	}
	request, ok := call.Request.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok || response.ID.Raw() != request.ID.Raw() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = bytes.Clone(response.Result)
	return nil
}

// callMemberTool lets the SDK validate the response and correlate the call,
// then returns the accepted raw result without a lossy typed round trip.
func callMemberTool(ctx context.Context, session *mcp.ClientSession, params *mcp.CallToolParams) (json.RawMessage, error) {
	capture := &memberToolResult{mu: sync.Mutex{}, raw: nil}
	_, err := session.CallTool(context.WithValue(ctx, memberToolResultKey{}, capture), params)
	if err != nil {
		return nil, fmt.Errorf("call member tool: %w", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.raw) == 0 {
		return nil, errors.New("member tool response has no captured result")
	}
	return capture.raw, nil
}
