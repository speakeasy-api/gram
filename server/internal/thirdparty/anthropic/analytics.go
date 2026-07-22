package anthropic

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Admin Analytics API (https://platform.claude.com/docs/en/api/admin/analytics).
// Both endpoints require a Claude Enterprise plan and an API key carrying the
// read:analytics scope; other organizations receive 403s.

// AnalyticsActor identifies the seat user a usage/cost row is attributed to.
type AnalyticsActor struct {
	UserID  string  `json:"user_id"`
	Email   *string `json:"email"`
	Name    *string `json:"name"`
	Deleted bool    `json:"deleted"`
}

// AnalyticsCacheCreation breaks cache-write tokens out by ephemeral TTL tier.
type AnalyticsCacheCreation struct {
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
}

// UserUsageRow is one (actor x time bucket x group_by dimensions) token usage
// row from the user_usage_report endpoint.
type UserUsageRow struct {
	Actor                AnalyticsActor         `json:"actor"`
	StartingAt           string                 `json:"starting_at"`
	EndingAt             string                 `json:"ending_at"`
	Model                string                 `json:"model"`
	Product              string                 `json:"product"`
	UncachedInputTokens  int64                  `json:"uncached_input_tokens"`
	OutputTokens         int64                  `json:"output_tokens"`
	CacheReadInputTokens int64                  `json:"cache_read_input_tokens"`
	CacheCreation        AnalyticsCacheCreation `json:"cache_creation"`
	TotalTokens          int64                  `json:"total_tokens"`
	Requests             int64                  `json:"requests"`
}

func (r UserUsageRow) StartingAtTime() (time.Time, error) {
	t, err := time.Parse(time.RFC3339, r.StartingAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse usage row starting_at: %w", err)
	}
	return t.UTC(), nil
}

// UserCostRow is one (actor x time bucket x group_by dimensions) cost row from
// the user_cost_report endpoint. Amount is post-discount, pre-credit, in
// fractional cents (minor units), e.g. "41280.000000" is $412.80.
type UserCostRow struct {
	Actor      AnalyticsActor `json:"actor"`
	StartingAt string         `json:"starting_at"`
	EndingAt   string         `json:"ending_at"`
	Model      string         `json:"model"`
	Product    string         `json:"product"`
	Amount     string         `json:"amount"`
	ListAmount string         `json:"list_amount"`
	Currency   string         `json:"currency"`
	Requests   int64          `json:"requests"`
}

// AmountUSD converts the fractional-cents amount to US dollars. The API
// reports fixed-point decimal strings, so the cents-to-dollars shift happens
// in exact decimal arithmetic and the value is rounded exactly once — onto
// float64, the telemetry storage type. Parsing into binary floating point
// first and then dividing would round twice, drifting up to an extra ULP.
func (r UserCostRow) AmountUSD() (float64, error) {
	if r.Amount == "" {
		return 0, nil
	}
	cents, ok := new(big.Rat).SetString(r.Amount)
	if !ok {
		return 0, fmt.Errorf("parse cost row amount: %q", r.Amount)
	}
	usd, _ := cents.Quo(cents, big.NewRat(100, 1)).Float64()
	return usd, nil
}

type UserUsageReportPage struct {
	Data            []UserUsageRow `json:"data"`
	DataRefreshedAt string         `json:"data_refreshed_at"`
	HasMore         bool           `json:"has_more"`
	NextPage        string         `json:"next_page"`
	OrganizationID  string         `json:"organization_id"`
}

type UserCostReportPage struct {
	Data            []UserCostRow `json:"data"`
	DataRefreshedAt string        `json:"data_refreshed_at"`
	HasMore         bool          `json:"has_more"`
	NextPage        string        `json:"next_page"`
	OrganizationID  string        `json:"organization_id"`
}

// UserAnalyticsReportParams are shared by the usage and cost report endpoints.
// StartingAt is inclusive, EndingAt exclusive. With BucketWidth "1m" the range
// may span at most 24 hours per request.
type UserAnalyticsReportParams struct {
	StartingAt  time.Time
	EndingAt    time.Time
	BucketWidth string
	Products    []string
	GroupBy     []string
	Limit       int
	Page        string
}

func (c *Client) GetUserUsageReport(ctx context.Context, params UserAnalyticsReportParams) (*UserUsageReportPage, error) {
	endpoint, err := c.analyticsEndpoint("/v1/organizations/analytics/user_usage_report", params)
	if err != nil {
		return nil, err
	}
	var page UserUsageReportPage
	if err := c.doJSON(ctx, http.MethodGet, endpoint, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) GetUserCostReport(ctx context.Context, params UserAnalyticsReportParams) (*UserCostReportPage, error) {
	endpoint, err := c.analyticsEndpoint("/v1/organizations/analytics/user_cost_report", params)
	if err != nil {
		return nil, err
	}
	var page UserCostReportPage
	if err := c.doJSON(ctx, http.MethodGet, endpoint, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) analyticsEndpoint(path string, params UserAnalyticsReportParams) (*url.URL, error) {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("starting_at", params.StartingAt.UTC().Format(time.RFC3339))
	if !params.EndingAt.IsZero() {
		q.Set("ending_at", params.EndingAt.UTC().Format(time.RFC3339))
	}
	if params.BucketWidth != "" {
		q.Set("bucket_width", params.BucketWidth)
	}
	for _, product := range params.Products {
		q.Add("products[]", product)
	}
	for _, dim := range params.GroupBy {
		q.Add("group_by[]", dim)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Page != "" {
		q.Set("page", params.Page)
	}
	endpoint.RawQuery = q.Encode()
	return endpoint, nil
}
