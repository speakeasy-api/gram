package anthropic

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetUserUsageReportSendsFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/organizations/analytics/user_usage_report" {
			t.Errorf("expected path /v1/organizations/analytics/user_usage_report, got %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			t.Errorf("expected x-api-key anthropic-key, got %s", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("expected anthropic-version 2023-06-01, got %s", got)
		}
		if got := r.URL.Query().Get("starting_at"); got != "2026-07-15T10:00:00Z" {
			t.Errorf("expected starting_at 2026-07-15T10:00:00Z, got %s", got)
		}
		if got := r.URL.Query().Get("ending_at"); got != "2026-07-16T10:00:00Z" {
			t.Errorf("expected ending_at 2026-07-16T10:00:00Z, got %s", got)
		}
		if got := r.URL.Query().Get("bucket_width"); got != "1m" {
			t.Errorf("expected bucket_width 1m, got %s", got)
		}
		if got := r.URL.Query()["products[]"]; !slices.Equal(got, []string{"chat"}) {
			t.Errorf("expected products[] [chat], got %v", got)
		}
		if got := r.URL.Query()["group_by[]"]; !slices.Equal(got, []string{"model"}) {
			t.Errorf("expected group_by[] [model], got %v", got)
		}
		if got := r.URL.Query().Get("limit"); got != "1000" {
			t.Errorf("expected limit 1000, got %s", got)
		}

		switch r.URL.Query().Get("page") {
		case "":
			_ = json.NewEncoder(w).Encode(UserUsageReportPage{
				Data: []UserUsageRow{{
					Actor:                AnalyticsActor{UserID: "user_1", Email: new("dev@example.com"), Name: new("Dev"), Deleted: false},
					StartingAt:           "2026-07-15T10:00:00Z",
					EndingAt:             "2026-07-15T10:01:00Z",
					Model:                "claude-opus-4-8",
					Product:              "",
					UncachedInputTokens:  100,
					OutputTokens:         50,
					CacheReadInputTokens: 3200,
					CacheCreation:        AnalyticsCacheCreation{Ephemeral1hInputTokens: 1000, Ephemeral5mInputTokens: 500},
					TotalTokens:          4850,
					Requests:             2,
				}},
				DataRefreshedAt: "2026-07-16T09:00:00Z",
				HasMore:         true,
				NextPage:        "page_2",
				OrganizationID:  "org_1",
			})
		case "page_2":
			_ = json.NewEncoder(w).Encode(UserUsageReportPage{
				Data: []UserUsageRow{{
					Actor:                AnalyticsActor{UserID: "user_2", Email: nil, Name: nil, Deleted: true},
					StartingAt:           "2026-07-15T10:02:00Z",
					EndingAt:             "2026-07-15T10:03:00Z",
					Model:                "claude-sonnet-5",
					Product:              "",
					UncachedInputTokens:  10,
					OutputTokens:         5,
					CacheReadInputTokens: 0,
					CacheCreation:        AnalyticsCacheCreation{Ephemeral1hInputTokens: 0, Ephemeral5mInputTokens: 0},
					TotalTokens:          15,
					Requests:             1,
				}},
				DataRefreshedAt: "2026-07-16T09:00:00Z",
				HasMore:         false,
				NextPage:        "",
				OrganizationID:  "org_1",
			})
		default:
			t.Errorf("unexpected page cursor %q", r.URL.Query().Get("page"))
		}
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("anthropic-key"))

	first, err := client.GetUserUsageReport(t.Context(), UserAnalyticsReportParams{
		StartingAt:  time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		EndingAt:    time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		BucketWidth: "1m",
		Products:    []string{"chat"},
		GroupBy:     []string{"model"},
		Limit:       1000,
		Page:        "",
	})
	require.NoError(t, err)
	require.True(t, first.HasMore)
	require.Len(t, first.Data, 1)
	require.Equal(t, "user_1", first.Data[0].Actor.UserID)
	require.Equal(t, int64(100), first.Data[0].UncachedInputTokens)

	second, err := client.GetUserUsageReport(t.Context(), UserAnalyticsReportParams{
		StartingAt:  time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		EndingAt:    time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		BucketWidth: "1m",
		Products:    []string{"chat"},
		GroupBy:     []string{"model"},
		Limit:       1000,
		Page:        first.NextPage,
	})
	require.NoError(t, err)
	require.False(t, second.HasMore)
	require.True(t, second.Data[0].Actor.Deleted)
	require.Nil(t, second.Data[0].Actor.Email)
	require.Equal(t, 2, requests)

	bucketStart, err := second.Data[0].StartingAtTime()
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 15, 10, 2, 0, 0, time.UTC), bucketStart)
}

