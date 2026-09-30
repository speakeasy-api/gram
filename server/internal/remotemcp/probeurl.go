package remotemcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const (
	// probeURLTimeout bounds connection setup and response classification for
	// the synchronous management endpoint. It also bounds each HTTP request,
	// including the session DELETE the SDK sends on a detached context.
	probeURLTimeout = 10 * time.Second

	probeURLMaxRedirects = 3

	// probeURLMaxBodyBytes caps each response body the probe reads. Discovery
	// and initialize results are small; 1 MiB leaves headroom without letting
	// an untrusted upstream hold a large buffer.
	probeURLMaxBodyBytes = 1 << 20 // 1 MiB

	ProbeOutcomeMCPAvailable           = "mcp_available"
	ProbeOutcomeAuthenticationRequired = "authentication_required"
	ProbeOutcomeInvalidMCPResponse     = "invalid_mcp_response"
	ProbeOutcomeUnreachable            = "unreachable"

	ProbeReasonTimeout          = "timeout"
	ProbeReasonRateLimited      = "rate_limited"
	ProbeReasonServerError      = "server_error"
	ProbeReasonDNSError         = "dns_error"
	ProbeReasonTLSError         = "tls_error"
	ProbeReasonGuardianRejected = "guardian_rejected"
	ProbeReasonTransportError   = "transport_error"
)

type ProbeResult struct {
	// Outcome identifies the probe result variant.
	Outcome string

	// ProtectedResourceMetadataURL is an advertised absolute HTTP(S) metadata URL.
	ProtectedResourceMetadataURL *string

	// HTTPStatus is the status returned by the remote server, when one was received.
	HTTPStatus *int

	// Reason is the stable failure code for unreachable outcomes.
	Reason *string
}

type probeObservation struct {
	result     ProbeResult
	httpStatus *int
}

// ProbeRemoteMcpURL connects to rawURL with the MCP SDK client and reports a
// structured outcome. The caller is responsible for bounding the overall
// deadline via ctx.
func ProbeRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string) ProbeResult {
	if _, err := proxy.ValidateRemoteMCPURL(ctx, policy, rawURL); err != nil {
		return classifyTransportError(ctx, err)
	}
	return probeRemoteMcpURL(ctx, policy, rawURL).result
}

// probeRemoteMcpURL probes a URL that has already passed management-time
// validation. Redirect targets are independently validated before following.
//
// The SDK client opens with a 2026-07-28 server/discover request and falls
// back to the initialize handshake when the upstream does not answer it, so
// upstreams that implement either one verify.
func probeRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string) probeObservation {
	client := policy.Client()
	client.Timeout = probeURLTimeout
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > probeURLMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", probeURLMaxRedirects)
		}
		if _, err := proxy.ValidateRemoteMCPURL(req.Context(), policy, req.URL.String()); err != nil {
			return fmt.Errorf("validate redirect url: %w", err)
		}

		// Redirect semantics match the hosted proxy, so the probe cannot
		// approve a path the proxy will not follow. 301, 302 and 303 are left
		// to net/http, which converts them to GET exactly as the runtime
		// does — an endpoint reachable only over POST therefore fails the
		// probe rather than passing it and failing at initialize. A 307 or
		// 308 keeps method and body, which must not leave the origin.
		if proxy.CrossOriginBodyReplay(via[0].URL, req) {
			return fmt.Errorf("probe redirect to %s: %w", req.URL.Host, proxy.ErrCrossOriginRemoteMCPRedirect)
		}
		return nil
	}
	recorder := &probeRoundTripper{base: client.Transport, mu: sync.Mutex{}, last: nil}
	client.Transport = recorder

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:        "gram-probe",
		Title:       "",
		Description: "",
		Version:     "1",
		WebsiteURL:  "",
		Icons:       nil,
	}, nil)
	transport := &probeTransport{
		inner: &mcp.StreamableClientTransport{
			Endpoint:   rawURL,
			HTTPClient: client,
			// mcp.StreamableClientTransport treats zero as the SDK default of 5;
			// a negative value disables reconnects so the probe stays one-shot.
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
			OAuthHandler:         nil,
		},
		conn: nil,
	}
	session, err := mcpClient.Connect(ctx, transport, nil)
	last := recorder.lastResponse()

	// Closing sends a DELETE for a stateful session on a context the SDK
	// detaches from ctx; client.Timeout bounds it, and the result does not
	// wait on it.
	if err != nil {
		if transport.conn != nil {
			go o11y.NoLogDefer(transport.conn.Close)
		}
		return classifyProbeError(ctx, err, last)
	}
	go o11y.NoLogDefer(session.Close)

	var status *int
	if last != nil {
		status = &last.status
	}
	return probeObservation{result: mcpAvailableResult(), httpStatus: status}
}

// probeResponse is the part of an upstream response the probe classifies.
type probeResponse struct {
	// status is the HTTP status code.
	status int

	// wwwAuthenticate holds every WWW-Authenticate header value.
	wwwAuthenticate []string
}

// probeTransport keeps the connection it opens. Connect does not close the
// connection on every failure path, and an unclosed connection leaves the
// SDK's reader goroutine blocked for good.
type probeTransport struct {
	inner mcp.Transport
	conn  mcp.Connection
}

func (t *probeTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	t.conn = conn
	return conn, err //nolint:wrapcheck // the SDK classifies its own transport errors
}

// probeRoundTripper caps response bodies and remembers the most recent
// response. The SDK falls back from server/discover to initialize on every
// discover failure, and the session DELETE it sends while closing is not
// recorded, so when Connect fails the most recent response belongs to the
// request that failed.
type probeRoundTripper struct {
	base http.RoundTripper
	mu   sync.Mutex
	last *probeResponse
}

