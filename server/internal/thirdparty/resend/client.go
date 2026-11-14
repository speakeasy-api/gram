// Package resend sends transactional emails using published Resend templates.
package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is the transactional delivery boundary.
type Client interface {
	Send(context.Context, SendInput) error
}

type SendInput struct {
	From           string   `json:"from"`
	To             []string `json:"to"`
	ReplyTo        string   `json:"reply_to"`
	Template       Template `json:"template"`
	IdempotencyKey string   `json:"-"`
}

type Template struct {
	ID        string            `json:"id"`
	Variables map[string]string `json:"variables"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type httpClient struct {
	transport HTTPDoer
	apiKey    string
	baseURL   string
}

func New(transport HTTPDoer, apiKey string) Client {
	return &httpClient{transport: transport, apiKey: apiKey, baseURL: "https://api.resend.com"}
}

// NewNoop returns a usable local-development client that drops emails.
func NewNoop() Client { return noopClient{} }

type noopClient struct{}

func (noopClient) Send(context.Context, SendInput) error { return nil }

func (c *httpClient) Send(ctx context.Context, input SendInput) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal resend email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if input.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", input.IdempotencyKey)
	}
	resp, err := c.transport.Do(req)
	if err != nil {
		return fmt.Errorf("send resend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Do not include the response body: provider errors can contain recipient data.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend API returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("decode resend response: %w", err)
	}
	if result.ID == "" {
		return fmt.Errorf("resend response missing email ID")
	}
	return nil
}
