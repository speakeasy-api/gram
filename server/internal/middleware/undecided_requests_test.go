package middleware

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, err //nolint:wrapcheck // test helper
	}
	return n, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newUndecidedTestLogger() (*slog.Logger, *lockedBuffer) {
	buf := &lockedBuffer{mu: sync.Mutex{}, buf: bytes.Buffer{}}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{AddSource: false, Level: slog.LevelInfo, ReplaceAttr: nil})), buf
}

func undecidedAuthContext(ctx context.Context, orgID string, sessionID *string) context.Context {
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

func newUndecidedEngine(t *testing.T) *authz.Engine {
	t.Helper()
	return authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
}

func TestUndecidedRequests_ReportsPreparedRequestWithNoDecision(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()
	engine := newUndecidedEngine(t)

	handler := UndecidedRequests(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := "session_123"
		// The demo org short-circuits grant loading, so preparing needs no DB.
		ctx := undecidedAuthContext(r.Context(), constants.DemoOrganizationID, &sessionID)
		if _, err := engine.PrepareContext(ctx); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/rpc/example.list", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	out := buf.String()
	require.Contains(t, out, "authenticated request made no authorization decision")
	require.Contains(t, out, "http.route=/rpc/example.list")
	require.Contains(t, out, "gram.authz.undecided.kind=no_decision")
}

func TestUndecidedRequests_SilentWhenAHandlerDecides(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()
	engine := newUndecidedEngine(t)

	handler := UndecidedRequests(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := "session_123"
		ctx := authz.GrantsToContext(undecidedAuthContext(r.Context(), "org_123", &sessionID), nil)
		_ = engine.Require(ctx, authz.Check{Scope: authz.ScopeLogRead, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil})
		w.WriteHeader(http.StatusForbidden)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/rpc/example.get", nil))

	require.Empty(t, buf.String())
}

func TestUndecidedRequests_SilentForUnauthenticatedRequests(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()

	handler := UndecidedRequests(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Empty(t, buf.String())
}

func TestUndecidedRequests_ReportsSkippedOnlyWithReason(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()
	engine := newUndecidedEngine(t)

	handler := UndecidedRequests(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No session: Require skips evaluation.
		ctx := undecidedAuthContext(r.Context(), "org_123", nil)
		if err := engine.Require(ctx, authz.Check{Scope: authz.ScopeLogRead, ResourceKind: "", ResourceID: "proj_1", Dimensions: nil}); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/some-slug/extra", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	out := buf.String()
	require.Contains(t, out, "request authorized only by skipped RBAC evaluation")
	require.Contains(t, out, "gram.authz.skip_reason=no_session")
	// Path segments after the first carry slugs and ids and are dropped.
	require.Contains(t, out, "http.route=/mcp/*")
	require.NotContains(t, out, "some-slug")
}

func TestUndecidedRequests_RateLimitsPerRoute(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reporter := &undecidedReporter{
		logger:   logger,
		interval: time.Minute,
		now:      func() time.Time { return now },
		mu:       sync.Mutex{},
		last:     make(map[undecidedKey]time.Time),
		count:    make(map[undecidedKey]int),
	}
	summary := authz.DecisionSummary{Enforced: 0, Skipped: 0, Operations: nil, Prepared: true, SkipReasons: nil}
	req := httptest.NewRequest(http.MethodPost, "/rpc/example.list", nil)

	reporter.observe(req, summary)
	reporter.observe(req, summary)
	reporter.observe(req, summary)
	require.Equal(t, 1, strings.Count(buf.String(), "no authorization decision"))

	now = now.Add(2 * time.Minute)
	reporter.observe(req, summary)
	out := buf.String()
	require.Equal(t, 2, strings.Count(out, "no authorization decision"))
	// The second line accounts for the requests the interval suppressed.
	require.Contains(t, out, "gram.authz.undecided.count=3")
}

func TestUndecidedRequests_BoundsDistinctRoutes(t *testing.T) {
	t.Parallel()
	logger, buf := newUndecidedTestLogger()
	reporter := &undecidedReporter{
		logger:   logger,
		interval: time.Hour,
		now:      time.Now,
		mu:       sync.Mutex{},
		last:     make(map[undecidedKey]time.Time),
		count:    make(map[undecidedKey]int),
	}
	summary := authz.DecisionSummary{Enforced: 0, Skipped: 0, Operations: nil, Prepared: true, SkipReasons: nil}

	for i := range undecidedMaxKeys + 50 {
		reporter.observe(httptest.NewRequest(http.MethodPost, fmt.Sprintf("/rpc/example.m%d", i), nil), summary)
	}

	require.LessOrEqual(t, len(reporter.count), undecidedMaxKeys+1)
	require.Contains(t, buf.String(), undecidedOverflowRoute)
}
