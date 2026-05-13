package remotemcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// verifyURLTimeout bounds the total time spent issuing the MCP initialize
	// probe — including connect, TLS handshake, and response read — to keep
	// the synchronous management API responsive when the remote server is
	// slow or unreachable.
	verifyURLTimeout = 10 * time.Second

	// verifyURLMaxRedirects caps the number of HTTP redirects [VerifyRemoteMcpURL]
	// will follow before failing the probe. Bounding this keeps the probe from
	// being used to chase arbitrary redirect chains supplied by a hostile target.
	verifyURLMaxRedirects = 3

	// verifyURLMaxBodyBytes caps how many response bytes are read into memory
	// during success classification. Sized large enough to cover real-world
	// `initialize` responses (which include the server's full capabilities and
	// instructions strings) without forcing unbounded allocations on a hostile
	// target. Bodies exceeding this cap will fail JSON-RPC parsing and bucket
	// as "did not respond with a valid MCP message".
	verifyURLMaxBodyBytes = 1024 * 1024

	// verifyURLBody is the canned MCP `initialize` JSON-RPC 2.0 request body
	// sent to the remote URL. Identical to what the MCP Go SDK would emit for
	// a minimal `mcp.InitializeParams` with our `clientInfo`; hardcoded so the
	// probe does not depend on package-init marshalling.
	verifyURLBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"roots":{}},"clientInfo":{"name":"gram-verify","version":"1"},"protocolVersion":"2025-06-18"}}`
)

// VerifyRemoteMcpURL issues an MCP initialize request against rawURL and
// reports a verification outcome. The supplied [guardian.Policy] enforces the
// SSRF blocklist; rawURL must already have passed [validateURL]. The caller
// is responsible for bounding the overall deadline via ctx.
func VerifyRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string) (verified bool, httpStatus *int, message string) {
	client := policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= verifyURLMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", verifyURLMaxRedirects)
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(verifyURLBody))
	if err != nil {
		return false, nil, "Could not build verification request"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.ContentLength = int64(len(verifyURLBody))

	resp, err := client.Do(req)
	if err != nil {
		return classifyTransportError(ctx, err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	status := resp.StatusCode

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true, &status, "Reachable: received authorization required response"
	case http.StatusNotFound:
		return false, &status, "MCP response not found"
	}

	if status >= 200 && status < 300 {
		if classifyMCPSuccess(resp) {
			return true, &status, "Success"
		}
		return true, &status, "Reachable: although received unexpected MCP response"
	}

	return false, &status, "Unexpected response from server"
}

// classifyTransportError maps an outbound HTTP error to a verification
// outcome. Order matters: blocklist hits and context cancellations are checked
// before generic transport faults so they bucket distinctly.
func classifyTransportError(ctx context.Context, err error) (verified bool, httpStatus *int, message string) {
	if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
		return false, nil, "Host is not allowed"
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return false, nil, "Request timed out"
	}

	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return false, nil, "Request timed out"
	}

	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return false, nil, "TLS certificate verification failed"
	}
	if _, ok := errors.AsType[*tls.RecordHeaderError](err); ok {
		return false, nil, "TLS handshake failed"
	}

	return false, nil, "Could not connect to host"
}

// classifyMCPSuccess returns true when a 2xx response looks like a valid MCP
// reply: either a JSON-RPC 2.0 envelope or a Streamable HTTP SSE stream.
func classifyMCPSuccess(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	body, err := io.ReadAll(io.LimitReader(resp.Body, verifyURLMaxBodyBytes))
	if err != nil {
		return false
	}

	switch mediaType {
	case "application/json":
		if _, err := jsonrpc.DecodeMessage(body); err != nil {
			return false
		}
		return true
	case "text/event-stream":
		// We accept any non-empty SSE body. Fully parsing the stream would
		// require following the event protocol; for a reachability probe the
		// presence of an SSE response with a 2xx status is a strong enough
		// signal.
		return len(body) > 0
	default:
		return false
	}
}
