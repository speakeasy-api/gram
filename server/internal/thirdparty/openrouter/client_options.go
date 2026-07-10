package openrouter

import "github.com/speakeasy-api/gram/server/internal/guardian"

type clientOptions struct {
	httpClient *guardian.HTTPClient
}

// ClientOption configures an OpenRouter HTTP client wrapper.
type ClientOption func(*clientOptions)

// WithHTTPClient injects the shared account-scoped HTTP client. It is used by
// the composition root so completions, metadata, and key-management requests
// all participate in one upstream rate limit.
func WithHTTPClient(httpClient *guardian.HTTPClient) ClientOption {
	return func(opts *clientOptions) {
		opts.httpClient = httpClient
	}
}

func applyClientOptions(options []ClientOption) clientOptions {
	result := clientOptions{httpClient: nil}
	for _, option := range options {
		option(&result)
	}
	return result
}
