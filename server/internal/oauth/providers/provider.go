package providers

import (
	"context"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// AuthURLParams contains parameters needed to build an authorization URL
type AuthURLParams struct {
	CallbackURL         string
	Scope               string
	State               string
	ClientID            string
	ScopesSupported     []string
	SpeakeasyServerAddr string
}

// TokenExchangeResult contains the result of exchanging an authorization code
type TokenExchangeResult struct {
	AccessToken string
	ExpiresAt   *time.Time
}

// Provider defines the interface for OAuth provider implementations
type Provider interface {
	// BuildAuthorizationURL builds the authorization URL for this provider
	BuildAuthorizationURL(ctx context.Context, params AuthURLParams) (*url.URL, error)

	// ExchangeToken exchanges an authorization code for an access token
	ExchangeToken(
		ctx context.Context,
		code string,
		provider repo.OauthProxyProvider,
		toolset *toolsets_repo.Toolset,
		serverURL *url.URL,
	) (*TokenExchangeResult, error)
}
