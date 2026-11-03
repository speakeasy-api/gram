// Shared outbound MCP client for member dispatch and consent verification.
package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
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

// memberAttempt is the routing and outcome of one operation over a member
// connection. A reused session gets a new one per call through the request
// context, so concurrent calls never share a proxy build or a failure.
type memberAttempt struct {
	build memberProxyBuilder

	mu         sync.Mutex
	rejected   int
	lastStatus int
	// lastRetryAfter is the latest recorded exchange's Retry-After header.
	lastRetryAfter string
	// lastErr is the transport failure of the latest recorded exchange, if any.
	lastErr error
	// refused marks a tools/call the member itself answered with a JSON-RPC error.
	refused bool
	// sentCall marks a tools/call handed to the member's proxy. A call on a
	// session that had already failed never gets that far.
	sentCall bool
}

type memberAttemptKey struct{}

func newMemberAttempt(build memberProxyBuilder) *memberAttempt {
	return &memberAttempt{build: build, mu: sync.Mutex{}, rejected: 0, lastStatus: 0, lastRetryAfter: "", lastErr: nil, refused: false, sentCall: false}
}

func (a *memberAttempt) memberRefused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refused
}

// memberOutcome is what failure classification reads from an operation.
type memberOutcome interface {
	failure() error
	rejection() (int, bool)
	status() int
	retryAfter(now time.Time) time.Duration
}

// memberRoundTripper runs every SDK request through the member's proxy builder,
// so routing, tunnel affinity, configured headers, RBAC and visibility all
// apply, and records any authentication rejection the member answered with.
// Its own attempt covers the handshake and exchanges on the connection's context.
type memberRoundTripper struct {
	*memberAttempt

	// deadline bounds the operation; a legacy DELETE or a cancellation notice after it still gets closeFloor.
	deadline   time.Time
	closeFloor time.Duration

	sessionMu sync.Mutex
	// initialSessionID is captured even when the SDK rejects the response headers or body.
	initialSessionID string

	// deleteAttempted prevents fallback cleanup from repeating an SDK DELETE.
	deleteAttempted bool
}

// closeBudget bounds a session DELETE or a cancellation notice: the usual close
// timeout while the probe has that much left, never less than the floor once it
// has not.
func (rt *memberRoundTripper) closeBudget() time.Duration {
	return min(memberSessionCloseTimeout, max(time.Until(rt.deadline), 0)+rt.closeFloor)
}

func (rt *memberRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if req.Method == http.MethodDelete {
		rt.sessionMu.Lock()
		if req.Header.Get(proxy.McpSessionIDHeader) == rt.initialSessionID {
			rt.deleteAttempted = true
		}
		rt.sessionMu.Unlock()
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
	if rpc.Method == mcpversions.MethodNotificationsCancelled {
		// The SDK notifies on its own detached context after the operation's
		// deadline, so a member that never answers would hold it past the budget.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rt.closeBudget())
		defer cancel()
	}
	attempt := rt.memberAttempt
	if perCall, ok := ctx.Value(memberAttemptKey{}).(*memberAttempt); ok {
		attempt = perCall
	}
	// The SDK falls back to initialize after any server/discover failure, and
	// cleanup must not overwrite the outcome of the call, so neither is recorded.
	record := req.Method != http.MethodDelete && rpc.Method != "server/discover"

	p, err := attempt.build(ctx)
	if err != nil {
		err = fmt.Errorf("%w: %w", errMemberUnroutable, err)
		attempt.recordFailure(record, err)
		return nil, err
	}
	if capture, ok := ctx.Value(memberToolResultKey{}).(*memberToolResult); ok {
		p.ToolsCallResponseInterceptors = append(p.ToolsCallResponseInterceptors, capture)
	}
	if rpc.Method == "tools/call" {
		attempt.mu.Lock()
		attempt.sentCall = true
		attempt.mu.Unlock()
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
		rt.sessionMu.Lock()
		rt.initialSessionID = mintedSession
		rt.sessionMu.Unlock()
	}
	if rec.truncated {
		exchangeErr = errMemberResponseTooLarge
	}
	if exchangeErr != nil {
		attempt.recordFailure(record, exchangeErr)
		return nil, exchangeErr
	}
	attempt.mu.Lock()
	if record {
		attempt.lastStatus = rec.status
		attempt.lastRetryAfter = rec.header.Get("Retry-After")
		attempt.lastErr = nil
		if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
			attempt.rejected = rec.status
		}
	}
	attempt.mu.Unlock()
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
func (a *memberAttempt) recordFailure(record bool, err error) {
	if !record {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastStatus = 0
	a.lastRetryAfter = ""
	a.lastErr = err
}

func (a *memberAttempt) failure() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastErr
}

func (a *memberAttempt) rejection() (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rejected, a.rejected != 0
}

func (a *memberAttempt) status() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastStatus
}

