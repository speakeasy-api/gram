// Package proxy forwards MCP client requests to an upstream Remote MCP
// Server and relays its responses back to the client. It implements the
// minimum surface required by the Model Context Protocol's Streamable HTTP
// transport (DELETE, GET, and POST on a single endpoint path) while exposing
// extension points for request/response inspection via interceptors.
//
// The proxy is intentionally transport-focused: callers are responsible for
// authenticating the client, loading the RemoteMcpServer configuration, and
// decrypting any secret header values before handing the resolved data to the
// proxy.
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// DefaultNonStreamingTimeout bounds the connect+headers phase for every
	// upstream request, plus the body read for non-streaming responses. The
	// MCP spec only mandates that implementations establish timeouts; it
	// does not prescribe a value, so this matches the 60s default used by
	// common MCP SDK implementations.
	//
	// For streaming responses (text/event-stream), this only bounds the
	// connect+headers phase — once headers are received, per-event idle
	// bounds via [DefaultStreamingTimeout] take over and the stream itself
	// is unbounded so long as upstream stays active.
	DefaultNonStreamingTimeout = 60 * time.Second

	// DefaultStreamingTimeout is the per-event idle bound applied to
	// streaming response bodies. The clock resets on every successful Read
	// from the upstream body, so a stream that's actively producing events
	// stays alive indefinitely; only inactivity longer than this duration
	// terminates the stream. Activity here is byte-level, so SSE keepalive
	// comments (`: keepalive\n`) reset the clock too — operators can keep
	// streams alive across long tool-call wait periods by sending
	// keepalives.
	DefaultStreamingTimeout = 60 * time.Second

	// DefaultMaxBufferedBodyBytes bounds the size of a JSON body that is
	// fully read into memory before parsing (via [io.ReadAll]) on both the
	// inbound user request and the upstream response. It exists to prevent
	// a misbehaving peer from sending arbitrarily large JSON bodies that
	// would exhaust server memory during parse.
	//
	// Streamed responses (Content-Type: text/event-stream) are not subject
	// to this cap — their bytes flow through bounded read/write buffers and
	// never materialize in a single allocation. Stream duration is bounded
	// by the request context deadline, not by this value.
	//
	// Set generously — large tool results (long-form text, base64-encoded
	// payloads) legitimately exceed a few MB — but bounded to keep parse
	// allocations predictable. Overridden per-proxy via
	// [Proxy.MaxBufferedBodyBytes].
	DefaultMaxBufferedBodyBytes int64 = 50 * 1024 * 1024
)

