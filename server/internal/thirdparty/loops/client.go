// Package loops is a thin transport wrapper around the Loops transactional
// email API (https://loops.so).
//
// The package only knows how to ship a request payload — it has no knowledge
// of which template ID maps to which feature. Use the email package to send
// templated emails with strongly typed variables.
package loops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	defaultBaseURL           = "https://app.loops.so/api/v1"
	maxWorkflowResponseBytes = 64 << 10
)

// Client sends transactional emails via Loops.
type Client interface {
	SendTransactional(ctx context.Context, input SendTransactionalInput) error
}

// WorkflowClient manages Loops contacts and sends events that trigger workflows.
type WorkflowClient interface {
	FindContact(ctx context.Context, input FindContactInput) (*Contact, error)
	UpdateContact(ctx context.Context, input UpdateContactInput) error
	SendEvent(ctx context.Context, input SendEventInput) error
}

var errInvalidFindContactInput = errors.New("find contact requires exactly one of email or user ID")

// FindContactInput identifies a Loops contact by exactly one of email or user ID.
type FindContactInput struct {
	// Email is the contact's email address.
	Email string
	// UserID is the contact's external user ID.
	UserID string
}

// UpdateContactInput contains the contact fields to upsert in Loops.
type UpdateContactInput struct {
	// Email identifies the contact by email address.
	Email string
	// FirstName is the optional standard Loops first-name property.
	FirstName *string
	// UserID identifies the contact by external user ID when provided.
	UserID string
	// CustomProperties contains camelCase Loops contact property names and values.
	CustomProperties map[string]any
}

// SendEventInput contains an event and its contact identity and properties.
type SendEventInput struct {
	// Email identifies the contact by email address.
	Email string
	// UserID identifies the contact by external user ID when provided.
	UserID string
	// EventName is the Loops workflow event name.
	EventName string
	// EventProperties contains values made available to the workflow email.
	EventProperties map[string]any
	// IdempotencyKey prevents duplicate lifecycle events.
	IdempotencyKey string
}

// Contact is the subset of Loops contact data needed by workflow callers.
type Contact struct {
	// Email is the contact's email address.
	Email string `json:"email"`

	// FirstName is the contact's first name when configured in Loops.
	FirstName *string `json:"firstName"`

	// UserID is the contact's external user ID.
	UserID *string `json:"userId"`

	// Subscribed reports whether the contact can receive workflow emails.
	Subscribed bool `json:"subscribed"`
}

// SendTransactionalInput is the boundary type for sending a transactional
// email. Higher level packages (e.g. email) translate their typed templates
// into this shape.
type SendTransactionalInput struct {
	// TransactionalID is the Loops template identifier.
	TransactionalID string
	// Email is the recipient's email address.
	Email string
	// DataVariables are the template merge variables. May be nil.
	DataVariables map[string]string
	// AddToAudience instructs Loops to upsert a contact in the audience as a
	// side effect of sending the email.
	AddToAudience bool
	// IdempotencyKey deduplicates sends for 24 hours and is limited to 100 characters.
	IdempotencyKey string
}

// New returns a Client that is always safe to call.
//
// When apiKey is empty or the placeholder value "unset", the returned client
// is a no-op that logs at debug level and returns nil for every send. This
// keeps configuration-gated behavior out of every call site.
func New(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, apiKey string) Client {
	logger = logger.With(attr.SlogComponent("loops"))

	if apiKey == "" || apiKey == "unset" {
		logger.InfoContext(ctx, "loops API key not configured, transactional emails will be dropped")
		return &noopClient{logger: logger}
	}

	return &httpClient{
		logger:     logger,
		httpClient: guardianPolicy.PooledClient(),
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
	}
}

// NewWorkflowClient returns a WorkflowClient that is always safe to call.
//
// When apiKey is empty or the placeholder value "unset", the returned client
// is a no-op that logs at debug level and returns nil for every operation.
func NewWorkflowClient(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, apiKey string) WorkflowClient {
	logger = logger.With(attr.SlogComponent("loops"))

	if apiKey == "" || apiKey == "unset" {
		logger.InfoContext(ctx, "loops API key not configured, workflow emails will be dropped")
		return &noopClient{logger: logger}
	}

	return &httpClient{
		logger:     logger,
		httpClient: guardianPolicy.PooledClient(),
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
	}
}

type httpClient struct {
	logger     *slog.Logger
	httpClient *guardian.HTTPClient
	baseURL    string
	apiKey     string
}

type transactionalRequest struct {
	TransactionalID string            `json:"transactionalId"`
	Email           string            `json:"email"`
	DataVariables   map[string]string `json:"dataVariables,omitempty"`
	AddToAudience   bool              `json:"addToAudience,omitempty"`
}

type transactionalResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