func (rt *probeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // preserve the base transport's error for classification
	}

	if req.Method != http.MethodDelete {
		rt.mu.Lock()
		rt.last = &probeResponse{status: resp.StatusCode, wwwAuthenticate: resp.Header.Values("WWW-Authenticate")}
		rt.mu.Unlock()
	}

	resp.Body = http.MaxBytesReader(nil, resp.Body, probeURLMaxBodyBytes)
	return resp, nil
}

func (rt *probeRoundTripper) lastResponse() *probeResponse {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.last
}

// classifyProbeError maps a failed SDK connection to a probe outcome. Checks
// run in order: failures that never produced a usable response, HTTP statuses
// with a fixed meaning, then JSON-RPC errors, which show the upstream speaks
// the protocol even while rejecting the request.
func classifyProbeError(ctx context.Context, err error, last *probeResponse) probeObservation {
	if _, ok := errors.AsType[*url.Error](err); ok || ctx.Err() != nil || last == nil {
		return probeObservation{result: classifyTransportError(ctx, err), httpStatus: nil}
	}

	status := last.status
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return probeObservation{
			result: ProbeResult{
				Outcome:                      ProbeOutcomeAuthenticationRequired,
				ProtectedResourceMetadataURL: parseProtectedResourceMetadataURL(last.wwwAuthenticate),
				HTTPStatus:                   nil,
				Reason:                       nil,
			},
			httpStatus: &status,
		}
	case status == http.StatusRequestTimeout:
		return probeObservation{result: unreachableResult(ProbeReasonTimeout, &status), httpStatus: &status}
	case status == http.StatusTooManyRequests:
		return probeObservation{result: unreachableResult(ProbeReasonRateLimited, &status), httpStatus: &status}
	case status >= http.StatusInternalServerError:
		return probeObservation{result: unreachableResult(ProbeReasonServerError, &status), httpStatus: &status}
	}

	if _, ok := errors.AsType[*jsonrpc.Error](err); ok {
		return probeObservation{result: mcpAvailableResult(), httpStatus: &status}
	}
	return probeObservation{result: invalidMCPResponseResult(status), httpStatus: &status}
}

func mcpAvailableResult() ProbeResult {
	return ProbeResult{
		Outcome:                      ProbeOutcomeMCPAvailable,
		ProtectedResourceMetadataURL: nil,
		HTTPStatus:                   nil,
		Reason:                       nil,
	}
}

func invalidMCPResponseResult(status int) ProbeResult {
	return ProbeResult{
		Outcome:                      ProbeOutcomeInvalidMCPResponse,
		ProtectedResourceMetadataURL: nil,
		HTTPStatus:                   &status,
		Reason:                       nil,
	}
}

func unreachableResult(reason string, status *int) ProbeResult {
	return ProbeResult{
		Outcome:                      ProbeOutcomeUnreachable,
		ProtectedResourceMetadataURL: nil,
		HTTPStatus:                   status,
		Reason:                       &reason,
	}
}

func classifyTransportError(ctx context.Context, err error) ProbeResult {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return unreachableResult(ProbeReasonTimeout, nil)
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return unreachableResult(ProbeReasonTimeout, nil)
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return unreachableResult(ProbeReasonDNSError, nil)
	}
	if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
		return unreachableResult(ProbeReasonGuardianRejected, nil)
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return unreachableResult(ProbeReasonTLSError, nil)
	}
	if _, ok := errors.AsType[*tls.RecordHeaderError](err); ok {
		return unreachableResult(ProbeReasonTLSError, nil)
	}
	return unreachableResult(ProbeReasonTransportError, nil)
}

func parseProtectedResourceMetadataURL(headers []string) *string {
	for _, header := range headers {
		for i := 0; i < len(header); {
			if header[i] == '"' {
				_, i, _ = parseAuthParamValue(header, i)
				continue
			}
			if !isAuthTokenByte(header[i]) {
				i++
				continue
			}

			start := i
			for i < len(header) && isAuthTokenByte(header[i]) {
				i++
			}
			name := header[start:i]
			for i < len(header) && (header[i] == ' ' || header[i] == '\t') {
				i++
			}
			if i >= len(header) || header[i] != '=' {
				continue
			}
			i++
			for i < len(header) && (header[i] == ' ' || header[i] == '\t') {
				i++
			}

			value, next, ok := parseAuthParamValue(header, i)
			i = next
			if !ok || !strings.EqualFold(name, "resource_metadata") {
				continue
			}

			metadataURL, err := url.Parse(value)
			if err != nil || !metadataURL.IsAbs() || metadataURL.Host == "" || metadataURL.User != nil ||
				(!strings.EqualFold(metadataURL.Scheme, "http") && !strings.EqualFold(metadataURL.Scheme, "https")) {
				continue
			}
			return &value
		}
	}
	return nil
}

func parseAuthParamValue(header string, start int) (string, int, bool) {
	if start >= len(header) {
		return "", start, false
	}
	if header[start] != '"' {
		end := start
		for end < len(header) && header[end] != ',' && header[end] != ' ' && header[end] != '\t' {
			end++
		}
		return header[start:end], end, end > start
	}

	var value strings.Builder
	for i := start + 1; i < len(header); i++ {
		switch header[i] {
		case '\\':
			i++
			if i >= len(header) {
				return "", len(header), false
			}
			value.WriteByte(header[i])
		case '"':
			return value.String(), i + 1, true
		case '\r', '\n':
			return "", len(header), false
		default:
			value.WriteByte(header[i])
		}
	}
	return "", len(header), false
}

func isAuthTokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b))
}
