package cursor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFetchUsageEventsPaginates(t *testing.T) {
	t.Parallel()

	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/teams/filtered-usage-events", r.URL.Path)
		user, pass, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "cursor-key", user)
		require.Equal(t, "", pass)

		var req filteredUsageEventsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		pages = append(pages, req.Page)
		require.Equal(t, int64(1710720000000), req.StartDate)
		require.Equal(t, int64(1710723600000), req.EndDate)
		require.Equal(t, 2, req.PageSize)

		resp := filteredUsageEventsResponse{
			Pagination: pagination{
				NumPages:    2,
				CurrentPage: req.Page,
				PageSize:    req.PageSize,
				HasNextPage: req.Page == 1,
			},
			UsageEvents: []UsageEvent{
				{
					Timestamp:    "1710720000000",
					Model:        "claude",
					Kind:         "Usage-based",
					ChargedCents: float64(req.Page),
					UserEmail:    "dev@example.com",
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)

	client := New(WithBaseURL(server.URL), WithPageSize(2))
	events, err := client.FetchUsageEvents(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
	)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, pages)
	require.Len(t, events, 2)
	require.Equal(t, float64(1), events[0].ChargedCents)
	require.Equal(t, float64(2), events[1].ChargedCents)
}

func TestFetchUsageEventsPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req filteredUsageEventsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, 3, req.Page)
		require.Equal(t, 2, req.PageSize)

		resp := filteredUsageEventsResponse{
			Pagination: pagination{
				CurrentPage: req.Page,
				PageSize:    req.PageSize,
				HasNextPage: true,
			},
			UsageEvents: []UsageEvent{
				{Timestamp: "1710720000000", ChargedCents: 3},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)

	client := New(WithBaseURL(server.URL), WithPageSize(2))
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
	require.Equal(t, float64(3), page.Events[0].ChargedCents)
}

func TestFetchUsageEventsReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := New(WithBaseURL(server.URL))
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

	ts, err := UsageEvent{Timestamp: "1710720000123"}.TimestampTime()
	require.NoError(t, err)
	require.Equal(t, int64(1710720000123), ts.UnixMilli())
}
