package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	defaultBaseURL      = "https://api.chatgpt.com/v1/compliance"
	maxLogFileSize      = 15 * 1024 * 1024
	maxHTTPErrorMessage = 1000
)

var externalOrganizationIDPattern = regexp.MustCompile(`^org-[A-Za-z0-9_-]+$`)

type Client struct {
	httpClient             *guardian.HTTPClient
	baseURL                string
	apiKey                 string
	externalOrganizationID string
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

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		c.apiKey = apiKey
	}
}

func New(guardianPolicy *guardian.Policy, externalOrganizationID string, opts ...Option) *Client {
	if guardianPolicy == nil {
		panic("codex compliance client requires guardian policy")
	}
	c := &Client{
		httpClient:             guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig()),
		baseURL:                defaultBaseURL,
		apiKey:                 "",
		externalOrganizationID: strings.TrimSpace(externalOrganizationID),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type ListLogsParams struct {
	EventType string
	After     time.Time
	Limit     int
}

type LogsPage struct {
	Data        []LogFile `json:"data"`
	HasMore     bool      `json:"has_more"`
	LastEndTime time.Time `json:"last_end_time"`
}

type LogFile struct {
	ID         string    `json:"id"`
	EventType  string    `json:"event_type"`
	EndTime    time.Time `json:"end_time"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	FileSHA256 string    `json:"file_sha256"`
}

func (c *Client) ListLogs(ctx context.Context, params ListLogsParams) (*LogsPage, error) {
	endpoint, err := c.endpoint("logs")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.EventType != "" {
		q.Set("event_type", params.EventType)
	}
	if !params.After.IsZero() {
		q.Set("after", formatComplianceTimestamp(params.After))
	}
	endpoint.RawQuery = q.Encode()

	var page LogsPage
	if err := c.doJSON(ctx, endpoint, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) DownloadLog(ctx context.Context, logID string) ([]byte, error) {
	logID, err := validateCodexPathID("codex compliance log id", logID)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.endpoint("logs", logID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create codex compliance download request: %w", err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex compliance download request failed: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, newHTTPError(res)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxLogFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read codex compliance log: %w", err)
	}
	if len(body) > maxLogFileSize {
		return nil, fmt.Errorf("codex compliance log %s exceeds %d byte limit", logID, maxLogFileSize)
	}
	return body, nil
}

func (c *Client) doJSON(ctx context.Context, endpoint *url.URL, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create codex compliance request: %w", err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("codex compliance request failed: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return newHTTPError(res)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode codex compliance response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(parts ...string) (*url.URL, error) {
	if !externalOrganizationIDPattern.MatchString(c.externalOrganizationID) {
		return nil, fmt.Errorf("codex compliance external organization id must be an OpenAI organization ID starting with org-")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse codex compliance base url: %w", err)
	}
	path := []string{"organizations", c.externalOrganizationID}
	for _, part := range parts {
		part, err := validateCodexPathID("codex compliance path id", part)
		if err != nil {
			return nil, err
		}
		path = append(path, part)
	}
	return base.JoinPath(path...), nil
}

func validateCodexPathID(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("%s must be a single path segment", name)
	}
	return value, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func formatComplianceTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("codex compliance API returned %s: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("codex compliance API returned %s", e.Status)
}

func newHTTPError(res *http.Response) *HTTPError {
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxHTTPErrorMessage+1))
	message := strings.TrimSpace(string(body))
	if len(message) > maxHTTPErrorMessage {
		message = message[:maxHTTPErrorMessage]
	}
	return &HTTPError{
		StatusCode: res.StatusCode,
		Status:     res.Status,
		Body:       message,
	}
}
