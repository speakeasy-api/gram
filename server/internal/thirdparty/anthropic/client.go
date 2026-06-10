package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
)

type Client struct {
	httpClient *guardian.HTTPClient
	baseURL    string
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

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		c.apiKey = apiKey
	}
}

func New(guardianPolicy *guardian.Policy, opts ...Option) *Client {
	if guardianPolicy == nil {
		panic("anthropic client requires guardian policy")
	}
	c := &Client{
		httpClient: guardianPolicy.PooledClient(),
		baseURL:    defaultBaseURL,
		apiKey:     "",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type ListActivitiesParams struct {
	ActivityTypes   []string
	OrganizationIDs []string
	CreatedAtGTE    time.Time
	AfterID         string
	Limit           int
}

type ActivitiesPage struct {
	Data    []Activity `json:"data"`
	HasMore bool       `json:"has_more"`
	FirstID string     `json:"first_id"`
	LastID  string     `json:"last_id"`
}

type Activity struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	CreatedAt        string `json:"created_at"`
	OrganizationID   string `json:"organization_id"`
	OrganizationUUID string `json:"organization_uuid"`
	Actor            Actor  `json:"actor"`
	ClaudeChatID     string `json:"claude_chat_id"`
	ClaudeProjectID  string `json:"claude_project_id"`
}

func (a Activity) CreatedAtTime() (time.Time, error) {
	if a.CreatedAt == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, a.CreatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse activity created_at: %w", err)
	}
	return t.UTC(), nil
}

type Actor struct {
	Type         string `json:"type"`
	EmailAddress string `json:"email_address"`
	UserID       string `json:"user_id"`
	IPAddress    string `json:"ip_address"`
	UserAgent    string `json:"user_agent"`
}

func (c *Client) ListActivities(ctx context.Context, params ListActivitiesParams) (*ActivitiesPage, error) {
	endpoint, err := c.endpoint("/v1/compliance/activities")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	for _, activityType := range params.ActivityTypes {
		q.Add("activity_types[]", activityType)
	}
	for _, organizationID := range params.OrganizationIDs {
		q.Add("organization_ids[]", organizationID)
	}
	if !params.CreatedAtGTE.IsZero() {
		q.Set("created_at.gte", params.CreatedAtGTE.UTC().Format(time.RFC3339))
	}
	if params.AfterID != "" {
		q.Set("after_id", params.AfterID)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	endpoint.RawQuery = q.Encode()

	var page ActivitiesPage
	if err := c.doJSON(ctx, http.MethodGet, endpoint, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

type GetChatMessagesParams struct {
	ClaudeChatID string
	AfterID      string
	Limit        int
}

type ChatMessagesPage struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	CreatedAt        string        `json:"created_at"`
	UpdatedAt        string        `json:"updated_at"`
	DeletedAt        *string       `json:"deleted_at"`
	Href             string        `json:"href"`
	Model            *string       `json:"model"`
	OrganizationID   string        `json:"organization_id"`
	OrganizationUUID string        `json:"organization_uuid"`
	ProjectID        string        `json:"project_id"`
	User             ChatUser      `json:"user"`
	Messages         []ChatMessage `json:"chat_messages"`
	HasMore          bool          `json:"has_more"`
	FirstID          string        `json:"first_id"`
	LastID           string        `json:"last_id"`
}

type ChatUser struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
}

type ChatMessage struct {
	ID             string          `json:"id"`
	Role           string          `json:"role"`
	CreatedAt      string          `json:"created_at"`
	Content        json.RawMessage `json:"content"`
	Files          []FileRef       `json:"files"`
	GeneratedFiles []FileRef       `json:"generated_files"`
	Artifacts      []ArtifactRef   `json:"artifacts"`
}

func (m ChatMessage) CreatedAtTime() (time.Time, error) {
	if m.CreatedAt == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse message created_at: %w", err)
	}
	return t.UTC(), nil
}

type FileRef struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
}

type ArtifactRef struct {
	ID           string `json:"id"`
	VersionID    string `json:"version_id"`
	Title        string `json:"title"`
	ArtifactType string `json:"artifact_type"`
}

func (c *Client) GetChatMessages(ctx context.Context, params GetChatMessagesParams) (*ChatMessagesPage, error) {
	if params.ClaudeChatID == "" {
		return nil, fmt.Errorf("claude chat id is required")
	}
	endpoint, err := c.endpoint("/v1/compliance/apps/chats/" + url.PathEscape(params.ClaudeChatID) + "/messages")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("order", "asc")
	q.Set("tool_result_max_chars", "-1")
	q.Set("tool_use_input_max_chars", "-1")
	if params.AfterID != "" {
		q.Set("after_id", params.AfterID)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	endpoint.RawQuery = q.Encode()

	var page ChatMessagesPage
	if err := c.doJSON(ctx, http.MethodGet, endpoint, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

type DownloadedContent struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	Filename      string
	ContentMD5    string
}

func (c *Client) DownloadChatFile(ctx context.Context, id string) (*DownloadedContent, error) {
	return c.download(ctx, "/v1/compliance/apps/chats/files/"+url.PathEscape(id)+"/content")
}

func (c *Client) DownloadGeneratedFile(ctx context.Context, id string) (*DownloadedContent, error) {
	return c.download(ctx, "/v1/compliance/apps/chats/generated-files/"+url.PathEscape(id)+"/content")
}

func (c *Client) DownloadArtifact(ctx context.Context, versionID string) (*DownloadedContent, error) {
	return c.download(ctx, "/v1/compliance/apps/artifacts/"+url.PathEscape(versionID)+"/content")
}

func (c *Client) download(ctx context.Context, path string) (*DownloadedContent, error) {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create anthropic compliance download request: %w", err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic compliance download request failed: %w", err)
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = res.Body.Close() }()
		return nil, &HTTPError{StatusCode: res.StatusCode, Status: res.Status}
	}
	return &DownloadedContent{
		Body:          res.Body,
		ContentType:   res.Header.Get("Content-Type"),
		ContentLength: res.ContentLength,
		Filename:      filenameFromContentDisposition(res.Header.Get("Content-Disposition")),
		ContentMD5:    res.Header.Get("Content-MD5"),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, method string, endpoint *url.URL, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create anthropic compliance request: %w", err)
	}
	c.setHeaders(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic compliance request failed: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return &HTTPError{StatusCode: res.StatusCode, Status: res.Status}
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode anthropic compliance response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(path string) (*url.URL, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse anthropic base url: %w", err)
	}
	return base.JoinPath(path), nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
}

func filenameFromContentDisposition(value string) string {
	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return params["filename"]
}

type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("anthropic compliance API returned %s", e.Status)
}