// Proxy is a one-request handler that forwards inbound MCP client requests
// to a configured Remote MCP Server. A fresh value is expected per inbound
// request so the SessionID and interceptor state stay tied to a single
// client exchange.
type Proxy struct {
	// GuardianPolicy is used to build a fresh, non-pooled HTTP client per
	// upstream request. Pooling is inappropriate here because each Proxy
	// instance handles a single upstream host and discards the connection
	// when the request finishes; a pooled transport would accumulate idle
	// connections across distinct Remote MCP Servers without ever reusing
	// them.
	GuardianPolicy *guardian.Policy
	Logger         *slog.Logger
	Tracer         trace.Tracer

	// NonStreamingTimeout bounds the connect+headers phase for every
	// upstream request, plus the body read for non-streaming responses.
	// For streaming (text/event-stream) responses this only bounds the
	// connect+headers phase; per-event idle bounds via StreamingTimeout
	// take over once headers are received. Callers must set an explicit
	// value; use [DefaultNonStreamingTimeout] for the package default.
	NonStreamingTimeout time.Duration

	// StreamingTimeout is the per-event idle bound applied to streaming
	// response bodies. The clock resets on every successful Read from
	// upstream, so an actively producing stream stays alive indefinitely
	// — only inactivity longer than this duration terminates the stream.
	// Callers must set an explicit value; use [DefaultStreamingTimeout]
	// for the package default.
	StreamingTimeout time.Duration

	// Metrics records per-request counters and histograms. Nil is safe and
	// disables metrics recording; tests that do not care about metrics pass
	// nil here.
	Metrics *Metrics
	// MaxBufferedBodyBytes bounds the size of any JSON body that is fully
	// read into memory before parsing — applied to both the inbound user
	// request and the upstream response. Streamed responses are not subject
	// to this cap; see [DefaultMaxBufferedBodyBytes] for the rationale.
	// Callers must set an explicit value; use [DefaultMaxBufferedBodyBytes]
	// for the package default.
	MaxBufferedBodyBytes int64

	// ServerID is the Remote MCP Server UUID. When set, it is attached to
	// every span emitted by the proxy so traces can be correlated across the
	// HTTP lifecycle and the proxy forward.
	ServerID string
	// RemoteURL is the upstream endpoint all requests are forwarded to.
	RemoteURL string
	// Headers are applied on top of any forwarded client headers when
	// constructing the upstream request.
	Headers []ConfiguredHeader

	// AuthorizationOverride is the Bearer token to set on the outgoing
	// Authorization header. The caller's incoming Authorization is
	// always dropped (Gram-issued credentials — API keys, OAuth tokens,
	// chat-session JWTs — are not meaningful upstream); when this field
	// is non-empty the proxy emits "Authorization: Bearer <override>"
	// instead. Use it for two flows:
	//
	//   - External OAuth: forward the caller's Bearer verbatim by
	//     setting this to the caller's own token (the upstream MCP
	//     server is the AS).
	//   - OAuth-proxy token swap: set this to a stored upstream
	//     credential resolved from the caller's Gram-issued OAuth
	//     token.
	//
	// Leave empty (default) to send no Authorization upstream.
	AuthorizationOverride string

	UserRequestInterceptors []UserRequestInterceptor

	// RemoteMessageInterceptors run for each JSON-RPC message arriving
	// from the remote MCP server: once per application/json POST response,
	// and once per parseable SSE event in a streamed response. Returning
	// a non-nil error blocks the message from being relayed to the user;
	// see [RemoteMessageInterceptor] for transport-specific rejection
	// semantics.
	RemoteMessageInterceptors []RemoteMessageInterceptor

	// ToolsCallRequestInterceptors run for inbound "tools/call" JSON-RPC
	// requests only, after the generic UserRequestInterceptors chain has
	// completed. Non-tools/call requests skip this loop entirely.
	ToolsCallRequestInterceptors []ToolsCallRequestInterceptor

	// ToolsCallResponseInterceptors run for "tools/call" JSON-RPC responses
	// only, after the generic RemoteMessageInterceptors chain has
	// completed. Dispatches from either the JSON response path or — for
	// SSE responses — when a terminal response event matching the
	// originating request ID is seen. Responses to non-tools/call requests
	// skip this loop entirely.
	ToolsCallResponseInterceptors []ToolsCallResponseInterceptor

	// ToolsListRequestInterceptors run for inbound "tools/list" JSON-RPC
	// requests only, after the generic UserRequestInterceptors chain has
	// completed. Non-tools/list requests skip this loop entirely.
	ToolsListRequestInterceptors []ToolsListRequestInterceptor

	// ToolsListResponseInterceptors run for "tools/list" JSON-RPC responses
	// only, after the generic RemoteMessageInterceptors chain has
	// completed. Dispatches from either the JSON response path or — for
	// SSE responses — when a terminal response event matching the
	// originating request ID is seen. Responses to non-tools/list requests
	// skip this loop entirely.
	ToolsListResponseInterceptors []ToolsListResponseInterceptor
}

// Delete forwards an inbound DELETE to the remote MCP server. In MCP's
// Streamable HTTP transport, DELETE is used by clients to explicitly
// terminate a session identified by Mcp-Session-Id (see spec § Session
// Management).
func (p *Proxy) Delete(w http.ResponseWriter, r *http.Request) (err error) {
	start := time.Now()
	ctx, span := p.Tracer.Start(r.Context(), "remotemcp.proxy.Delete", trace.WithAttributes(p.requestSpanAttributes(http.MethodDelete)...))
	defer span.End()
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
		}
	}()

	var (
		upstreamStatus int
		responseBytes  int64
	)
	defer func() {
		p.Metrics.Record(ctx, p.ServerID, http.MethodDelete, upstreamStatus, responseBytes, time.Since(start))
	}()

	//nolint:bodyclose // Body is closed via the defer below; linter can't trace the close across the forwardRequest helper.
	_, upstreamResp, err := p.forwardRequest(ctx, r, http.NoBody)
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(upstreamResp.Body.Close)

	upstreamStatus = upstreamResp.StatusCode
	span.SetAttributes(attr.RemoteMCPProxyRemoteStatusCode(upstreamStatus))

	n, err := writeResponse(w, upstreamResp, upstreamResp.Body)
	responseBytes = n
	if err != nil {
		return err
	}
	return nil
}

