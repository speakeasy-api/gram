package remotemcp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const (
	// probeURLTimeout bounds the total time spent issuing the MCP initialize
	// probe — including connect, TLS handshake, and response read — to keep
	// the synchronous management API responsive when the remote server is
	// slow or unreachable.
	probeURLTimeout = 10 * time.Second

	// probeURLMaxRedirects caps the number of HTTP redirects [ProbeRemoteMcpURL]
	// will follow before failing the probe. Bounding this keeps the probe from
	// being used to chase arbitrary redirect chains supplied by a hostile target.
	probeURLMaxRedirects = 3

	// probeURLMaxBodyBytes caps how many response bytes are read into memory
	// during success classification. Sized large enough to cover real-world
	// `initialize` responses (which include the server's full capabilities and
	// instructions strings) without forcing unbounded allocations on a hostile
	// target. Bodies exceeding this cap will fail JSON-RPC parsing and bucket
	// as "did not respond with a valid MCP message".
	probeURLMaxBodyBytes = 1024 * 1024
	probeURLRequestID    = int64(1)

	// probeURLBody is the canned MCP `initialize` JSON-RPC 2.0 request body
	// sent to the remote URL. Identical to what the MCP Go SDK would emit for
	// a minimal `mcp.InitializeParams` with our `clientInfo`; hardcoded so the
	// probe does not depend on package-init marshalling.
	//
	// The requested protocol version is a Gram-as-client choice, deliberately
	// independent of the supported sets Gram-as-server negotiates against: the
	// probe only needs the upstream to answer some valid MCP message, and it
	// never inspects which revision the upstream picks. Probing upstreams that
	// speak only 2026-07-28 — a revision with no `initialize` at all — needs a
	// modern-first probe with this request as the legacy fallback, which is
	// future work rather than a version bump here.
	probeURLBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"roots":{}},"clientInfo":{"name":"gram-probe","version":"1"},"protocolVersion":"` + mcpversions.Version20250618 + `"}}`

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

// ProbeRemoteMcpURL issues an MCP initialize request against rawURL and
// reports a structured outcome. The supplied [guardian.Policy] enforces the
// SSRF blocklist and Remote MCP transport requirements. The caller is
// responsible for bounding the overall deadline via ctx.
func ProbeRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string) ProbeResult {
	return probeRemoteMcpURL(ctx, policy, rawURL).result
}

func probeRemoteMcpURL(ctx context.Context, policy *guardian.Policy, rawURL string) probeObservation {
	if _, err := proxy.ValidateRemoteMCPURL(ctx, policy, rawURL); err != nil {
		return probeObservation{result: classifyTransportError(ctx, err), httpStatus: nil}
	}

	client := policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > probeURLMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", probeURLMaxRedirects)
		}
		if _, err := proxy.ValidateRemoteMCPURL(req.Context(), policy, req.URL.String()); err != nil {
			return fmt.Errorf("validate redirect url: %w", err)
		}

		original := via[0]
		body, err := original.GetBody()
		if err != nil {
			return fmt.Errorf("restore probe request body: %w", err)
		}
		req.Method = original.Method
		req.Header = make(http.Header) //nolint:gosec // Only non-sensitive probe negotiation headers are restored below.
		req.Header.Set("Accept", original.Header.Get("Accept"))
		req.Header.Set("Content-Type", original.Header.Get("Content-Type"))
		req.Body = body
		req.GetBody = original.GetBody
		req.ContentLength = original.ContentLength
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(probeURLBody))
	if err != nil {
		return probeObservation{result: unreachableResult(ProbeReasonTransportError, nil), httpStatus: nil}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.ContentLength = int64(len(probeURLBody))

	resp, err := client.Do(req)
	if err != nil {
		return probeObservation{result: classifyTransportError(ctx, err), httpStatus: nil}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	status := resp.StatusCode

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return probeObservation{
			result: ProbeResult{
				Outcome:                      ProbeOutcomeAuthenticationRequired,
				ProtectedResourceMetadataURL: parseProtectedResourceMetadataURL(resp.Header.Values("WWW-Authenticate")),
				HTTPStatus:                   nil,
				Reason:                       nil,
			},
			httpStatus: &status,
		}
	case http.StatusRequestTimeout:
		return probeObservation{result: unreachableResult(ProbeReasonTimeout, &status), httpStatus: &status}
	case http.StatusTooManyRequests:
		return probeObservation{result: unreachableResult(ProbeReasonRateLimited, &status), httpStatus: &status}
	}

	if status >= 200 && status < 300 {
		validMCP, err := classifyMCPSuccess(resp)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return probeObservation{result: invalidMCPResponseResult(status), httpStatus: &status}
			}
			result := classifyTransportError(ctx, err)
			result.HTTPStatus = &status
			return probeObservation{result: result, httpStatus: &status}
		}
		if validMCP {
			return probeObservation{
				result: ProbeResult{
					Outcome:                      ProbeOutcomeMCPAvailable,
					ProtectedResourceMetadataURL: nil,
					HTTPStatus:                   nil,
					Reason:                       nil,
				},
				httpStatus: &status,
			}
		}
		return probeObservation{result: invalidMCPResponseResult(status), httpStatus: &status}
	}

	if status >= 500 {
		return probeObservation{result: unreachableResult(ProbeReasonServerError, &status), httpStatus: &status}
	}

	return probeObservation{result: invalidMCPResponseResult(status), httpStatus: &status}
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

// classifyTransportError maps an outbound HTTP error to a probe
// outcome. Order matters: blocklist hits and context cancellations are checked
// before generic transport faults so they bucket distinctly.
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

// classifyMCPSuccess returns true when a 2xx response looks like a valid MCP
// reply: either a JSON-RPC 2.0 envelope or a Streamable HTTP SSE stream.
func classifyMCPSuccess(resp *http.Response) (bool, error) {
	contentType := resp.Header.Get("Content-Type")
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	switch mediaType {
	case "application/json":
		body, err := io.ReadAll(io.LimitReader(resp.Body, probeURLMaxBodyBytes+1))
		if err != nil {
			return false, fmt.Errorf("read probe response: %w", err)
		}
		if len(body) > probeURLMaxBodyBytes {
			return false, nil
		}
		return isInitializeResponse(body), nil
	case "text/event-stream":
		return containsJSONRPCSSEEvent(resp.Body)
	default:
		return false, nil
	}
}

func containsJSONRPCSSEEvent(body io.Reader) (bool, error) {
	limited := &io.LimitedReader{R: body, N: probeURLMaxBodyBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), probeURLMaxBodyBytes+1)

	var data strings.Builder
	decodeEvent := func() bool {
		if data.Len() == 0 {
			return false
		}
		return isInitializeResponse([]byte(data.String()))
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if decodeEvent() {
				return true, nil
			}
			data.Reset()
			continue
		}

		field, value, found := strings.Cut(line, ":")
		if !found || field != "data" {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(value)
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) || limited.N == 0 {
			return false, nil
		}
		return false, fmt.Errorf("scan probe event stream: %w", err)
	}
	if limited.N == 0 {
		return false, nil
	}
	return decodeEvent(), nil
}

func isInitializeResponse(data []byte) bool {
	message, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		return false
	}
	response, ok := message.(*jsonrpc.Response)
	return ok && response.ID.Raw() == probeURLRequestID
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
			if err != nil || !metadataURL.IsAbs() || metadataURL.Host == "" || (!strings.EqualFold(metadataURL.Scheme, "http") && !strings.EqualFold(metadataURL.Scheme, "https")) {
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
