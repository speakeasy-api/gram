package loopsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://app.loops.so/api/v1"
	maxRetryDelay  = 30 * time.Second
)

type TransactionalEmail struct {
	ID                                 string   `json:"id"`
	Name                               string   `json:"name"`
	DraftEmailMessageID                *string  `json:"draftEmailMessageId"`
	DraftEmailMessageContentRevisionID *string  `json:"draftEmailMessageContentRevisionId"`
	PublishedEmailMessageID            *string  `json:"publishedEmailMessageId"`
	DataVariables                      []string `json:"dataVariables"`
}

type EmailMessage struct {
	ID                string `json:"id"`
	Subject           string `json:"subject"`
	PreviewText       string `json:"previewText"`
	FromName          string `json:"fromName"`
	FromEmail         string `json:"fromEmail"`
	ReplyToEmail      string `json:"replyToEmail"`
	LMX               string `json:"lmx"`
	ContentRevisionID string `json:"contentRevisionId"`
}

type GuardianResult struct {
	Errors   []GuardianIssue `json:"errors"`
	Warnings []GuardianIssue `json:"warnings"`
}

type GuardianIssue struct {
	Rule        string `json:"rule"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type API interface {
	ListTransactionalEmails(context.Context) ([]TransactionalEmail, error)
	GetTransactionalEmail(context.Context, string) (TransactionalEmail, error)
	CreateTransactionalEmail(context.Context, string) (TransactionalEmail, error)
	EnsureDraft(context.Context, string) (TransactionalEmail, error)
	GetEmailMessage(context.Context, string) (EmailMessage, error)
	UpdateEmailMessage(context.Context, string, UpdateEmailMessageInput) (EmailMessage, error)
	Guardian(context.Context, string) (GuardianResult, error)
	Publish(context.Context, string) (TransactionalEmail, error)
}

type UpdateEmailMessageInput struct {
	ExpectedRevisionID string `json:"expectedRevisionId"`
	Subject            string `json:"subject"`
	PreviewText        string `json:"previewText"`
	FromName           string `json:"fromName"`
	FromEmail          string `json:"fromEmail"`
	ReplyToEmail       string `json:"replyToEmail"`
	LMX                string `json:"lmx"`
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient HTTPDoer
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type requestOptions struct {
	expectedStatus int
	retryable      bool
}

func NewClient(baseURL, apiKey string, httpClient HTTPDoer) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: httpClient}
}

func (c *Client) ListTransactionalEmails(ctx context.Context) ([]TransactionalEmail, error) {
	var result []TransactionalEmail
	cursor := ""
	for {
		query := url.Values{"perPage": []string{"50"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page struct {
			Pagination struct {
				NextCursor *string `json:"nextCursor"`
			} `json:"pagination"`
			Data []TransactionalEmail `json:"data"`
		}
		if err := c.do(ctx, http.MethodGet, "/transactional-emails?"+query.Encode(), nil, &page, requestOptions{expectedStatus: http.StatusOK, retryable: true}); err != nil {
			return nil, err
		}
		result = append(result, page.Data...)
		if page.Pagination.NextCursor == nil || *page.Pagination.NextCursor == "" {
			return result, nil
		}
		cursor = *page.Pagination.NextCursor
	}
}

func (c *Client) GetTransactionalEmail(ctx context.Context, id string) (TransactionalEmail, error) {
	var result TransactionalEmail
	err := c.do(ctx, http.MethodGet, "/transactional-emails/"+url.PathEscape(id), nil, &result, requestOptions{expectedStatus: http.StatusOK, retryable: true})
	return result, err
}

func (c *Client) CreateTransactionalEmail(ctx context.Context, name string) (TransactionalEmail, error) {
	var result TransactionalEmail
	err := c.do(ctx, http.MethodPost, "/transactional-emails", map[string]string{"name": name}, &result, requestOptions{expectedStatus: http.StatusCreated, retryable: false})
	return result, err
}

func (c *Client) EnsureDraft(ctx context.Context, id string) (TransactionalEmail, error) {
	var result TransactionalEmail
	err := c.do(ctx, http.MethodPost, "/transactional-emails/"+url.PathEscape(id)+"/draft", nil, &result, requestOptions{expectedStatus: http.StatusOK, retryable: false})
	return result, err
}

func (c *Client) GetEmailMessage(ctx context.Context, id string) (EmailMessage, error) {
	var result EmailMessage
	err := c.do(ctx, http.MethodGet, "/email-messages/"+url.PathEscape(id), nil, &result, requestOptions{expectedStatus: http.StatusOK, retryable: true})
	return result, err
}

func (c *Client) UpdateEmailMessage(ctx context.Context, id string, input UpdateEmailMessageInput) (EmailMessage, error) {
	var result EmailMessage
	// The expected revision makes replay safe: a completed first attempt causes
	// the retry to fail closed with a revision conflict instead of overwriting.
	err := c.do(ctx, http.MethodPost, "/email-messages/"+url.PathEscape(id), input, &result, requestOptions{expectedStatus: http.StatusOK, retryable: true})
	return result, err
}

func (c *Client) Guardian(ctx context.Context, id string) (GuardianResult, error) {
	var result GuardianResult
	err := c.do(ctx, http.MethodGet, "/email-messages/"+url.PathEscape(id)+"/guardian", nil, &result, requestOptions{expectedStatus: http.StatusOK, retryable: true})
	return result, err
}

func (c *Client) Publish(ctx context.Context, id string) (TransactionalEmail, error) {
	var result TransactionalEmail
	err := c.do(ctx, http.MethodPost, "/transactional-emails/"+url.PathEscape(id)+"/publish", nil, &result, requestOptions{expectedStatus: http.StatusOK, retryable: false})
	return result, err
}

func (c *Client) do(ctx context.Context, method, path string, input, output any, options requestOptions) error {
	var payload []byte
	var err error
	if input != nil {
		payload, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Loops request: %w", err)
		}
	}

	for attempt := range 3 {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build Loops request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("call Loops Content API: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Loops Content API response: %w", readErr)
		}

		if resp.StatusCode == options.expectedStatus {
			if output == nil || len(body) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, output); err != nil {
				return fmt.Errorf("decode Loops Content API response: %w", err)
			}
			return nil
		}

		if attempt < 2 && options.retryable && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
			delay := retryDelay(time.Now(), attempt, resp.Header.Get("Retry-After"))
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait to retry Loops Content API: %w", ctx.Err())
			case <-time.After(delay):
				continue
			}
		}

		var apiError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiError)
		if apiError.Message == "" {
			apiError.Message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("loops Content API %s %s returned HTTP %d: %s", method, path, resp.StatusCode, apiError.Message)
	}
	return fmt.Errorf("loops Content API %s %s exhausted retries", method, path)
}

func retryDelay(now time.Time, attempt int, retryAfter string) time.Duration {
	delay := time.Duration(attempt+1) * time.Second
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		if seconds >= 0 {
			maxSeconds := int(maxRetryDelay / time.Second)
			return time.Duration(min(seconds, maxSeconds)) * time.Second
		}
		return delay
	}

	retryAt, err := http.ParseTime(retryAfter)
	if err != nil {
		return delay
	}
	if wait := retryAt.Sub(now); wait > 0 {
		return min(wait, maxRetryDelay)
	}
	return delay
}