// Get forwards an inbound GET to the remote MCP server. In MCP's Streamable
// HTTP transport, GET is used by clients to open a Server-Sent Events stream
// so the server can push requests and notifications unprompted (see spec
// § Listening for Messages from the Server). The response body is streamed
// through with a flush after each chunk so SSE events reach the client without
// being buffered to EOF.
//
// Per the spec, upstream MUST respond with either Content-Type:
// text/event-stream or HTTP 405 Method Not Allowed. The text/event-stream
// path goes through [Proxy.relaySSEStream] for per-event interceptor
// dispatch; any other response (the spec'd 405, or non-conformant upstream
// behavior) is relayed verbatim via [writeResponse] so the user's MCP
// runtime sees what upstream actually said.
func (p *Proxy) Get(w http.ResponseWriter, r *http.Request) (err error) {
	start := time.Now()
	ctx, span := p.Tracer.Start(r.Context(), "remotemcp.proxy.Get", trace.WithAttributes(p.requestSpanAttributes(http.MethodGet)...))
	defer span.End()
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
		}
	}()

	var (
		upstreamStatus int
		responseBytes  int64
	)
	defer func() {
		p.Metrics.Record(ctx, p.ServerID, http.MethodGet, upstreamStatus, responseBytes, time.Since(start))
	}()

	//nolint:bodyclose // Body is closed via the defer below; linter can't trace the close across the forwardRequest helper.
	upstreamReq, upstreamResp, err := p.forwardRequest(ctx, r, http.NoBody)
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(upstreamResp.Body.Close)

	upstreamStatus = upstreamResp.StatusCode
	span.SetAttributes(attr.RemoteMCPProxyRemoteStatusCode(upstreamStatus))

	// Per MCP spec § Listening for Messages from the Server, upstream MUST
	// return either Content-Type: text/event-stream or HTTP 405. Route SSE
	// responses through the per-event relay so RemoteMessageInterceptors
	// fire for server-initiated requests and notifications; relay any
	// non-SSE response (spec'd 405, or non-conformant body) verbatim so
	// the user's MCP runtime sees upstream's actual response instead of
	// silently misparsing it as an SSE stream.
	if isEventStream(upstreamResp.Header) {
		n, streamErr := p.relaySSEStream(ctx, w, r, upstreamReq, upstreamResp, nil, nil)
		responseBytes = n
		if streamErr != nil {
			return streamErr
		}
		return nil
	}

	n, err := writeResponse(w, upstreamResp, upstreamResp.Body)
	responseBytes = n
	if err != nil {
		return err
	}
	return nil
}

