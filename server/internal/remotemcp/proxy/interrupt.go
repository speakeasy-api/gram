package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// upstreamCancelTimeout bounds the best-effort notifications/cancelled sent
// upstream for an interrupted request. It is spent inside the server's
// shutdown window, so it stays short; an upstream that cannot accept a
// notification this quickly would not act on it in time anyway.
const upstreamCancelTimeout = 2 * time.Second

// errShutdownInterrupt is the cause given to the upstream call of a request
// the drain interrupted because the remote MCP server had not responded.
var errShutdownInterrupt = errors.New("interrupted by server shutdown")

// forwardInterrupted reports whether the drain interrupted the upstream call
// running under forwardCtx.
func forwardInterrupted(forwardCtx context.Context) bool {
	return errors.Is(context.Cause(forwardCtx), errShutdownInterrupt)
}

// interruptedErrorData is the data of the JSON-RPC error that answers a
// request interrupted by shutdown.
type interruptedErrorData struct {
	// Reason is always "server_shutdown".
	Reason string `json:"reason"`

	// Retryable reports whether the request can safely be sent again. Only
	// tools/call can have side effects the remote MCP server may already have
	// carried out, so it is never marked retryable.
	Retryable bool `json:"retryable"`
}

// interruptedRejection is the JSON-RPC error that answers a request the drain
// interrupted. -32603 (internal error) is the one code valid under every
// supported MCP revision for a server-side failure: 2026-07-28 deprecates the
// -32000 to -32019 range and reserves the rest of the server error band.
func interruptedRejection(method string) *RejectError {
	return &RejectError{
		Code:    RejectCodeInternalError,
		Message: "Speakeasy is restarting and stopped waiting for the MCP server's response. The request may still complete on the MCP server.",
		Data:    interruptedErrorData{Reason: "server_shutdown", Retryable: method != methodToolsCall},
	}
}

// writeInterruptedEvent answers an interrupted request on its open SSE stream.
// A notification is owed no response, so nothing is written for it.
func writeInterruptedEvent(w http.ResponseWriter, id jsonrpc.ID, method string) (int64, error) {
	if !id.IsValid() {
		return 0, nil
	}
	payload, err := marshalErrorResponse(id, interruptedRejection(method))
	if err != nil {
		return 0, err
	}
	return writeSSEBytes(w, formatSSEDataEvent(payload))
}

// writeInterruptedResponse answers an interrupted request before any response
// header was written. A request gets its JSON-RPC error with HTTP 200, as every
// MCP revision requires of a request answered with a JSON body. A notification
// gets 503: MCP requires an HTTP error status when a notification is not
// accepted, and a 4xx would tell 2026-07-28 clients to fall back to an older
// revision.
func (p *Proxy) writeInterruptedResponse(ctx context.Context, w http.ResponseWriter, span trace.Span, id jsonrpc.ID, method string) int64 {
	if !id.IsValid() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return 0
	}
	return p.writeRejection(ctx, w, span, id, interruptedRejection(method))
}

// logInterrupted records a request the drain interrupted. It is a warning,
// not an error: a deploy that catches a slow tool call does this by design.
func (p *Proxy) logInterrupted(ctx context.Context, method string, resumable bool) {
	p.logWithIdentity(ctx, slog.LevelWarn, "interrupted remote mcp request at shutdown",
		attr.SlogComponent("remotemcp.proxy"),
		attr.SlogMcpMethod(mcprequests.ClampMethod(method)),
		attr.SlogRemoteMCPProxyStreamResumable(resumable),
	)
}

// cancelledNotification is the MCP notifications/cancelled message.
type cancelledNotification struct {
	// JSONRPC is always "2.0".
	JSONRPC string `json:"jsonrpc"`

	// Method is always "notifications/cancelled".
	Method string `json:"method"`

	// Params names the cancelled request.
	Params cancelledParams `json:"params"`
}

// cancelledParams are the params of notifications/cancelled.
type cancelledParams struct {
	// RequestID is the id of the request being cancelled.
	RequestID any `json:"requestId"`

	// Reason explains the cancellation.
	Reason string `json:"reason"`
}

// cancelUpstreamRequest asks the remote MCP server to stop working on a
// request the proxy stopped waiting for, so it does not finish work nobody
// will see. Before 2026-07-28, closing the connection is explicitly not a
// cancellation, so the proxy sends notifications/cancelled in the client's
// session. Under 2026-07-28 closing the response stream is the cancellation
// and nothing is sent. Best effort: failures are logged at debug level.
//
// answeredBy is the URL that answered the interrupted request, nil when no
// response arrived. When the request followed a redirect on the configured
// origin, the notification goes there, where the session lives. A redirect to
// another origin is not followed: the proxy strips credentials on such a
// hop, and resending them there would undo that.
func (p *Proxy) cancelUpstreamRequest(ctx context.Context, userReq *http.Request, id jsonrpc.ID, answeredBy *url.URL) {
	if !id.IsValid() || mcpversions.AtLeast(mcpversions.Sanitize(userReq.Header.Get(mcpversions.HTTPHeader)), mcpversions.Version20260728) {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamCancelTimeout)
	defer cancel()

	if err := p.sendUpstreamCancel(ctx, userReq, id, p.upstreamCancelTarget(answeredBy)); err != nil {
		p.Logger.DebugContext(ctx, "send notifications/cancelled to remote mcp server",
			attr.SlogComponent("remotemcp.proxy"), attr.SlogError(err))
	}
}

// upstreamCancelTarget returns the URL notifications/cancelled is sent to:
// answeredBy when it is on the configured origin, otherwise the configured URL.
func (p *Proxy) upstreamCancelTarget(answeredBy *url.URL) string {
	if answeredBy == nil {
		return p.RemoteURL
	}
	configured, err := url.Parse(p.RemoteURL)
	if err != nil || !sameRemoteMCPOrigin(configured, answeredBy) {
		return p.RemoteURL
	}
	return answeredBy.String()
}

// answeredBy returns the URL that produced resp, after any redirects, or nil.
func answeredBy(resp *http.Response) *url.URL {
	if resp == nil || resp.Request == nil {
		return nil
	}
	return resp.Request.URL
}

func (p *Proxy) sendUpstreamCancel(ctx context.Context, userReq *http.Request, id jsonrpc.ID, target string) error {
	body, err := json.Marshal(cancelledNotification{
		JSONRPC: "2.0",
		Method:  mcpversions.MethodNotificationsCancelled,
		Params:  cancelledParams{RequestID: id.Raw(), Reason: "Speakeasy is restarting and stopped waiting for the response."},
	})
	if err != nil {
		return fmt.Errorf("marshal notifications/cancelled: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build notifications/cancelled request: %w", err)
	}
	// The same headers as the interrupted request, so the notification lands
	// in the same session with the same credentials and tunnel routing.
	if err := p.applyRequestHeaders(ctx, userReq, req); err != nil {
		return fmt.Errorf("apply notifications/cancelled headers: %w", err)
	}

	client := p.GuardianPolicy.Client(append([]guardian.ClientOption{guardian.WithInternalCatalog()}, p.GuardianClientOptions...)...)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post notifications/cancelled: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote mcp server answered notifications/cancelled with %d", resp.StatusCode)
	}
	return nil
}
