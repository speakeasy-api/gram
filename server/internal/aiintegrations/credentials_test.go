package aiintegrations

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

const (
	testExternalOrgID = "org-test"
	testWorkspaceID   = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// providerStub stands in for every provider API the probes call, routed by
// path plus event_type (which separates the two workspace-scoped feeds).
type providerStub struct {
	server *httptest.Server
	mu     sync.Mutex
	routes []string
	// details keeps each request whole so tests can assert probe shape.
	details []string
}

type stubRoute struct {
	status int
	body   string
}

func newProviderStub(t *testing.T, routes map[string]stubRoute) *providerStub {
	t.Helper()

	stub := &providerStub{server: nil, mu: sync.Mutex{}, routes: nil, details: nil}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		if eventType := r.URL.Query().Get("event_type"); eventType != "" {
			route += "?event_type=" + eventType
		}

		detail := r.Method + " " + r.URL.Path
		if query := r.URL.Query().Encode(); query != "" {
			detail += "?" + query
		}
		if body, err := io.ReadAll(r.Body); err == nil && len(body) > 0 {
			detail += " " + string(body)
		}

		stub.mu.Lock()
		stub.routes = append(stub.routes, route)
		stub.details = append(stub.details, detail)
		stub.mu.Unlock()

		response, ok := routes[route]
		if !ok {
			response = stubRoute{status: http.StatusOK, body: "{}"}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		body := response.body
		if body == "" {
			body = "{}"
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *providerStub) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.routes...)
}

// requestDetails returns each request as "METHOD path[?query][ body]".
func (s *providerStub) requestDetails() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.details...)
}

// requireProbeShape asserts exactly one request matched path and carried
// every marker.
func requireProbeShape(t *testing.T, details []string, path string, markers ...string) {
	t.Helper()

	matched := make([]string, 0, 1)
	for _, detail := range details {
		if strings.Contains(detail, path) {
			matched = append(matched, detail)
		}
	}
	require.Len(t, matched, 1, "expected exactly one %s probe in %v", path, details)
	for _, marker := range markers {
		require.Contains(t, matched[0], marker)
	}
}

func newTestCredentialVerifier(t *testing.T, baseURL string) *CredentialVerifier {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	verifier := NewCredentialVerifier(testenv.NewLogger(t), policy)
	verifier.cursorBaseURL = baseURL
	verifier.anthropicBaseURL = baseURL
	verifier.codexBaseURL = baseURL
	return verifier
}

func TestVerifyCredentialsAcceptsAKeyEveryScheduleCanRead(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, nil)
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderChatGPTCompliance,
		APIKey:                 "chatgpt-key",
		ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
	})

	require.Empty(t, rejections)
	// Both workspace feeds are probed.
	require.ElementsMatch(t, []string{
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CONVERSATION_MESSAGE",
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CODEX_LOG",
	}, stub.requested())
}

// The incident this check exists for: one unentitled key, every feed refusing
// it, six failed polls.
func TestVerifyCredentialsReportsEveryRefusedChatGPTFeed(t *testing.T) {
	t.Parallel()

	body := `{"detail":"API key not authorized for enterprise logs"}`
	stub := newProviderStub(t, map[string]stubRoute{
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CONVERSATION_MESSAGE": {status: http.StatusForbidden, body: body},
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CODEX_LOG":            {status: http.StatusForbidden, body: body},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderChatGPTCompliance,
		APIKey:                 "chatgpt-key",
		ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
	})

	require.Len(t, rejections, 2)
	require.Equal(t, ScheduleChatGPTCompliance, rejections[0].Schedule)
	require.Equal(t, ScheduleCodexCloudSessions, rejections[1].Schedule)
	// The provider's own explanation is what fixes the key.
	require.Contains(t, rejections[0].Err.Error(), "API key not authorized for enterprise logs")
	require.Contains(t, rejections[0].Err.Error(), "403 Forbidden")
	require.Contains(t, rejections[0].Err.Error(), ScheduleChatGPTCompliance)
}

// Anthropic entitles Compliance and Admin Analytics separately, so a key can
// read one and not the other. Only the refused feed is reported.
func TestVerifyCredentialsReportsOnlyTheRefusedAnthropicFeed(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/v1/compliance/activities": {status: http.StatusForbidden, body: `{"error":{"message":"compliance api not enabled"}}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderAnthropicCompliance,
		APIKey:                 "anthropic-key",
		ExternalOrganizationID: conv.PtrEmpty(testExternalOrgID),
	})

	require.Len(t, rejections, 1)
	require.Equal(t, ScheduleAnthropicCompliance, rejections[0].Schedule)
	require.Contains(t, rejections[0].Err.Error(), "compliance api not enabled")
	require.ElementsMatch(t, []string{
		"/v1/compliance/activities",
		"/v1/organizations/analytics/user_usage_report",
		"/v1/organizations/analytics/user_cost_report",
	}, stub.requested())
}