// retryAfter is the latest exchange's Retry-After delay, as seconds or an HTTP
// date, or 0 when it sent none or an unreadable one. Delays past a whole
// listing's time are capped there, since no retry could wait that long.
func (a *memberAttempt) retryAfter(now time.Time) time.Duration {
	a.mu.Lock()
	value := strings.TrimSpace(a.lastRetryAfter)
	a.mu.Unlock()
	seconds, err := strconv.ParseUint(value, 10, 64)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return maxGatewayListingTime
	case err == nil && seconds > 0:
		return time.Duration(min(seconds, uint64(maxGatewayListingTime/time.Second))) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return min(at.Sub(now), maxGatewayListingTime)
	}
	return 0
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

// connectMetaMember opens an SDK connection with its own transport state; only
// a Code Mode execution shares one, across its own operations.
// The SDK owns negotiation and sessions; the proxy retains routing and enforcement.
func (s *Service) connectMetaMember(ctx context.Context, logger *slog.Logger, build memberProxyBuilder, closeFloor time.Duration) (*mcp.ClientSession, *memberRoundTripper, error) {
	deadline, _ := ctx.Deadline()
	rt := &memberRoundTripper{memberAttempt: newMemberAttempt(build), deadline: deadline, closeFloor: closeFloor, sessionMu: sync.Mutex{}, initialSessionID: "", deleteAttempted: false}
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
		rt.sessionMu.Lock()
		orphan := rt.initialSessionID
		if rt.deleteAttempted {
			orphan = ""
		}
		rt.deleteAttempted = true
		rt.sessionMu.Unlock()
		closeUnobservedMemberSession(ctx, logger, build, orphan, rt.closeBudget())
		return nil, rt, fmt.Errorf("connect meta MCP member: %w", err)
	}
	return session, rt, nil
}

func upstreamStatusOK(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// memberDiscoveryError is a member failure that leaves its tools unknown: its
// tool source did not answer successfully, or the gateway could not load what
// reaching it needs. Gateway discovery records it, and retries it briefly when
// retryable. Exclusions are not discovery errors and are left out unrecorded:
// visibility, consent, a member that is not servable, and a credential that is
// missing, rejected, misconfigured, or needs a new sign-in, all of which are
// fixed where the member or its sign-in is configured.
type memberDiscoveryError struct {
	*metaMemberError

	// status is the upstream HTTP status, or 0 when the upstream did not answer.
	status int

	// retryable marks a failure a short retry may clear: a transport failure,
	// rate limit, or server error. A timeout already spent the attempt's time.
	retryable bool

	// retryAfter is the delay the upstream asked for before a retry, or 0.
	retryAfter time.Duration
}

func (e memberDiscoveryError) Unwrap() error { return e.metaMemberError }

// memberClientFailure keeps transport failures scoped to a member and preserves
// the distinction between an absent credential and a rejected routed credential.
// Failures it cannot attribute to the member are logged, since they may be ours.
func memberClientFailure(ctx context.Context, logger *slog.Logger, rt memberOutcome, dial memberDial, member metaMember, err error) error {
	// The SDK need not wrap a transport error, so consult the recorded
	// exchange before reducing a credential failure to an upstream status.
	exchangeErr := rt.failure()
	if errors.Is(exchangeErr, remotesessions.ErrClientCredentialUnavailable) || errors.Is(exchangeErr, remotesessions.ErrClientCredentialMisconfigured) {
		failure := metaMemberClientCredentialError(member, exchangeErr)
		// This attempt's own exchange failed to renew the credential, and the
		// next attempt renews it again, so an outage is retryable unless the
		// attempt's time ran out or the token endpoint rate limited the
		// client, which the credential source remembers longer than a
		// listing lasts.
		if outage, ok := errors.AsType[memberDiscoveryError](failure); ok {
			tokenEndpoint, answered := errors.AsType[*remotesessions.TokenEndpointError](exchangeErr)
			rateLimited := answered && tokenEndpoint.StatusCode == http.StatusTooManyRequests
			outage.retryable = ctx.Err() == nil && !rateLimited && !errors.Is(exchangeErr, context.DeadlineExceeded) && !errors.Is(exchangeErr, context.Canceled)
			return outage
		}
		return failure
	}
	if _, rejected := rt.rejection(); rejected {
		return memberAuthFailure(member, dial)
	}
	status := rt.status()
	if upstreamStatusOK(status) {
		status = 0
	}
	// Rate limits and server errors retry whatever JSON-RPC body they carry.
	retryableStatus := status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
	upstreamFailure := func(message string, retryable bool) error {
		return memberDiscoveryError{metaMemberError: &metaMemberError{message: message}, status: status, retryable: retryable, retryAfter: rt.retryAfter(time.Now())}
	}
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return upstreamFailure(fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug), false)
	case errors.Is(err, errMemberResponseTooLarge) || errors.Is(exchangeErr, errMemberResponseTooLarge):
		return upstreamFailure(fmt.Sprintf("server %q returned a response too large for the meta MCP", member.slug), false)
	case errors.Is(err, errMemberUnroutable) || errors.Is(exchangeErr, errMemberUnroutable):
		// A tunnel with no live route is a member outage, not a meta MCP bug.
		return upstreamFailure(fmt.Sprintf("server %q is not reachable right now", member.slug), false)
	case exchangeErr != nil:
		// A transport failure may clear on a retry, unless it timed out, which
		// already spent the attempt's time, or no retry can change it: egress
		// policy refused the address, or the certificate failed verification.
		netErr, isNetErr := errors.AsType[net.Error](exchangeErr)
		spent := (isNetErr && netErr.Timeout()) || errors.Is(exchangeErr, context.DeadlineExceeded) || errors.Is(exchangeErr, context.Canceled)
		_, untrusted := errors.AsType[*tls.CertificateVerificationError](exchangeErr)
		refused := untrusted || errors.Is(exchangeErr, guardian.ErrBlockedIP) || errors.Is(exchangeErr, guardian.ErrBadHost)
		return upstreamFailure(fmt.Sprintf("server %q did not answer: upstream unreachable or timed out", member.slug), !spent && !refused)
	}
	if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok {
		return upstreamFailure(fmt.Sprintf("server %q rejected the call: %s", member.slug, rpcErr.Message), retryableStatus)
	}
	if status != 0 {
		return upstreamFailure(fmt.Sprintf("server %q upstream call failed with status %d", member.slug, status), retryableStatus)
	}
	logger.WarnContext(ctx, "unclassified meta MCP member failure", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	return upstreamFailure(fmt.Sprintf("server %q returned a response the meta MCP could not read", member.slug), false)
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
	// refusal is the member's own JSON-RPC error. It runs last, so an error a
	// proxy interceptor synthesized, including a withheld response after the
	// tool ran, never reaches it.
	refusal *jsonrpc.Error
}