// Post forwards an inbound POST to the remote MCP server, running any
// configured interceptors before the forward and after the response returns.
// POST is the primary MCP method — every JSON-RPC message sent by the client
// is a POST to the MCP endpoint (see spec § Sending Messages to the Server).
func (p *Proxy) Post(w http.ResponseWriter, r *http.Request) (err error) {
	start := time.Now()
	ctx, span := p.Tracer.Start(r.Context(), "remotemcp.proxy.Post", trace.WithAttributes(p.requestSpanAttributes(http.MethodPost)...))
	defer span.End()
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
		}
	}()

	var (
		upstreamStatus int
		responseBytes  int64
	)
	defer func() {
		p.Metrics.Record(ctx, p.ServerID, http.MethodPost, upstreamStatus, responseBytes, time.Since(start))
	}()

	userReq := &UserRequest{UserHTTPRequest: r, JSONRPCMessages: nil, body: nil}
	if parseErr := userReq.ParseJSONRPCMessages(p.MaxBufferedBodyBytes); parseErr != nil {
		return oops.E(oops.CodeBadRequest, parseErr, "invalid jsonrpc request").Log(ctx, p.Logger)
	}

	// Extract the originating request id once. Used as the correlation id
	// on any rejection envelope written back to the user — invalid id
	// (notification) leads writeRejection to omit the field and use HTTP
	// 4xx per MCP spec.
	//
	// NOTE: the length-1 guard reflects MCP 2025-06-18 Streamable HTTP
	// transport, which carries exactly one JSON-RPC message per POST. If
	// batched requests ever return to the spec, this needs to extract
	// per-message ids and writeRejection needs a batch-aware overload.
	var userReqID jsonrpc.ID
	if len(userReq.JSONRPCMessages) == 1 {
		if rpcReq, ok := userReq.JSONRPCMessages[0].(*jsonrpc.Request); ok {
			userReqID = rpcReq.ID
		}
	}

	if err := p.runUserRequestInterceptors(ctx, userReq); err != nil {
		n := p.writeRejection(ctx, w, span, userReqID, err)
		responseBytes = n
		return nil
	}

	// Typed per-RPC dispatch runs after the generic chain so generic
	// observability (audit logs, request counters) covers every request
	// even when a typed interceptor rejects it. toolsCallReq and
	// toolsListReq are nil for any request whose method does not match —
	// the corresponding typed loop is skipped in that case. At most one of
	// the two is non-nil for a given request.
	toolsCallReq, _ := toolsCallRequestFromUserRequest(userReq)
	if toolsCallReq != nil {
		if err := p.runToolsCallRequestInterceptors(ctx, toolsCallReq); err != nil {
			n := p.writeRejection(ctx, w, span, userReqID, err)
			responseBytes = n
			return nil
		}
	}

	toolsListReq, _ := toolsListRequestFromUserRequest(userReq)
	if toolsListReq != nil {
		if err := p.runToolsListRequestInterceptors(ctx, toolsListReq); err != nil {
			n := p.writeRejection(ctx, w, span, userReqID, err)
			responseBytes = n
			return nil
		}
	}

	//nolint:bodyclose // Body is closed via the defer below; linter can't trace the close across the forwardRequest helper.
	upstreamReq, upstreamResp, err := p.forwardRequest(ctx, r, userReq.BodyReader())
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(upstreamResp.Body.Close)

	upstreamStatus = upstreamResp.StatusCode
	span.SetAttributes(attr.RemoteMCPProxyRemoteStatusCode(upstreamStatus))

	// When upstream returns Content-Type: text/event-stream (per MCP spec
	// § Sending Messages to the Server, step 5), dispatch through the
	// SSE-aware relay where RemoteMessageInterceptors fire per parseable
	// event and typed tools/call response interceptors fire on the
	// terminal response event. The buffered JSON-path interceptor chain
	// below is bypassed entirely for SSE responses because the body is
	// not a single message to hand off — it's a stream of them.
	if isEventStream(upstreamResp.Header) {
		n, streamErr := p.relaySSEStream(ctx, w, r, upstreamReq, upstreamResp, toolsCallReq, toolsListReq)
		responseBytes = n
		if streamErr != nil {
			return streamErr
		}
		return nil
	}

	bodyBytes, msg, err := readJSONRPCBody(upstreamResp.Body, p.MaxBufferedBodyBytes)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid jsonrpc response from remote mcp server").Log(ctx, p.Logger)
	}

	// Empty bodies skip interceptor invocation but still relay through to
	// the client (preserves status-only responses).
	if msg != nil {
		remoteMsg := &RemoteMessage{
			UserHTTPRequest:    r,
			RemoteHTTPRequest:  upstreamReq,
			RemoteHTTPResponse: upstreamResp,
			Message:            msg,
		}

		if err := p.runRemoteMessageInterceptors(ctx, remoteMsg); err != nil {
			n := p.writeRejection(ctx, w, span, userReqID, err)
			responseBytes = n
			return nil
		}

		// Typed response dispatch runs after the generic chain, symmetric
		// with the request side. Only runs when the originating request
		// was a typed method (tools/call or tools/list) AND the upstream
		// response decoded into a typed view.
		if toolsCallReq != nil {
			if toolsCallResp, ok := toolsCallResponseFromRemoteMessage(toolsCallReq, remoteMsg); ok {
				if err := p.runToolsCallResponseInterceptors(ctx, toolsCallResp); err != nil {
					n := p.writeRejection(ctx, w, span, userReqID, err)
					responseBytes = n
					return nil
				}
			}
		}

		if toolsListReq != nil {
			if toolsListResp, ok := toolsListResponseFromRemoteMessage(toolsListReq, remoteMsg); ok {
				if err := p.runToolsListResponseInterceptors(ctx, toolsListResp); err != nil {
					n := p.writeRejection(ctx, w, span, userReqID, err)
					responseBytes = n
					return nil
				}
			}
		}
	}

	n, err := writeResponse(w, upstreamResp, bytes.NewReader(bodyBytes))
	responseBytes = n
	if err != nil {
		return err
	}
	return nil
}