func TestVerifyCredentialsReportsAnthropicKeyWithoutChatListScope(t *testing.T) {
	t.Parallel()

	// An Admin API key reads the activity feed but not chat content. The
	// compliance schedule cannot import anything with it, so the save must
	// surface Anthropic's refusal instead of failing on the first poll.
	stub := newProviderStub(t, map[string]stubRoute{
		"/v1/compliance/apps/chats": {status: http.StatusForbidden, body: `{"error":{"message":"Missing required scopes"}}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderAnthropicCompliance,
		APIKey:                 "anthropic-key",
		ExternalOrganizationID: conv.PtrEmpty(testExternalOrgID),
	})

	require.Len(t, rejections, 1)
	require.Equal(t, ScheduleAnthropicCompliance, rejections[0].Schedule)
	require.Contains(t, rejections[0].Err.Error(), "Missing required scopes")
	require.ElementsMatch(t, []string{
		"/v1/compliance/activities",
		"/v1/compliance/apps/chats",
		"/v1/organizations/analytics/user_usage_report",
		"/v1/organizations/analytics/user_cost_report",
	}, stub.requested())
}

func TestVerifyCredentialsReportsRefusedCursorKey(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/teams/filtered-usage-events": {status: http.StatusUnauthorized, body: `{"error":"invalid api key"}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	rejections := verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderCursor,
		APIKey:                 "cursor-key",
		ExternalOrganizationID: nil,
	})

	require.Len(t, rejections, 1)
	require.Equal(t, ScheduleCursor, rejections[0].Schedule)
	require.Contains(t, rejections[0].Err.Error(), "invalid api key")
}

// A bad minute says nothing about the credentials, so the probe stays silent.
// Cursor's client has no HTTP-level retries, which keeps this quick.
func TestVerifyCredentialsIgnoresProviderOutage(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/teams/filtered-usage-events": {status: http.StatusInternalServerError, body: `{"error":"boom"}`},
	})
	verifier := newTestCredentialVerifier(t, stub.server.URL)

	require.Empty(t, verifier.Verify(t.Context(), Credentials{
		Provider:               ProviderCursor,
		APIKey:                 "cursor-key",
		ExternalOrganizationID: nil,
	}))
}

func TestProviderRejectedCredentialsMatchesConfigurationRefusals(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		got, ok := providerRejectedCredentials(&codexapi.HTTPError{StatusCode: status, Status: "", Body: ""})
		require.True(t, ok, status)
		require.Equal(t, status, got)
	}

	// 400/422 on a fixed minimal probe complains about the probe, not the
	// credentials. Throttling and outages are equally inconclusive.
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError} {
		_, ok := providerRejectedCredentials(&anthropicapi.HTTPError{StatusCode: status, Status: "", Body: ""})
		require.False(t, ok, status)
	}

	_, ok := providerRejectedCredentials(&cursorapi.RateLimitError{Status: "429 Too Many Requests", RetryAfter: 0, Page: 1})
	require.False(t, ok)
}

// Probes collect a verdict, not data. A save waits on all of them inside a
// 20s budget, so one that widened into a real fetch would cost every save.
func TestCredentialProbesAskForASingleRecord(t *testing.T) {
	t.Parallel()

	probeAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	since := probeAt.Add(-time.Minute)

	t.Run("cursor asks for one usage event", func(t *testing.T) {
		t.Parallel()

		stub := newProviderStub(t, nil)
		verifier := newTestCredentialVerifier(t, stub.server.URL)
		verifier.now = func() time.Time { return probeAt }

		require.Empty(t, verifier.Verify(t.Context(), Credentials{
			Provider:               ProviderCursor,
			APIKey:                 "cursor-key",
			ExternalOrganizationID: nil,
		}))

		requireProbeShape(t, stub.requestDetails(), "/teams/filtered-usage-events",
			`"page":1`,
			`"pageSize":1`,
			`"startDate":`+strconv.FormatInt(since.UnixMilli(), 10),
			`"endDate":`+strconv.FormatInt(probeAt.UnixMilli(), 10),
		)
	})

	t.Run("anthropic asks for one activity and one analytics bucket", func(t *testing.T) {
		t.Parallel()

		stub := newProviderStub(t, nil)
		verifier := newTestCredentialVerifier(t, stub.server.URL)
		verifier.now = func() time.Time { return probeAt }

		require.Empty(t, verifier.Verify(t.Context(), Credentials{
			Provider:               ProviderAnthropicCompliance,
			APIKey:                 "anthropic-key",
			ExternalOrganizationID: conv.PtrEmpty(testExternalOrgID),
		}))

		details := stub.requestDetails()
		requireProbeShape(t, details, "/v1/compliance/activities",
			"limit=1",
			"activity_types%5B%5D=claude_chat_created",
			"created_at.gte="+url.QueryEscape(since.Format(time.RFC3339)),
		)
		// The chat list is the feed that reports new messages and sits
		// behind its own scope, so the key is probed against it too.
		requireProbeShape(t, details, "/v1/compliance/apps/chats",
			"limit=1",
			"order_by=updated_at",
			"updated_at.gte="+url.QueryEscape(since.Format(time.RFC3339)),
		)
		// One bucket, one minute wide.
		for _, report := range []string{"user_usage_report", "user_cost_report"} {
			requireProbeShape(t, details, "/v1/organizations/analytics/"+report,
				"limit=1",
				"bucket_width="+anthropicAnalyticsBucketWidth,
				"starting_at="+url.QueryEscape(since.Format(time.RFC3339)),
				"ending_at="+url.QueryEscape(probeAt.Format(time.RFC3339)),
			)
		}
	})

	t.Run("chatgpt asks for one log file per feed", func(t *testing.T) {
		t.Parallel()

		stub := newProviderStub(t, nil)
		verifier := newTestCredentialVerifier(t, stub.server.URL)
		verifier.now = func() time.Time { return probeAt }

		require.Empty(t, verifier.Verify(t.Context(), Credentials{
			Provider:               ProviderChatGPTCompliance,
			APIKey:                 "chatgpt-key",
			ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
		}))

		details := stub.requestDetails()
		for _, eventType := range []string{chatgptConversationEventType, codexCloudEventType} {
			requireProbeShape(t, details, "event_type="+eventType,
				"limit=1",
				"after="+url.QueryEscape(since.Format(time.RFC3339Nano)),
			)
		}
	})
}