func TestGetUserCostReportDecodesAmounts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/analytics/user_cost_report" {
			t.Errorf("expected path /v1/organizations/analytics/user_cost_report, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(UserCostReportPage{
			Data: []UserCostRow{{
				Actor:      AnalyticsActor{UserID: "user_1", Email: new("dev@example.com"), Name: new("Dev"), Deleted: false},
				StartingAt: "2026-07-15T10:00:00Z",
				EndingAt:   "2026-07-15T10:01:00Z",
				Model:      "claude-opus-4-8",
				Product:    "",
				Amount:     "41280.000000",
				ListAmount: "51600.000000",
				Currency:   "USD",
				Requests:   2,
			}},
			DataRefreshedAt: "2026-07-16T09:00:00Z",
			HasMore:         false,
			NextPage:        "",
			OrganizationID:  "org_1",
		})
	}))
	t.Cleanup(server.Close)

	client := New(testGuardianPolicy(t), WithBaseURL(server.URL), WithAPIKey("anthropic-key"))
	page, err := client.GetUserCostReport(t.Context(), UserAnalyticsReportParams{
		StartingAt:  time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		EndingAt:    time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		BucketWidth: "1m",
		Products:    []string{"chat"},
		GroupBy:     []string{"model"},
		Limit:       1000,
		Page:        "",
	})
	require.NoError(t, err)
	require.Len(t, page.Data, 1)

	amountUSD, err := page.Data[0].AmountUSD()
	require.NoError(t, err)
	require.InDelta(t, 412.80, amountUSD, 0.0001)
}

func TestUserCostRowAmountUSDEmptyAmount(t *testing.T) {
	t.Parallel()

	row := UserCostRow{
		Actor:      AnalyticsActor{UserID: "user_1", Email: nil, Name: nil, Deleted: false},
		StartingAt: "",
		EndingAt:   "",
		Model:      "",
		Product:    "",
		Amount:     "",
		ListAmount: "",
		Currency:   "",
		Requests:   0,
	}
	amountUSD, err := row.AmountUSD()
	require.NoError(t, err)
	require.Zero(t, amountUSD)
}

// The cents-to-dollars shift must round exactly once, onto float64. This
// amount is a counterexample to the ParseFloat-then-divide approach: parsing
// "575398922.098702" to float64 and dividing by 100 lands one ULP below the
// correctly rounded value (5753989.220987019... instead of
// 5753989.22098702).
func TestUserCostRowAmountUSDRoundsOnce(t *testing.T) {
	t.Parallel()

	row := UserCostRow{
		Actor:      AnalyticsActor{UserID: "user_1", Email: nil, Name: nil, Deleted: false},
		StartingAt: "",
		EndingAt:   "",
		Model:      "",
		Product:    "",
		Amount:     "575398922.098702",
		ListAmount: "",
		Currency:   "USD",
		Requests:   0,
	}
	amountUSD, err := row.AmountUSD()
	require.NoError(t, err)
	// Bit-exact comparison on purpose: a 1-ULP drift is precisely the bug
	// this test guards against, so approximate float assertions cannot.
	require.Equal(t, math.Float64bits(5753989.22098702), math.Float64bits(amountUSD))
}

func TestUserCostRowAmountUSDInvalidAmount(t *testing.T) {
	t.Parallel()

	row := UserCostRow{
		Actor:      AnalyticsActor{UserID: "user_1", Email: nil, Name: nil, Deleted: false},
		StartingAt: "",
		EndingAt:   "",
		Model:      "",
		Product:    "",
		Amount:     "not-a-number",
		ListAmount: "",
		Currency:   "USD",
		Requests:   0,
	}
	_, err := row.AmountUSD()
	require.ErrorContains(t, err, "parse cost row amount")
}
