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
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const defaultBaseURL = "https://app.loops.so/api/v1"

// Client sends transactional emails via Loops.
type Client interface {
	SendTransactional(ctx context.Context, input SendTransactionalInput) error
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
	payload, err := json.Marshal(transactionalRequest(input))
	if err != nil {
		return fmt.Errorf("marshal transactional request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transactional", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build transactional request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send transactional request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read transactional response: %w", err)
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

type noopClient struct {
	logger *slog.Logger
}

func (c *noopClient) SendTransactional(ctx context.Context, input SendTransactionalInput) error {
	c.logger.DebugContext(ctx, "loops disabled, dropping transactional email",
		attr.SlogName(input.TransactionalID),
	)
	return nil
}
