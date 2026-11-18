package catalogpromotion

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGitHubFailuresClassifyWithoutExposingProviderContent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		kind    ErrorKind
		delay   time.Duration
	}{
		{name: "quota reset", status: 403, headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now.Add(2*time.Minute).Unix(), 10)}, kind: ErrorTransient, delay: 2 * time.Minute},
		{name: "retry seconds", status: 429, headers: map[string]string{"Retry-After": "30"}, kind: ErrorTransient, delay: 30 * time.Second},
		{name: "retry HTTP date", status: 429, headers: map[string]string{"Retry-After": now.Add(3 * time.Minute).Format(http.TimeFormat)}, kind: ErrorTransient, delay: 3 * time.Minute},
		{name: "respect later quota reset", status: 429, headers: map[string]string{"Retry-After": "30", "X-RateLimit-Reset": strconv.FormatInt(now.Add(2*time.Minute).Unix(), 10)}, kind: ErrorTransient, delay: 2 * time.Minute},
		{name: "secondary no header", status: 403, body: `{"message":"You have exceeded a secondary rate limit. PRIVATE_PROVIDER_TEXT"}`, kind: ErrorTransient, delay: time.Minute},
		{name: "missing retry hint", status: 429, kind: ErrorTransient, delay: time.Minute},
		{name: "malformed retry hint", status: 429, headers: map[string]string{"Retry-After": "bad"}, kind: ErrorTransient, delay: time.Minute},
		{name: "authorization reset is not quota", status: 403, headers: map[string]string{"X-RateLimit-Remaining": "99", "X-RateLimit-Reset": strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}, kind: ErrorConfiguration},
		{name: "expired token", status: 401, kind: ErrorConfiguration},
		{name: "repository inaccessible", status: 404, kind: ErrorConfiguration},
		{name: "invalid input", status: 422, kind: ErrorPermanent},
		{name: "server outage", status: 503, headers: map[string]string{"Retry-After": "15", "X-RateLimit-Reset": strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}, kind: ErrorTransient, delay: 15 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				for name, value := range tt.headers {
					w.Header().Set(name, value)
				}
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.body)
			})
			c.now = func() time.Time { return now }
			_, err := c.EnsureGuide(t.Context(), Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"})
			require.Error(t, err)
			kind, delay := Classify(err)
			require.Equal(t, tt.kind, kind)
			require.Equal(t, tt.delay, delay)
			require.NotContains(t, err.Error(), "PRIVATE_PROVIDER_TEXT")
			require.NotContains(t, err.Error(), "test-token")
		})
	}
}

func TestEnsureGuideCancellationStopsInFlightReconciliation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.EnsureGuide(ctx, Guide{Slug: "example", Name: "Example", RemoteURL: "https://mcp.example.com/mcp", DocumentationURL: "https://docs.example.com/mcp", AuthMode: "none"})
		result <- err
	}()
	<-started
	cancel()
	err := <-result
	require.ErrorIs(t, err, context.Canceled)
	kind, _ := Classify(err)
	require.Equal(t, ErrorTransient, kind)
	require.EqualValues(t, 1, calls.Load())
}

func TestEnsureGuideExpiredCallerDeadlineDoesNotStartRequest(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("expired context started a request") })
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancel()
	_, err := c.EnsureGuide(ctx, Guide{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGuideValidationAndConfigurationNeedAttention(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid delivery reached GitHub") })
	_, err := c.EnsureGuide(t.Context(), Guide{})
	kind, _ := Classify(err)
	require.Equal(t, ErrorPermanent, kind)
	c.config.GitHubRepository = "invalid"
	_, err = c.EnsureGuide(t.Context(), Guide{})
	kind, _ = Classify(err)
	require.Equal(t, ErrorConfiguration, kind)
	kind, _ = Classify(fmt.Errorf("wrapped: %w", ErrNotConfigured))
	require.Equal(t, ErrorConfiguration, kind)
	require.ErrorIs(t, fmt.Errorf("wrapped: %w", ErrNotConfigured), ErrNotConfigured)
}

func TestReconciliationPageBoundsNeedAttention(t *testing.T) {
	t.Parallel()
	for _, events := range []bool{false, true} {
		t.Run(strconv.FormatBool(events), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			page := "[" + strings.Repeat("{},", pageSize-1) + "{}]"
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet {
					t.Error("page limit must not create an issue or label")
				}
				_, _ = fmt.Fprint(w, page)
			})
			var err error
			if events {
				_, err = c.wasRequested(t.Context(), 42)
			} else {
				_, err = c.findIssue(t.Context(), "absent", "absent")
			}
			require.Error(t, err)
			kind, _ := Classify(err)
			require.Equal(t, ErrorPermanent, kind)
			require.EqualValues(t, maxPages, calls.Load())
		})
	}
}