// forwardRequest builds and sends the upstream HTTP request, applying a
// two-phase timeout policy:
//
//   - Phase 1 (connect + headers): bounded by [Proxy.NonStreamingTimeout]
//     via a [time.AfterFunc] that cancels the request's context if the
//     headers don't arrive in time. Tracked via a timer rather than
//     [context.WithTimeout] so that, once headers arrive and we know
//     whether the response is streaming, we can stop the timer without
//     cancelling the body.
//
//   - Phase 2 (body): for non-streaming responses the timer is reset to
//     bound the body read; for streaming responses the timer is stopped
//     and a [streamReader] enforces a per-event idle bound via
//     [Proxy.StreamingTimeout] instead, leaving the stream's total
//     duration unbounded so legitimate long-running tool calls aren't
//     cut off mid-stream.
//
// Body close (via [cancellingBody] for non-streaming or [streamReader]
// for streaming) calls the request-context cancel and stops the phase
// timer, releasing all resources.
//
// The returned [*http.Request] is the outbound request as built by the proxy
// — populated with the configured remote URL, the headers we applied via
// [Proxy.applyRequestHeaders], and the request body. It is returned alongside
// the response so callers can hand it to interceptors as
// [RemoteMessage.RemoteHTTPRequest]. See the field doc for why this is
// preferable to relying on [http.Response.Request].
func (p *Proxy) forwardRequest(ctx context.Context, r *http.Request, body io.Reader) (*http.Request, *http.Response, error) {
	forwardCtx, forwardCancel := context.WithCancel(ctx)
	phaseTimer := time.AfterFunc(p.NonStreamingTimeout, forwardCancel)

	upstreamReq, err := http.NewRequestWithContext(forwardCtx, r.Method, p.RemoteURL, body)
	if err != nil {
		phaseTimer.Stop()
		forwardCancel()
		return nil, nil, oops.E(oops.CodeUnexpected, err, "build upstream request").Log(ctx, p.Logger)
	}

	if err := p.applyRequestHeaders(ctx, r, upstreamReq); err != nil {
		phaseTimer.Stop()
		forwardCancel()
		return nil, nil, err
	}

	resp, err := p.GuardianPolicy.Client().Do(upstreamReq)
	if err != nil {
		// timer.Stop() returns false if the timer has already fired;
		// that's how we distinguish a phase-1 timeout from a parent
		// cancellation when both surface as the same context error
		// inside the http.Client error chain.
		timedOut := !phaseTimer.Stop()
		forwardCancel()
		return nil, nil, p.classifyForwardError(ctx, err, timedOut)
	}

	// Atomically transition out of the headers-phase window. Stop returning
	// false means the AfterFunc has already started — forwardCancel was
	// (or is being) called, and any body Read on resp would race with the
	// cancellation and surface as an opaque context error. Close the body,
	// release the context, and return a clean phase-1 timeout instead.
	if !phaseTimer.Stop() {
		_ = resp.Body.Close()
		forwardCancel()
		return nil, nil, p.classifyForwardError(ctx, context.DeadlineExceeded, true)
	}

	if isEventStream(resp.Header) {
		// Streaming response: hand body lifetime over to streamReader's
		// per-event idle bound. The headers-phase timer is already stopped.
		resp.Body = newStreamReader(resp.Body, p.StreamingTimeout, forwardCancel)
	} else {
		// Non-streaming response: re-arm the timer to bound body read with
		// a fresh NonStreamingTimeout window; cancellingBody stops the
		// timer and the request context when the body is closed.
		phaseTimer.Reset(p.NonStreamingTimeout)
		resp.Body = &cancellingBody{ReadCloser: resp.Body, cancel: forwardCancel, timer: phaseTimer}
	}

	return upstreamReq, resp, nil
}

