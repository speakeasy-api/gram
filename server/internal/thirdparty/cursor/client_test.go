package cursor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFetchUsageEventsPaginates(t *testing.T) {
	t.Parallel()

	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teams/filtered-usage-events" {
			t.Errorf("expected path /teams/filtered-usage-events, got %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			t.Errorf("expected basic auth")
		}
		if user != "cursor-key" {
			t.Errorf("expected basic auth user cursor-key, got %s", user)
		}
		if pass != "" {
			t.Errorf("expected empty basic auth password, got %s", pass)
		}

		var req filteredUsageEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		pages = append(pages, req.Page)
		if req.StartDate != int64(1710720000000) {
			t.Errorf("expected start date 1710720000000, got %d", req.StartDate)
		}
		if req.EndDate != int64(1710723600000) {
			t.Errorf("expected end date 1710723600000, got %d", req.EndDate)
		}
		if req.PageSize != 2 {
			t.Errorf("expected page size 2, got %d", req.PageSize)
		}

		resp := filteredUsageEventsResponse{
			TotalUsageEventsCount: 2,
			Pagination: pagination{
				NumPages:        2,
				CurrentPage:     req.Page,
				PageSize:        req.PageSize,
				HasNextPage:     req.Page == 1,
				HasPreviousPage: req.Page > 1,
			},
			UsageEvents: []UsageEvent{
				testUsageEvent(float64(req.Page)),
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithPageSize(2))
	events, err := client.FetchUsageEvents(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
	)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, pages)
	require.Len(t, events, 2)
	require.InDelta(t, float64(1), events[0].ChargedCents, 0.000001)
	require.InDelta(t, float64(2), events[1].ChargedCents, 0.000001)
}

func TestFetchUsageEventsPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req filteredUsageEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Page != 3 {
			t.Errorf("expected page 3, got %d", req.Page)
		}
		if req.PageSize != 2 {
			t.Errorf("expected page size 2, got %d", req.PageSize)
		}

		resp := filteredUsageEventsResponse{
			TotalUsageEventsCount: 1,
			Pagination: pagination{
				NumPages:        4,
				CurrentPage:     req.Page,
				PageSize:        req.PageSize,
				HasNextPage:     true,
				HasPreviousPage: true,
			},
			UsageEvents: []UsageEvent{
				testUsageEvent(3),
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithPageSize(2))
	page, err := client.FetchUsageEventsPage(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
		3,
	)
	require.NoError(t, err)
	require.True(t, page.HasNextPage)
	require.Len(t, page.Events, 1)
	require.InDelta(t, float64(3), page.Events[0].ChargedCents, 0.000001)
}

func TestFetchUsageEventsReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL))
	_, err := client.FetchUsageEvents(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
	)

	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	require.Equal(t, "429 Too Many Requests", rateLimitErr.Status)
	require.Equal(t, 7*time.Second, rateLimitErr.RetryAfter)
	require.Equal(t, 1, rateLimitErr.Page)
}

func TestTimestampTime(t *testing.T) {
	t.Parallel()

	ts, err := testUsageEvent(0).TimestampTime()
	require.NoError(t, err)
	require.Equal(t, int64(1710720000123), ts.UnixMilli())
}

func testGuardianPolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}

func testUsageEvent(chargedCents float64) UsageEvent {
	return UsageEvent{
		Timestamp:        "1710720000123",
		Model:            "claude",
		Kind:             "Usage-based",
		ChargedCents:     chargedCents,
		MaxMode:          false,
		IsHeadless:       false,
		IsTokenBasedCall: true,
		TokenUsage: TokenUsage{
			InputTokens:      1,
			OutputTokens:     2,
			CacheReadTokens:  3,
			CacheWriteTokens: 4,
			TotalCents:       chargedCents,
		},
		UserEmail: "dev@example.com",
	}
}
