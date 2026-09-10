// The consent-page probe: a dry run of dispatch on the official MCP SDK, routed through the member's proxy.

package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// probeReserve is the most a probe keeps back from ValidationTimeout, and a
// quarter of the budget the least: half a floor for the session close after
// the deadline, half for the verdict write after that.
const probeReserve = 2 * time.Second

// errProbeResponseTooLarge marks a member answer past MaxMemberResponseBytes.
var errProbeResponseTooLarge = errors.New("member response exceeds the meta MCP response cap")

// memberRoundTripper runs every SDK request through the member's proxy builder,
// so routing, tunnel affinity, configured headers, RBAC and visibility all
// apply, and records any authentication rejection the member answered with.
type memberRoundTripper struct {
	build  memberProxyBuilder
	logger *slog.Logger
	// deadline is the probe's; a DELETE after it still gets closeFloor.
	deadline   time.Time
	closeFloor time.Duration

	mu         sync.Mutex
	rejected   int
	lastStatus int
}

// closeBudget bounds a session DELETE: the usual close timeout while the probe
// has that much left, never less than the floor once it has not.
func (rt *memberRoundTripper) closeBudget() time.Duration {
	return min(memberSessionCloseTimeout, max(time.Until(rt.deadline), 0)+rt.closeFloor)
}

func (rt *memberRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if req.Method == http.MethodDelete {
		// The SDK closes on its own detached context; the DELETE gets its own budget.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), rt.closeBudget())
		defer cancel()
	}
	p, err := rt.build(ctx)
	if err != nil {
		return nil, err
	}
	body := req.Body
	if body == nil {
		body = http.NoBody
	}
	out, err := http.NewRequestWithContext(ctx, req.Method, "/", body)
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
	if err := serveProxyBackend(rec, out, p); err != nil {
		if req.Method != http.MethodDelete {
			closeUpstreamSession(ctx, rt.logger, rt.build, mintedSession, rt.closeBudget())
		}
		return nil, err
	}
	if rec.truncated {
		if req.Method != http.MethodDelete {
			closeUpstreamSession(ctx, rt.logger, rt.build, mintedSession, rt.closeBudget())
		}
		return nil, errProbeResponseTooLarge
	}
	rt.mu.Lock()
	rt.lastStatus = rec.status
	if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
		rt.rejected = rec.status
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

// probeUpstream connects to the member the way dispatch would and lists its
// tools, all inside ValidationTimeout less the close floor and the verdict's
// write window. The verdict is what a real tool call would have seen: a 401
// or 403 on either leg is a rejection, a tool list is valid, anything else is
// unknown. Closing the session is the SDK's DELETE.
func (s *Service) probeUpstream(ctx context.Context, logger *slog.Logger, build memberProxyBuilder, name string) (remotesessions.ValidationOutcome, string) {
	timeout := s.metaRuntime.ValidationTimeout
	if parentDeadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(parentDeadline))
	}
	reserve := min(probeReserve, timeout/4)
	deadline := time.Now().Add(max(timeout-reserve, time.Millisecond))
	probeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	rt := &memberRoundTripper{build: build, logger: logger, deadline: deadline, closeFloor: reserve / 2, mu: sync.Mutex{}, rejected: 0, lastStatus: 0}
	client := mcp.NewClient(&mcp.Implementation{Name: "gram-gateway", Version: "1", Title: "", Description: "", WebsiteURL: "", Icons: nil}, nil)
	// The policy's client, with the member proxy as its transport: every dial happens inside the proxy, under the same policy.
	httpClient := s.guardianPolicy.Client()
	httpClient.Transport = rt
	transport := &mcp.StreamableClientTransport{
		Endpoint:             "http://member.invalid/",
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
		OAuthHandler:         nil,
	}
	session, err := client.Connect(probeCtx, transport, nil)
	if err != nil {
		return classifyProbe(probeCtx, rt, err, name)
	}
	_, err = session.ListTools(probeCtx, nil)
	if cerr := session.Close(); cerr != nil {
		logger.DebugContext(ctx, "close probe session", attr.SlogError(cerr))
	}
	if err != nil {
		return classifyProbe(probeCtx, rt, err, name)
	}
	return remotesessions.ValidationOutcomeValid, ""
}

// classifyProbe maps a failed leg to a verdict and a fixed-phrase reason; nothing upstream sent is quoted.
func classifyProbe(ctx context.Context, rt *memberRoundTripper, err error, name string) (remotesessions.ValidationOutcome, string) {
	if _, rejected := rt.rejection(); rejected {
		return remotesessions.ValidationOutcomeRejectedByMember, "Rejected by " + name
	}
	switch status := rt.status(); {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return remotesessions.ValidationOutcomeUnknown, name + " did not answer in time"
	case errors.Is(err, errProbeResponseTooLarge):
		return remotesessions.ValidationOutcomeUnknown, "Unexpected answer from " + name
	case status == 0:
		return remotesessions.ValidationOutcomeUnknown, "Could not reach " + name
	case !upstreamStatusOK(status):
		return remotesessions.ValidationOutcomeUnknown, fmt.Sprintf("%s answered with status %d", name, status)
	default:
		return remotesessions.ValidationOutcomeUnknown, "Unexpected answer from " + name
	}
}
