package cursor_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type filteredUsageEventsRequest struct {
	StartDate int64 `json:"startDate"`
	EndDate   int64 `json:"endDate"`
	Page      int   `json:"page"`
	PageSize  int   `json:"pageSize"`
}

type filteredUsageEventsResponse struct {
	Pagination  pagination          `json:"pagination"`
	UsageEvents []cursor.UsageEvent `json:"usageEvents"`
}

type pagination struct {
	NumPages    int  `json:"numPages"`
	CurrentPage int  `json:"currentPage"`
	PageSize    int  `json:"pageSize"`
	HasNextPage bool `json:"hasNextPage"`
}

func TestFetchUsageEventsPaginates(t *testing.T) {
	t.Parallel()

	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/teams/filtered-usage-events", r.URL.Path)
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "cursor-key", user)
		assert.Empty(t, pass)

		var req filteredUsageEventsRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		pages = append(pages, req.Page)
		assert.Equal(t, int64(1710720000000), req.StartDate)
		assert.Equal(t, int64(1710723600000), req.EndDate)
		assert.Equal(t, 2, req.PageSize)

		resp := filteredUsageEventsResponse{
			Pagination: pagination{
				NumPages:    2,
				CurrentPage: req.Page,
				PageSize:    req.PageSize,
				HasNextPage: req.Page == 1,
			},
			UsageEvents: []cursor.UsageEvent{
				{
					Timestamp:    "1710720000000",
					Model:        "claude",
					Kind:         "Usage-based",
					ChargedCents: float64(req.Page),
					UserEmail:    "dev@example.com",
				},
			},
		}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := cursor.New(policy, cursor.WithBaseURL(server.URL), cursor.WithPageSize(2))
	events, err := client.FetchUsageEvents(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
	)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, pages)
	require.Len(t, events, 2)
	require.InDelta(t, float64(1), events[0].ChargedCents, 0.0001)
	require.InDelta(t, float64(2), events[1].ChargedCents, 0.0001)
}

func TestFetchUsageEventsPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req filteredUsageEventsRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, 3, req.Page)
		assert.Equal(t, 2, req.PageSize)

		resp := filteredUsageEventsResponse{
			Pagination: pagination{
				CurrentPage: req.Page,
				PageSize:    req.PageSize,
				HasNextPage: true,
			},
			UsageEvents: []cursor.UsageEvent{
				{Timestamp: "1710720000000", ChargedCents: 3},
			},
		}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := cursor.New(policy, cursor.WithBaseURL(server.URL), cursor.WithPageSize(2))
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
	require.InDelta(t, float64(3), page.Events[0].ChargedCents, 0.0001)
}

func TestFetchUsageEventsReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	client := cursor.New(policy, cursor.WithBaseURL(server.URL))
	_, err = client.FetchUsageEvents(
		t.Context(),
		"cursor-key",
		time.UnixMilli(1710720000000),
		time.UnixMilli(1710723600000),
	)

	var rateLimitErr *cursor.RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	require.Equal(t, "429 Too Many Requests", rateLimitErr.Status)
	require.Equal(t, 7*time.Second, rateLimitErr.RetryAfter)
	require.Equal(t, 1, rateLimitErr.Page)
}

func TestTimestampTime(t *testing.T) {
	t.Parallel()

	ts, err := cursor.UsageEvent{Timestamp: "1710720000123"}.TimestampTime()
	require.NoError(t, err)
	require.Equal(t, int64(1710720000123), ts.UnixMilli())
}
