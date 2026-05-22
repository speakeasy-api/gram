package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	defaultBaseURL = "https://api.cursor.com"
	// Cursor supports a maximum page size of 500.
	defaultPageSize = 500
)

type Client struct {
	httpClient *guardian.HTTPClient
	baseURL    string
	pageSize   int
	apiKey     string
}

type Option func(*Client)

func WithHTTPClient(httpClient *guardian.HTTPClient) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

func WithPageSize(pageSize int) Option {
	return func(c *Client) {
		if pageSize > 0 {
			c.pageSize = pageSize
		}
	}
}

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		if apiKey != "" {
			c.apiKey = apiKey
		}
	}
}

func New(guardianPolicy *guardian.Policy, opts ...Option) *Client {
	if guardianPolicy == nil {
		panic("cursor client requires guardian policy")
	}
	c := &Client{
		httpClient: guardianPolicy.PooledClient(),
		baseURL:    defaultBaseURL,
		pageSize:   defaultPageSize,
		apiKey:     "",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type UsageEvent struct {
	Timestamp        time.Time  `json:"timestamp"`
	Model            string     `json:"model"`
	Kind             string     `json:"kind"`
	ChargedCents     float64    `json:"chargedCents"`
	MaxMode          bool       `json:"maxMode"`
	IsHeadless       bool       `json:"isHeadless"`
	IsTokenBasedCall bool       `json:"isTokenBasedCall"`
	TokenUsage       TokenUsage `json:"tokenUsage"`
	UserEmail        string     `json:"userEmail"`
}

func (e *UsageEvent) UnmarshalJSON(data []byte) error {
	type usageEvent UsageEvent
	var raw struct {
		Timestamp string `json:"timestamp"`
		usageEvent
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal cursor usage event: %w", err)
	}

	ms, err := strconv.ParseInt(raw.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("parse cursor timestamp %q: %w", raw.Timestamp, err)
	}

	*e = UsageEvent(raw.usageEvent)
	e.Timestamp = time.UnixMilli(ms).UTC()
	return nil
}

func (e UsageEvent) MarshalJSON() ([]byte, error) {
	type usageEvent UsageEvent
	data, err := json.Marshal(struct {
		Timestamp string `json:"timestamp"`
		usageEvent
	}{
		Timestamp:  strconv.FormatInt(e.Timestamp.UTC().UnixMilli(), 10),
		usageEvent: usageEvent(e),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cursor usage event: %w", err)
	}
	return data, nil
}

type TokenUsage struct {
	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CacheReadTokens  int64   `json:"cacheReadTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	TotalCents       float64 `json:"totalCents"`
}

type filteredUsageEventsRequest struct {
	StartDate int64 `json:"startDate"`
	EndDate   int64 `json:"endDate"`
	Page      int   `json:"page"`
	PageSize  int   `json:"pageSize"`
}

type filteredUsageEventsResponse struct {
	TotalUsageEventsCount int          `json:"totalUsageEventsCount"`
	Pagination            pagination   `json:"pagination"`
	UsageEvents           []UsageEvent `json:"usageEvents"`
}

type pagination struct {
	NumPages        int  `json:"numPages"`
	CurrentPage     int  `json:"currentPage"`
	PageSize        int  `json:"pageSize"`
	HasNextPage     bool `json:"hasNextPage"`
	HasPreviousPage bool `json:"hasPreviousPage"`
}

type FetchUsageEventsPageParams struct {
	Start time.Time
	End   time.Time
	Page  int
}

type UsageEventsPage struct {
	Events      []UsageEvent
	HasNextPage bool
}

func (c *Client) FetchUsageEventsPage(ctx context.Context, params FetchUsageEventsPageParams) (*UsageEventsPage, error) {
	if params.Page < 1 {
		return nil, fmt.Errorf("cursor usage page must be positive")
	}
	if !params.End.After(params.Start) {
		return &UsageEventsPage{
			Events:      nil,
			HasNextPage: false,
		}, nil
	}

	resp, err := c.fetchUsageEventsPage(ctx, filteredUsageEventsRequest{
		StartDate: params.Start.UTC().UnixMilli(),
		EndDate:   params.End.UTC().UnixMilli(),
		Page:      params.Page,
		PageSize:  c.pageSize,
	})
	if err != nil {
		return nil, err
	}

	return &UsageEventsPage{
		Events:      resp.UsageEvents,
		HasNextPage: resp.Pagination.HasNextPage,
	}, nil
}

func (c *Client) fetchUsageEventsPage(ctx context.Context, payload filteredUsageEventsRequest) (*filteredUsageEventsResponse, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse cursor base url: %w", err)
	}
	endpoint := base.JoinPath("/teams/filtered-usage-events")

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal cursor usage request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cursor usage request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.apiKey, "")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cursor usage request failed: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{
			Status:     res.Status,
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After")),
			Page:       payload.Page,
		}
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, &HTTPError{
			StatusCode: res.StatusCode,
			Status:     res.Status,
		}
	}

	var decoded filteredUsageEventsResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode cursor usage response: %w", err)
	}

	return &decoded, nil
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return max(time.Duration(seconds)*time.Second, 0)
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return max(time.Until(retryAt), 0)
	}
	return 0
}