func (c *httpClient) SendTransactional(ctx context.Context, input SendTransactionalInput) error {
	payload, err := json.Marshal(transactionalRequest{
		TransactionalID: input.TransactionalID,
		Email:           input.Email,
		DataVariables:   input.DataVariables,
		AddToAudience:   input.AddToAudience,
	})
	if err != nil {
		return fmt.Errorf("marshal transactional request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transactional", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build transactional request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if input.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", input.IdempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send transactional request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read transactional response: %w", err)
	}

	// A 409 for an idempotent send means Loops already accepted it.
	if resp.StatusCode == http.StatusConflict && input.IdempotencyKey != "" {
		c.logger.DebugContext(ctx, "loops suppressed duplicate transactional email", attr.SlogName(input.TransactionalID))
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loops API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result transactionalResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode transactional response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("loops API rejected transactional email: %s", result.Message)
	}

	return nil
}

type eventRequest struct {
	Email           string         `json:"email,omitempty"`
	UserID          string         `json:"userId,omitempty"`
	EventName       string         `json:"eventName"`
	EventProperties map[string]any `json:"eventProperties,omitempty"`
}

func (c *httpClient) FindContact(ctx context.Context, input FindContactInput) (*Contact, error) {
	if (input.Email == "") == (input.UserID == "") {
		return nil, errInvalidFindContactInput
	}

	query := url.Values{}
	if input.Email != "" {
		query.Set("email", input.Email)
	} else if input.UserID != "" {
		query.Set("userId", input.UserID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/contacts/find?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build find contact request: %w", err)
	}

	respBody, err := c.doWorkflowRequestWithRequest(req)
	if err != nil {
		return nil, fmt.Errorf("find contact: %w", err)
	}

	var contacts []Contact
	if err := json.Unmarshal(respBody, &contacts); err != nil {
		return nil, fmt.Errorf("decode find contact response: %w", err)
	}
	if len(contacts) == 0 {
		return nil, nil
	}
	return &contacts[0], nil
}

func (c *httpClient) UpdateContact(ctx context.Context, input UpdateContactInput) error {
	payload := map[string]any{}
	if input.Email != "" {
		payload["email"] = input.Email
	}
	if input.FirstName != nil {
		payload["firstName"] = *input.FirstName
	}
	if input.UserID != "" {
		payload["userId"] = input.UserID
	}
	maps.Copy(payload, input.CustomProperties)

	return c.sendWorkflowJSON(ctx, http.MethodPut, "/contacts/update", payload, "update contact", "")
}

func (c *httpClient) SendEvent(ctx context.Context, input SendEventInput) error {
	payload, err := json.Marshal(eventRequest{
		Email:           input.Email,
		UserID:          input.UserID,
		EventName:       input.EventName,
		EventProperties: input.EventProperties,
	})
	if err != nil {
		return fmt.Errorf("marshal send event request: %w", err)
	}
	return c.sendWorkflowJSON(ctx, http.MethodPost, "/events/send", payload, "send event", input.IdempotencyKey)
}

func (c *httpClient) sendWorkflowJSON(ctx context.Context, method, path string, payload any, operation, idempotencyKey string) error {
	var body io.Reader
	if data, ok := payload.([]byte); ok {
		body = bytes.NewReader(data)
	} else {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", operation, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build %s request: %w", operation, err)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if _, err := c.doWorkflowRequestWithRequest(req); err != nil {
		var respErr *workflowResponseError
		if errors.As(err, &respErr) && respErr.statusCode == http.StatusConflict && idempotencyKey != "" {
			c.logger.DebugContext(ctx, "loops suppressed duplicate workflow event", attr.SlogName(idempotencyKey))
			return nil
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (c *httpClient) doWorkflowRequestWithRequest(req *http.Request) ([]byte, error) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute workflow request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkflowResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read workflow response: %w", err)
	}
	if len(respBody) > maxWorkflowResponseBytes {
		return nil, fmt.Errorf("workflow response body exceeded %d bytes", maxWorkflowResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &workflowResponseError{statusCode: resp.StatusCode, body: string(respBody)}
	}
	return respBody, nil
}

type workflowResponseError struct {
	statusCode int
	body       string
}

func (e *workflowResponseError) Error() string {
	return fmt.Sprintf("loops API returned HTTP %d: %s", e.statusCode, e.body)
}

type noopClient struct {
	logger *slog.Logger
}

func (c *noopClient) FindContact(_ context.Context, input FindContactInput) (*Contact, error) {
	if (input.Email == "") == (input.UserID == "") {
		return nil, errInvalidFindContactInput
	}
	return nil, nil
}

func (c *noopClient) UpdateContact(ctx context.Context, input UpdateContactInput) error {
	c.logger.DebugContext(ctx, "loops disabled, dropping contact update", attr.SlogName(input.Email))
	return nil
}

func (c *noopClient) SendEvent(ctx context.Context, input SendEventInput) error {
	c.logger.DebugContext(ctx, "loops disabled, dropping workflow event", attr.SlogName(input.EventName))
	return nil
}

func (c *noopClient) SendTransactional(ctx context.Context, input SendTransactionalInput) error {
	c.logger.DebugContext(ctx, "loops disabled, dropping transactional email",
		attr.SlogName(input.TransactionalID),
	)
	return nil
}
