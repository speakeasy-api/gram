package llmanalyzer

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

const (
	// DefaultTimeout bounds one Complete call end to end, retries included.
	// The realtime enforcement lane waits on this budget, so it is a code
	// constant rather than configuration.
	DefaultTimeout = 15 * time.Second

	// DefaultMaxTokens caps the completion length. A verdict is a small JSON
	// object; the headroom exists only so the model is never cut mid-object.
	DefaultMaxTokens = 1024

	// DefaultModel is the served model name of the merged fine-tune. It must
	// equal the --served-model-name of the deployment the base URL points at.
	DefaultModel = "risk-judge-4b"
)

// Config configures the fine-tuned risk model client.
type Config struct {
	// BaseURL is the OpenAI-compatible base URL of the model deployment,
	// including the API version segment (for example
	// https://model.example.com/v1). The client appends /chat/completions to
	// it verbatim. It must be https. An empty value disables the analyzer.
	BaseURL string

	// APIKey is sent as a bearer token on every request.
	APIKey string

	// Model is the served model name placed in the request body.
	Model string

	// Timeout bounds one Complete call end to end, including retries and
	// backoff. Zero selects DefaultTimeout.
	Timeout time.Duration

	// MaxTokens caps the completion length requested from the model. Zero
	// selects DefaultMaxTokens.
	MaxTokens int
}

// Enabled reports whether a base URL is configured. A disabled analyzer is
// never constructed; callers reply dead-letter (sync) or publish nothing
// (async) instead.
func (c Config) Enabled() bool {
	return c.BaseURL != ""
}

// Validate checks the fields a client needs: an absolute https base URL with a
// host, a model name and an API key. The guardian dialer still enforces the
// private-network blocklist on every request, so this only rejects
// configurations that could never work. Zero Timeout and MaxTokens are valid
// and take their defaults in NewClient.
func (c Config) Validate() error {
	var errs []error

	if c.BaseURL == "" {
		errs = append(errs, errors.New("base url is required"))
	} else {
		u, err := url.Parse(c.BaseURL)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("parse base url: %w", err))
		case u.Scheme != "https":
			errs = append(errs, errors.New("base url scheme must be https"))
		case u.Host == "":
			errs = append(errs, errors.New("base url must include a host"))
		}
	}

	if c.Model == "" {
		errs = append(errs, errors.New("model is required"))
	}
	if c.APIKey == "" {
		errs = append(errs, errors.New("api key is required"))
	}
	if c.Timeout < 0 {
		errs = append(errs, errors.New("timeout must not be negative"))
	}
	if c.MaxTokens < 0 {
		errs = append(errs, errors.New("max tokens must not be negative"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("risk llm config: %w", errors.Join(errs...))
	}
	return nil
}
