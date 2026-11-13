package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// maxUpstreamToolListSize bounds how many tools a listing may return. Real
// servers list at most hundreds; this stops an upstream paginating without end
// from holding memory until the caller's deadline.
const maxUpstreamToolListSize = 10_000

// upstreamToolListHeaders are the caller's protocol headers, which describe the
// caller's own session rather than the separate one a listing opens.
var upstreamToolListHeaders = []string{
	"Accept",
	"Content-Type",
	"Last-Event-ID",
	"Mcp-Method",
	"Mcp-Name",
	"MCP-Protocol-Version",
	McpSessionIDHeader,
}

// UpstreamListingRefusalError is why a listing for a tools/call couldn't complete,
// answered to the caller the way a forwarded call failing the same way would be,
// so an expired credential still reaches its repair flow.
type UpstreamListingRefusalError struct {
	// Cause is the error forwarding or [Proxy.UpstreamResponseInterceptor]
	// returned. It is nil when the upstream answered with a refusal instead.
	Cause error

	// Response carries the status and headers of an upstream auth refusal, and
	// is nil when Cause is set. Its body is not retained.
	Response *http.Response
}

func (e *UpstreamListingRefusalError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("upstream listing refused: %s", e.Cause)
	}
	return fmt.Sprintf("upstream listing refused: status %d", e.Response.StatusCode)
}

func (e *UpstreamListingRefusalError) Unwrap() error {
	return e.Cause
}

// ListUpstreamTools returns the upstream's whole tool list, across every page, in
// a separate MCP session using userReq's credentials. No tools/list interceptors
// run, so the result isn't narrowed by the caller's access. Requests travel the
// same path as forwarded traffic: same headers, credentials, target validation,
// redirect rules, credential replacement and response interception. A failure
// that path decides is returned as an [UpstreamListingRefusalError].
func (p *Proxy) ListUpstreamTools(ctx context.Context, userReq *http.Request) ([]*mcp.Tool, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "speakeasy-tool-catalog", Title: "", Version: "1.0.0", WebsiteURL: "", Description: "", Icons: nil}, nil)

	listing := &upstreamListingTransport{proxy: p, userReq: userReq, mu: sync.Mutex{}, refusal: nil}

	// Only the transport is used: it forwards through a guardian client of its
	// own, which also decides redirects.
	httpClient := p.GuardianPolicy.Client()
	httpClient.Transport = listing
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	transport := &mcp.StreamableClientTransport{
		Endpoint:             p.RemoteURL,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
		OAuthHandler:         nil,
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, listing.failure(fmt.Errorf("connect to upstream: %w", err))
	}
	defer o11y.NoLogDefer(session.Close)

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, listing.failure(fmt.Errorf("list tools: %w", err))
		}
		if len(tools) == maxUpstreamToolListSize {
			return nil, errors.New("list tools: upstream exceeded the tool list size limit")
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// upstreamListingTransport sends an SDK client's requests through the proxy's
// forwarding path. It keeps the first refusal, because the SDK reduces transport
// failures to errors that no longer identify it.
type upstreamListingTransport struct {
	proxy   *Proxy
	userReq *http.Request

	mu      sync.Mutex
	refusal *UpstreamListingRefusalError
}

func (t *upstreamListingTransport) failure(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.refusal != nil {
		return t.refusal
	}
	return err
}

func (t *upstreamListingTransport) refuse(refusal *UpstreamListingRefusalError) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.refusal == nil {
		t.refusal = refusal
	}
	return refusal
}

func (t *upstreamListingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		defer o11y.NoLogDefer(req.Body.Close)

		read, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read upstream listing request: %w", err)
		}
		body = read
	}

	forwarded := t.userReq.Clone(req.Context())
	forwarded.Method = req.Method
	for _, name := range upstreamToolListHeaders {
		forwarded.Header.Del(name)
	}
	maps.Copy(forwarded.Header, req.Header)

	_, resp, err := t.proxy.forwardRequestWithRetry(req.Context(), forwarded, func() io.Reader { return bytes.NewReader(body) }, nil)
	if err != nil {
		return nil, t.refuse(&UpstreamListingRefusalError{Cause: err, Response: nil})
	}

	if t.proxy.UpstreamResponseInterceptor != nil {
		if err := t.proxy.UpstreamResponseInterceptor(req.Context(), resp); err != nil {
			o11y.NoLogDefer(resp.Body.Close)
			return nil, t.refuse(&UpstreamListingRefusalError{Cause: err, Response: nil})
		}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		o11y.NoLogDefer(resp.Body.Close)

		refused := new(http.Response)
		refused.StatusCode = resp.StatusCode
		refused.Header = resp.Header.Clone()
		return nil, t.refuse(&UpstreamListingRefusalError{Cause: nil, Response: refused})
	}
	return resp, nil
}
