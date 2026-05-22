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

func TestFetchUsageEventsPageSendsAuthAndRequest(t *testing.T) {
	t.Parallel()

	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teams/filtered-usage-events" {
			t.Errorf("expected path /teams/filtered-usage-events, got %s", r.URL.Path)
		}
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Errorf("expected basic auth")
		}
		if username != "cursor-key" {
			t.Errorf("expected basic auth username cursor-key, got %s", username)
		}
		if password != "" {
			t.Errorf("expected empty basic auth password, got %s", password)
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

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithPageSize(2), WithAPIKey("cursor-key"))
	eventsPage, err := client.FetchUsageEventsPage(
		t.Context(),
		FetchUsageEventsPageParams{
			Start: time.UnixMilli(1710720000000),
			End:   time.UnixMilli(1710723600000),
			Page:  1,
		},
	)
	require.NoError(t, err)
	require.Equal(t, []int{1}, pages)
	require.Len(t, eventsPage.Events, 1)
	require.Equal(t, 2, eventsPage.TotalUsageEventsCount)
	require.Equal(t, 1, eventsPage.CurrentPage)
	require.Equal(t, 2, eventsPage.NumPages)
	require.Equal(t, 2, eventsPage.PageSize)
	require.InDelta(t, float64(1), eventsPage.Events[0].ChargedCents, 0.000001)
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

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithPageSize(2), WithAPIKey("cursor-key"))
	page, err := client.FetchUsageEventsPage(
		t.Context(),
		FetchUsageEventsPageParams{
			Start: time.UnixMilli(1710720000000),
			End:   time.UnixMilli(1710723600000),
			Page:  3,
		},
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
	_, err := client.FetchUsageEventsPage(
		t.Context(),
		FetchUsageEventsPageParams{
			Start: time.UnixMilli(1710720000000),
			End:   time.UnixMilli(1710723600000),
			Page:  1,
		},
	)
	require.Error(t, err)
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
}

func TestFetchUsageEventsReturnsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL))
	_, err := client.FetchUsageEventsPage(
		t.Context(),
		FetchUsageEventsPageParams{
			Start: time.UnixMilli(1710720000000),
			End:   time.UnixMilli(1710723600000),
			Page:  1,
		},
	)

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
	require.Equal(t, "401 Unauthorized", httpErr.Status)
}

func TestUsageEventUnmarshalsTimestamp(t *testing.T) {
	t.Parallel()

	var event UsageEvent
	err := json.Unmarshal([]byte(`{
		"timestamp": "1710720000123",
		"model": "claude",
		"kind": "Usage-based",
		"chargedCents": 0,
		"maxMode": false,
		"isHeadless": false,
		"isTokenBasedCall": true,
		"tokenUsage": {
			"inputTokens": 1,
			"outputTokens": 2,
			"cacheReadTokens": 3,
			"cacheWriteTokens": 4,
			"totalCents": 0
		},
		"userEmail": "dev@example.com"
	}`), &event)
	require.NoError(t, err)
	require.Equal(t, int64(1710720000123), event.Timestamp.UnixMilli())
}

func TestFilteredUsageEventsResponseUnmarshalsDocsShape(t *testing.T) {
	t.Parallel()

	var resp filteredUsageEventsResponse
	err := json.Unmarshal([]byte(`{
		"totalUsageEventsCount": 113,
		"pagination": {
			"numPages": 12,
			"currentPage": 1,
			"pageSize": 10,
			"hasNextPage": true,
			"hasPreviousPage": false
		},
		"usageEvents": [
			{
				"timestamp": "1750979225854",
				"userEmail": "developer@company.com",
				"model": "claude-4.5-sonnet",
				"kind": "Usage-based",
				"maxMode": true,
				"requestsCosts": 5,
				"isTokenBasedCall": true,
				"isChargeable": true,
				"isHeadless": false,
				"tokenUsage": {
					"inputTokens": 126,
					"outputTokens": 450,
					"cacheWriteTokens": 6112,
					"cacheReadTokens": 11964,
					"totalCents": 20.18232
				},
				"chargedCents": 21.36232,
				"cursorTokenFee": 1.18,
				"isFreeBugbot": false
			},
			{
				"timestamp": "1750978339901",
				"userEmail": "admin@company.com",
				"model": "claude-4-sonnet-thinking",
				"kind": "Included in Business",
				"maxMode": true,
				"requestsCosts": 1.4,
				"isTokenBasedCall": false,
				"isChargeable": false,
				"isHeadless": false,
				"chargedCents": 8,
				"isFreeBugbot": false
			}
		],
		"period": {
			"startDate": 1748411762359,
			"endDate": 1751003762359
		}
	}`), &resp)
	require.NoError(t, err)
	require.Equal(t, 113, resp.TotalUsageEventsCount)
	require.Equal(t, 12, resp.Pagination.NumPages)
	require.True(t, resp.Pagination.HasNextPage)
	require.Len(t, resp.UsageEvents, 2)
	require.Equal(t, "developer@company.com", resp.UsageEvents[0].UserEmail)
	require.Equal(t, int64(1750979225854), resp.UsageEvents[0].Timestamp.UnixMilli())
	require.InDelta(t, float64(21.36232), resp.UsageEvents[0].ChargedCents, 0.000001)
	require.Equal(t, int64(126), resp.UsageEvents[0].TokenUsage.InputTokens)
	require.Equal(t, "admin@company.com", resp.UsageEvents[1].UserEmail)
	require.False(t, resp.UsageEvents[1].IsTokenBasedCall)
	require.Zero(t, resp.UsageEvents[1].TokenUsage.InputTokens)
}

func testGuardianPolicy(t *testing.T) *guardian.Policy {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return policy
}

func testUsageEvent(chargedCents float64) UsageEvent {
	return UsageEvent{
		Timestamp:        time.UnixMilli(1710720000123).UTC(),
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