// relaySSEStream parses Server-Sent Events from the upstream body, relays
// each event to the user with a per-event flush, and fires per-event
// interceptors (generic plus typed dispatch when applicable). Use this for
// GET responses and for POST responses where upstream returned
// text/event-stream.
//
// Interceptors run after each event is flushed so real-time streaming to
// the client is preserved. Their errors are logged and span-marked but do
// not abort the stream — headers are already sent by the time the first
// event is emitted, and an inconsistent mid-stream close would deliver a
// truncated stream to the client.
//
// Events whose data fails to decode as a JSON-RPC message are still
// relayed verbatim but are skipped for interceptor invocation — the
// inspection layer operates on protocol-level messages, not raw SSE.
//
// Per-event size is capped by MaxBufferedBodyBytes; an oversized event
// trips [ErrBodyTooLarge]. Stream duration is bounded by the request
// context deadline.
func (p *Proxy) relaySSEStream(
	ctx context.Context,
	w http.ResponseWriter,
	userReq *http.Request,
	remoteReq *http.Request,
	upstreamResp *http.Response,
	toolsCallReq *ToolsCallRequest,
	toolsListReq *ToolsListRequest,
) (int64, error) {
	applyResponseHeaders(w, upstreamResp)
	w.WriteHeader(upstreamResp.StatusCode)

	if upstreamResp.Body == nil {
		return 0, nil
	}

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		// Flush headers immediately so the client can start parsing the SSE
		// stream before any event body arrives.
		flusher.Flush()
	}

	// Extract the request ID of the originating typed request once, so we
	// can match it against response events to detect the terminal event.
	// At most one of toolsCallReq and toolsListReq is non-nil for a given
	// inbound POST.
	var terminalID jsonrpc.ID
	var haveTerminalID bool
	switch {
	case toolsCallReq != nil && len(toolsCallReq.UserRequest.JSONRPCMessages) == 1:
		if rpcReq, ok := toolsCallReq.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request); ok {
			terminalID = rpcReq.ID
			haveTerminalID = true
		}
	case toolsListReq != nil && len(toolsListReq.UserRequest.JSONRPCMessages) == 1:
		if rpcReq, ok := toolsListReq.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request); ok {
			terminalID = rpcReq.ID
			haveTerminalID = true
		}
	}

	var total int64
	parseErr := forEachSSEEvent(upstreamResp.Body, p.MaxBufferedBodyBytes, func(rawEvent []byte, data []byte) error {
		// 1. Try to decode the event's payload as a JSON-RPC message.
		//    Events whose data is empty or doesn't decode (comments,
		//    keepalives, non-JSON data) skip interception entirely and
		//    are relayed verbatim.
		var msg jsonrpc.Message
		if len(data) > 0 {
			if decoded, err := jsonrpc.DecodeMessage(data); err == nil {
				msg = decoded
			}
		}

		// 2. If we have a parseable JSON-RPC message, run interceptors
		//    BEFORE relaying to the client. Interceptors that return an
		//    error reject the message — its bytes are not written to the
		//    client and the proxy substitutes a spec-aligned JSON-RPC
		//    error event in its place (see substituteRejectedSSEEvent).
		var rejectionErr error
		if msg != nil {
			remoteMsg := &RemoteMessage{
				UserHTTPRequest:    userReq,
				RemoteHTTPRequest:  remoteReq,
				RemoteHTTPResponse: upstreamResp,
				Message:            msg,
			}

			if err := p.runRemoteMessageInterceptors(ctx, remoteMsg); err != nil {
				rejectionErr = err
			}

			// Typed dispatch on terminal response events. Same rejection
			// semantics as the generic chain — a typed rejection
			// substitutes the same way. At most one of the typed
			// dispatchers fires per event because at most one of
			// toolsCallReq/toolsListReq is non-nil.
			if rejectionErr == nil && haveTerminalID {
				if resp, ok := msg.(*jsonrpc.Response); ok && jsonrpcIDsEqual(resp.ID, terminalID) {
					switch {
					case toolsCallReq != nil:
						if typedResp, typedOK := toolsCallResponseFromRemoteMessage(toolsCallReq, remoteMsg); typedOK {
							if err := p.runToolsCallResponseInterceptors(ctx, typedResp); err != nil {
								rejectionErr = err
							}
						}
					case toolsListReq != nil:
						if typedResp, typedOK := toolsListResponseFromRemoteMessage(toolsListReq, remoteMsg); typedOK {
							if err := p.runToolsListResponseInterceptors(ctx, typedResp); err != nil {
								rejectionErr = err
							}
						}
					}
				}
			}
		}

		// 3. If the message was rejected, write a substitute event in its
		//    place so the user's MCP runtime can correlate (response/
		//    server-request shapes) or surface the rejection (notification
		//    shapes) rather than silently missing the message.
		if rejectionErr != nil {
			rejectErr := RejectErrorFromCause(rejectionErr)
			substitute, buildErr := substituteRejectedSSEEvent(msg, rejectErr)
			if buildErr != nil {
				p.Logger.WarnContext(ctx, "failed to build substitute SSE event for rejected message; dropping silently",
					attr.SlogError(buildErr),
					attr.SlogComponent("remotemcp.proxy"))
				return nil
			}
			p.Logger.InfoContext(ctx, "interceptor rejected SSE event; substituting JSON-RPC error event",
				attr.SlogError(rejectionErr),
				attr.SlogComponent("remotemcp.proxy"))
			if _, writeErr := w.Write(substitute); writeErr != nil {
				return fmt.Errorf("write substitute sse event: %w", writeErr)
			}
			total += int64(len(substitute))
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}

		// 4. Otherwise relay the raw event bytes to the client and flush
		//    so the next event reaches them as soon as it's read. Headers
		//    and prior events were already flushed during the previous
		//    loop iteration (or the initial header flush above).
		if _, writeErr := w.Write(rawEvent); writeErr != nil {
			return fmt.Errorf("stream sse event: %w", writeErr)
		}
		total += int64(len(rawEvent))
		if flusher != nil {
			flusher.Flush()
		}

		return nil
	})

	if parseErr != nil {
		return total, parseErr
	}
	return total, nil
}

