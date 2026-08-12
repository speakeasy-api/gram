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

var (
	externalOrganizationIDPattern = regexp.MustCompile(`^org-[A-Za-z0-9_-]+$`)
	workspaceIDPattern            = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Compliance resources are served under two path scopes: API organizations
// (organizations/org-…, e.g. Codex COSTS files) and ChatGPT workspaces
// (workspaces/<uuid>, e.g. CONVERSATION_MESSAGE files). A client is bound to
// one scope at construction.
type scope struct {
	prefix  string
	id      string
	pattern *regexp.Regexp
	// name and label compose the invalid-id error:
	// "<errPrefix> <name> must be <label>".
	name  string
	label string
	// errPrefix names the integration in every error this client surfaces,
	// so workspace-scoped (ChatGPT) failures are not misattributed to Codex
	// in logs and Temporal failure details.
	errPrefix string
}

type Client struct {
	httpClient *guardian.HTTPClient
	baseURL    string
	apiKey     string
	scope      scope
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

// New returns a client scoped to an OpenAI API organization (org-… id),
// the scope Codex COSTS files are served under.
func New(guardianPolicy *guardian.Policy, externalOrganizationID string, opts ...Option) *Client {
	return newScoped(guardianPolicy, scope{
		prefix:    "organizations",
		id:        strings.TrimSpace(externalOrganizationID),
		pattern:   externalOrganizationIDPattern,
		name:      "external organization id",
		label:     "an OpenAI organization ID starting with org-",
		errPrefix: "codex compliance",
	}, opts...)
}

// NewWorkspaceClient returns a client scoped to a ChatGPT workspace (UUID),
// the scope conversation/audit/auth log files are served under.
func NewWorkspaceClient(guardianPolicy *guardian.Policy, workspaceID string, opts ...Option) *Client {
	return newScoped(guardianPolicy, scope{
		prefix:    "workspaces",
		id:        strings.TrimSpace(workspaceID),
		pattern:   workspaceIDPattern,
		name:      "workspace id",
		label:     "a ChatGPT workspace UUID",
		errPrefix: "chatgpt compliance",
	}, opts...)
}

func newScoped(guardianPolicy *guardian.Policy, s scope, opts ...Option) *Client {
	if guardianPolicy == nil {
		panic("codex compliance client requires guardian policy")
	}
	c := &Client{
		httpClient: guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig()),
		baseURL:    defaultBaseURL,
		apiKey:     "",
		scope:      s,
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
	logID, err := validateCodexPathID(c.scope.errPrefix+" log id", logID)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.endpoint("logs", logID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create %s download request: %w", c.scope.errPrefix, err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s download request failed: %w", c.scope.errPrefix, err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, newHTTPError(res, c.scope.errPrefix)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxLogFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s log: %w", c.scope.errPrefix, err)
	}
	if len(body) > maxLogFileSize {
		return nil, fmt.Errorf("%s log %s exceeds %d byte limit", c.scope.errPrefix, logID, maxLogFileSize)
	}
	return body, nil
}

func (c *Client) doJSON(ctx context.Context, endpoint *url.URL, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create %s request: %w", c.scope.errPrefix, err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", c.scope.errPrefix, err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return newHTTPError(res, c.scope.errPrefix)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", c.scope.errPrefix, err)
	}
	return nil
}

func (c *Client) endpoint(parts ...string) (*url.URL, error) {
	if !c.scope.pattern.MatchString(c.scope.id) {
		return nil, fmt.Errorf("%s %s must be %s", c.scope.errPrefix, c.scope.name, c.scope.label)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse %s base url: %w", c.scope.errPrefix, err)
	}
	path := []string{c.scope.prefix, c.scope.id}
	for _, part := range parts {
		part, err := validateCodexPathID(c.scope.errPrefix+" path id", part)
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
	// prefix names the integration ("codex compliance" / "chatgpt
	// compliance") so error text attributes failures to the right feed.
	prefix string
}

func (e *HTTPError) Error() string {
	prefix := e.prefix
	if prefix == "" {
		prefix = "codex compliance"
	}
	if e.Body != "" {
		return fmt.Sprintf("%s API returned %s: %s", prefix, e.Status, e.Body)
	}
	return fmt.Sprintf("%s API returned %s", prefix, e.Status)
}

func newHTTPError(res *http.Response, prefix string) *HTTPError {
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxHTTPErrorMessage+1))
	message := strings.TrimSpace(string(body))
	if len(message) > maxHTTPErrorMessage {
		message = message[:maxHTTPErrorMessage]
	}
	return &HTTPError{
		StatusCode: res.StatusCode,
		Status:     res.Status,
		Body:       message,
		prefix:     prefix,
	}
}
