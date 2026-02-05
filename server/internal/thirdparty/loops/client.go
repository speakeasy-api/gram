package loops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const baseURL = "https://app.loops.so/api/v1"

// Template is a key used to look up a Loops transactional email template ID.
type Template string

const (
	TemplateTeamInvite Template = "team_invite"
)

// templates maps template keys to their Loops transactional IDs.
var templates = map[Template]string{
	TemplateTeamInvite: "cml3n1h2n27o50i2rakc30bwb",
}

// TransactionalID returns the Loops transactional ID for the given template key.
// Returns empty string if the template is not registered.
func TransactionalID(t Template) string {
	return templates[t]
}

// Client is a lightweight HTTP client for the Loops transactional email API.
type Client struct {
	logger     *slog.Logger
	httpClient *http.Client
	apiKey     string
	enabled    bool
}

// NewClient creates a new Loops client. If apiKey is empty or "unset", the client
// is disabled and all send operations become no-ops.
func NewClient(logger *slog.Logger, apiKey string) *Client {
	enabled := apiKey != "" && apiKey != "unset"
	return &Client{
		logger:     logger.With(attr.SlogComponent("loops")),
		httpClient: retryablehttp.NewClient().StandardClient(),
		apiKey:     apiKey,
		enabled:    enabled,
	}
}

// Enabled returns whether the client has a valid API key configured.
func (c *Client) Enabled() bool {
	return c.enabled
}

// SendTransactionalEmailInput contains the parameters for sending a transactional email.
type SendTransactionalEmailInput struct {
	// TransactionalID is the ID of the transactional email template in Loops.
	TransactionalID string
	// Email is the recipient email address.
	Email string
	// DataVariables are template variables passed to the email.
	DataVariables map[string]string
	// AddToAudience creates a contact in your Loops audience if true.
	AddToAudience bool
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

// SendTransactionalEmail sends a transactional email via the Loops API.
// If the client is disabled, this is a no-op and returns nil.
func (c *Client) SendTransactionalEmail(ctx context.Context, input SendTransactionalEmailInput) error {
	if !c.enabled {
		c.logger.DebugContext(ctx, "loops client disabled, skipping transactional email")
		return nil
	}

	body := transactionalRequest(input)

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/transactional", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loops API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result transactionalResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("loops API error: %s", result.Message)
	}

	return nil
}
