package xmcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// newPosthogForTest mirrors the pattern used elsewhere in the codebase
// (see e.g. server/internal/auth/sessions/speakeasyconnections_test.go) — a
// PostHog client constructed with bogus credentials so the wrapper does not
// short-circuit but enqueued events fail to deliver. Tests verify that the
// interceptor exercises the code path without panicking or rejecting; event
// content is not asserted because the wrapper does not surface enqueued
// payloads to callers.
func newPosthogForTest(t *testing.T) *posthog.Posthog {
	t.Helper()
	return posthog.New(t.Context(), testenv.NewLogger(t), "test-posthog-key", "test-posthog-host", "")
}

// newInitializeRequest builds an [*proxy.InitializeRequest] carrying the
// given session header, mirroring what the proxy passes to typed initialize
// interceptors after method routing.
func newInitializeRequest(t *testing.T, sessionID string) *proxy.InitializeRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", http.NoBody)
	require.NoError(t, err)
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	return &proxy.InitializeRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: httpReq,
		},
		Params: &mcp.InitializeParams{
			ProtocolVersion: "2025-03-26",
		},
	}
}

func TestInitializePostHogEventInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewInitializePostHogEventInterceptor(newPosthogForTest(t), testenv.NewLogger(t))
	require.Equal(t, "initialize-posthog-event", interceptor.Name())
}

func TestInitializePostHogEventInterceptor_MissingRequestContextPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewInitializePostHogEventInterceptor(newPosthogForTest(t), testenv.NewLogger(t))

	require.NoError(t, interceptor.InterceptInitializeRequest(t.Context(), newInitializeRequest(t, "session-1")))
}

func TestInitializePostHogEventInterceptor_PassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewInitializePostHogEventInterceptor(newPosthogForTest(t), testenv.NewLogger(t))

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-init",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/abc",
	})

	require.NoError(t, interceptor.InterceptInitializeRequest(ctx, newInitializeRequest(t, "session-init")))
}

func TestInitializePostHogEventInterceptor_MissingSessionIDPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewInitializePostHogEventInterceptor(newPosthogForTest(t), testenv.NewLogger(t))

	// When Mcp-Session-Id is absent on the inbound request — common for the
	// initial initialize handshake — the interceptor synthesizes a UUID for
	// the PostHog distinct ID rather than emitting an empty value.
	ctx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/abc",
	})

	require.NoError(t, interceptor.InterceptInitializeRequest(ctx, newInitializeRequest(t, "")))
}

func TestInitializePostHogEventInterceptor_UnauthenticatedPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewInitializePostHogEventInterceptor(newPosthogForTest(t), testenv.NewLogger(t))

	// No auth context — the interceptor still emits with authenticated=false
	// and an empty project_id rather than rejecting.
	ctx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/abc",
	})

	require.NoError(t, interceptor.InterceptInitializeRequest(ctx, newInitializeRequest(t, "session-anon")))
}