// requestSpanAttributes returns the attribute set applied to every span the proxy
// emits for an inbound request. ServerID is optional on Proxy and is omitted
// when empty rather than emitted as an empty-string label.
func (p *Proxy) requestSpanAttributes(method string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attr.HTTPRequestMethod(method),
		attr.RemoteMCPServerURL(p.RemoteURL),
	}
	if p.ServerID != "" {
		attrs = append(attrs, attr.RemoteMCPServerID(p.ServerID))
	}
	return attrs
}

// wrapInterceptorRejection logs the rejection at error level and returns an
// error that wraps the interceptor's err with [fmt.Errorf]. The wrap
// deliberately uses fmt.Errorf rather than oops.E so a typed inner error
// (e.g. a [*RejectError] or an [*oops.ShareableError] with a domain code
// like CodeForbidden) is the first match when [RejectErrorFromCause] walks
// the error chain — wrapping with oops.E would prepend an outer
// *oops.ShareableError with CodeUnexpected, masking the interceptor's
// intended JSON-RPC code.
//
// Span error attribution lives at the call site so the spancheck linter can
// statically verify each rejection path marks its span.
func (p *Proxy) wrapInterceptorRejection(ctx context.Context, kind string, name string, err error) error {
	p.Logger.ErrorContext(ctx, "remote mcp proxy interceptor rejected",
		attr.SlogComponent("remotemcp.proxy"),
		attr.SlogRemoteMCPProxyInterceptor(name),
		attr.SlogError(err))
	return fmt.Errorf("%s interceptor %s rejected: %w", kind, name, err)
}

