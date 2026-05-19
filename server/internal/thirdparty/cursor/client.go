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
	defaultBaseURL  = "https://api.cursor.com"
	defaultPageSize = 100
)

type Client struct {
	httpClient *guardian.HTTPClient
	baseURL    string
	pageSize   int
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

func New(guardianPolicy *guardian.Policy, opts ...Option) *Client {
	if guardianPolicy == nil {
		panic("cursor client requires guardian policy")
	}
	c := &Client{
		httpClient: guardianPolicy.PooledClient(),
		baseURL:    defaultBaseURL,
		pageSize:   defaultPageSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type UsageEvent struct {
	Timestamp        string     `json:"timestamp"`
	Model            string     `json:"model"`
	Kind             string     `json:"kind"`
	ChargedCents     float64    `json:"chargedCents"`
	MaxMode          bool       `json:"maxMode"`
	IsHeadless       bool       `json:"isHeadless"`
	IsTokenBasedCall bool       `json:"isTokenBasedCall"`
	TokenUsage       TokenUsage `json:"tokenUsage"`
	UserEmail        string     `json:"userEmail"`
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

type UsageEventsPage struct {
	Events      []UsageEvent
	HasNextPage bool
}

func (c *Client) FetchUsageEvents(ctx context.Context, apiKey string, start, end time.Time) ([]UsageEvent, error) {
	var events []UsageEvent
	page := 1
	for {
		resp, err := c.FetchUsageEventsPage(ctx, apiKey, start, end, page)
		if err != nil {
			return nil, err
		}
		events = append(events, resp.Events...)

		if !resp.HasNextPage {
			break
		}
		page++
	}

	return events, nil
}

func (c *Client) FetchUsageEventsPage(ctx context.Context, apiKey string, start, end time.Time, page int) (*UsageEventsPage, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("cursor api key is required")
	}
	if page < 1 {
		return nil, fmt.Errorf("cursor usage page must be positive")
	}
	if !end.After(start) {
		return &UsageEventsPage{
			Events:      nil,
			HasNextPage: false,
		}, nil
	}

	resp, err := c.fetchUsageEventsPage(ctx, apiKey, filteredUsageEventsRequest{
		StartDate: start.UTC().UnixMilli(),
		EndDate:   end.UTC().UnixMilli(),
		Page:      page,
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

type RateLimitError struct {
	Status     string
	RetryAfter time.Duration
	Page       int
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("cursor usage request rate limited with status %s; retry after %s", e.Status, e.RetryAfter)
	}
	return fmt.Sprintf("cursor usage request rate limited with status %s", e.Status)
}

func (c *Client) fetchUsageEventsPage(ctx context.Context, apiKey string, payload filteredUsageEventsRequest) (*filteredUsageEventsResponse, error) {
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
	req.SetBasicAuth(apiKey, "")

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
		return nil, fmt.Errorf("cursor usage request failed with status %s", res.Status)
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

func (e UsageEvent) TimestampTime() (time.Time, error) {
	ms, err := strconv.ParseInt(e.Timestamp, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cursor timestamp %q: %w", e.Timestamp, err)
	}
	return time.UnixMilli(ms).UTC(), nil
}
