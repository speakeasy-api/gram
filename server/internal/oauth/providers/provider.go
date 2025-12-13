package providers

import (
	"context"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TokenExchangeResult contains the result of exchanging an authorization code
type TokenExchangeResult struct {
	AccessToken string
	ExpiresAt   *time.Time
}

// Provider defines the interface for OAuth provider implementations
type Provider interface {
	// ExchangeToken exchanges an authorization code for an access token
	ExchangeToken(
		ctx context.Context,
		code string,
		provider repo.OauthProxyProvider,
		toolset *toolsets_repo.Toolset,
		serverURL *url.URL,
	) (*TokenExchangeResult, error)
}