// runRemoteMessageInterceptors invokes each configured
// RemoteMessageInterceptor in order, returning the first wrapped rejection
// error or nil if all interceptors accept the message.
func (p *Proxy) runRemoteMessageInterceptors(ctx context.Context, msg *RemoteMessage) error {
	for _, interceptor := range p.RemoteMessageInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.RemoteMessageInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptRemoteMessage(iterCtx, msg); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "remote message", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// runToolsCallRequestInterceptors invokes each configured
// ToolsCallRequestInterceptor in order, returning the first wrapped
// rejection error or nil if all interceptors accept the request.
func (p *Proxy) runToolsCallRequestInterceptors(ctx context.Context, call *ToolsCallRequest) error {
	for _, interceptor := range p.ToolsCallRequestInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.ToolsCallRequestInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptToolsCallRequest(iterCtx, call); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "tools/call request", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// runToolsCallResponseInterceptors invokes each configured
// ToolsCallResponseInterceptor in order, returning the first wrapped
// rejection error or nil if all interceptors accept the response.
func (p *Proxy) runToolsCallResponseInterceptors(ctx context.Context, call *ToolsCallResponse) error {
	for _, interceptor := range p.ToolsCallResponseInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.ToolsCallResponseInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptToolsCallResponse(iterCtx, call); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "tools/call response", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// runToolsListRequestInterceptors invokes each configured
// ToolsListRequestInterceptor in order, returning the first wrapped
// rejection error or nil if all interceptors accept the request.
func (p *Proxy) runToolsListRequestInterceptors(ctx context.Context, list *ToolsListRequest) error {
	for _, interceptor := range p.ToolsListRequestInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.ToolsListRequestInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptToolsListRequest(iterCtx, list); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "tools/list request", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// runToolsListResponseInterceptors invokes each configured
// ToolsListResponseInterceptor in order, returning the first wrapped
// rejection error or nil if all interceptors accept the response.
func (p *Proxy) runToolsListResponseInterceptors(ctx context.Context, list *ToolsListResponse) error {
	for _, interceptor := range p.ToolsListResponseInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.ToolsListResponseInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptToolsListResponse(iterCtx, list); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "tools/list response", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// runUserRequestInterceptors invokes each configured UserRequestInterceptor
// in order, returning the first wrapped rejection error or nil if all
// interceptors accept the request. Each invocation gets its own span so
// per-interceptor timing and the rejecting interceptor's name are visible
// in traces.
func (p *Proxy) runUserRequestInterceptors(ctx context.Context, req *UserRequest) error {
	for _, interceptor := range p.UserRequestInterceptors {
		iterCtx, span := p.Tracer.Start(ctx, "remotemcp.proxy.UserRequestInterceptor",
			trace.WithAttributes(attr.RemoteMCPProxyInterceptor(interceptor.Name())))
		if err := interceptor.InterceptUserRequest(iterCtx, req); err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true))
			span.End()
			return p.wrapInterceptorRejection(iterCtx, "user request", interceptor.Name(), err)
		}
		span.End()
	}
	return nil
}

// writeRejection writes a spec-aligned JSON-RPC error envelope to w in
// response to an interceptor rejection on the JSON path. id is the
// originating user request's id and is used as the correlation id on the
// envelope; an invalid id (notification rejection) leads to omitting the
// "id" field and using HTTP 400 per MCP § Streamable HTTP transport.
// Otherwise the envelope is written with HTTP 200 — the JSON-RPC error is
// the response.
//
// Always marks the span as failed with the original cause: a successful
// rejection write is still a non-success outcome from the user's
// perspective, and span status drives error rates in dashboards. Returns
// the number of bytes written so the caller's metrics defer captures it.
//
// Upstream response headers are deliberately NOT relayed alongside the
// rejection envelope. Substitution replaces upstream's body with our own,
// so we serve our own Content-Type and skip propagating upstream's
// metadata (headers may carry tracing IDs, custom fields, or — in failure
// modes — sensitive material).
//
// Never returns an error: a write failure here means the client connection
// is already broken, so we log and move on; returning an error would
// cause [oops.ErrHandle] to attempt a second WriteHeader on top of the
// one this helper already issued.
func (p *Proxy) writeRejection(ctx context.Context, w http.ResponseWriter, span trace.Span, id jsonrpc.ID, cause error) int64 {
	rejectErr := RejectErrorFromCause(cause)
	payload, err := marshalErrorResponse(id, rejectErr)
	if err != nil {
		p.Logger.WarnContext(ctx, "failed to marshal jsonrpc rejection envelope; client will see no body",
			attr.SlogError(err),
			attr.SlogComponent("remotemcp.proxy"))
		span.SetStatus(codes.Error, cause.Error())
		span.RecordError(cause, trace.WithStackTrace(true))
		return 0
	}

	statusCode := http.StatusOK
	if !id.IsValid() {
		// Per MCP § Streamable HTTP: rejected notifications get HTTP 4xx.
		statusCode = http.StatusBadRequest
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	n, writeErr := w.Write(payload)
	if writeErr != nil {
		p.Logger.WarnContext(ctx, "failed to write jsonrpc rejection envelope to client",
			attr.SlogError(writeErr),
			attr.SlogComponent("remotemcp.proxy"))
	}
	span.SetStatus(codes.Error, cause.Error())
	span.RecordError(cause, trace.WithStackTrace(true))
	return int64(n)
}

// writeResponse relays status, headers, and body from the upstream response
// back to the user. body may differ from upstreamResp.Body (POST replaces it
// with a buffered reader after parsing JSON-RPC). Returns the number of bytes
// written to the client.
//
// No size cap is applied here: peak server memory during [io.Copy] is
// bounded by its internal buffer, and the body coming from a parsed POST
// flow was already capped during [readJSONRPCBody] (upstream response) or
// [UserRequest.ParseJSONRPCMessages] (inbound user request).
func writeResponse(w http.ResponseWriter, upstreamResp *http.Response, body io.Reader) (int64, error) {
	applyResponseHeaders(w, upstreamResp)
	w.WriteHeader(upstreamResp.StatusCode)

	if body == nil {
		return 0, nil
	}

	n, err := io.Copy(w, body)
	if err != nil {
		return n, fmt.Errorf("relay response body: %w", err)
	}
	return n, nil
}