func (*memberToolResult) Name() string { return "meta_member_result" }

func (c *memberToolResult) InterceptToolsCallResponse(_ context.Context, call *proxy.ToolsCallResponse) error {
	if call.RemoteMessage == nil || call.Request == nil || call.Request.UserRequest == nil || len(call.Request.UserRequest.JSONRPCMessages) != 1 {
		return nil
	}
	response, ok := call.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return nil
	}
	request, ok := call.Request.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok || response.ID.Raw() != request.ID.Raw() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if response.Error != nil {
		c.refusal, _ = errors.AsType[*jsonrpc.Error](response.Error)
		return nil
	}
	c.raw = bytes.Clone(response.Result)
	c.refusal = nil
	return nil
}

// callMemberTool lets the SDK validate the response and correlate the call,
// then returns the accepted raw result without a lossy typed round trip.
func callMemberTool(ctx context.Context, session *mcp.ClientSession, params *mcp.CallToolParams) (json.RawMessage, error) {
	if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok {
		invocation.dispatched = true
	}
	capture := &memberToolResult{mu: sync.Mutex{}, raw: nil, refusal: nil}
	_, err := session.CallTool(context.WithValue(ctx, memberToolResultKey{}, capture), params)
	if err != nil {
		capture.mu.Lock()
		refusal := capture.refusal
		capture.mu.Unlock()
		if attempt, ok := ctx.Value(memberAttemptKey{}).(*memberAttempt); ok {
			attempt.mu.Lock()
			attempt.refused = refusal != nil
			sent := attempt.sentCall
			attempt.mu.Unlock()
			if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok && !sent {
				invocation.dispatched = false
			}
		}
		// A member reporting an unknown tool or invalid arguments did not run it.
		if invocation, ok := ctx.Value(metaCodeInvocationKey{}).(*metaCodeInvocation); ok && refusal != nil &&
			(refusal.Code == jsonrpc.CodeInvalidParams || refusal.Code == jsonrpc.CodeMethodNotFound) {
			invocation.rejected = true
		}
		return nil, fmt.Errorf("call member tool: %w", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.raw) == 0 {
		return nil, errors.New("member tool response has no captured result")
	}
	return capture.raw, nil
}
