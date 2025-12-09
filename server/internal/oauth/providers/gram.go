package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// GramProvider implements OAuth provider for Gram/Speakeasy
type GramProvider struct {
	logger   *slog.Logger
	sessions *sessions.Manager
}

// NewGramProvider creates a new Gram OAuth provider
func NewGramProvider(logger *slog.Logger, sessions *sessions.Manager) *GramProvider {
	return &GramProvider{
		logger:   logger,
		sessions: sessions,
	}
}

// BuildAuthorizationURL builds the authorization URL for Gram OAuth
func (p *GramProvider) BuildAuthorizationURL(ctx context.Context, params AuthURLParams) (*url.URL, error) {
	urlParams := url.Values{}
	urlParams.Add("return_url", params.CallbackURL)
	urlParams.Add("state", params.State)

	// !TODO: Check why these are empty
	// Set scope from provider configuration or request
	// if len(params.ScopesSupported) > 0 {
	// Gram provider doesn't use scopes in the same way, but we'll include it if specified
	// } else if params.Scope != "" {
	// Include scope if provided in request
	// }

	gramAuthURL := fmt.Sprintf("%s/v1/speakeasy_provider/login?%s",
		params.SpeakeasyServerAddr,
		urlParams.Encode())

	authURL, err := url.Parse(gramAuthURL)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to parse gram OAuth URL", attr.SlogError(err))
		return nil, fmt.Errorf("failed to parse gram OAuth URL: %w", err)
	}

	return authURL, nil
}

// ExchangeToken exchanges an authorization code for an access token from Gram
func (p *GramProvider) ExchangeToken(
	ctx context.Context,
	code string,
	provider repo.OauthProxyProvider,
	toolset *toolsets_repo.Toolset,
	serverURL *url.URL,
) (*TokenExchangeResult, error) {
	// Exchange code for ID token from Speakeasy
	idToken, err := p.sessions.ExchangeTokenFromSpeakeasy(ctx, code)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to exchange code for token from oauth gram provider",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Get user info from Speakeasy
	userInfo, err := p.sessions.GetUserInfoFromSpeakeasy(ctx, idToken)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to get user info from oauth gram provider",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to retrieve user info: %w", err)
	}

	// Check if user has access to the organization
	hasOrgAccess := false
	for _, org := range userInfo.Organizations {
		if org.ID == toolset.OrganizationID {
			hasOrgAccess = true
			break
		}
	}

	if !hasOrgAccess {
		p.logger.WarnContext(ctx, "user does not have access to organization",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogUserID(userInfo.UserID),
			attr.SlogOrganizationID(toolset.OrganizationID))
		return nil, fmt.Errorf("user does not have access to the requested organization")
	}

	// Use idToken as access token for gram providers
	return &TokenExchangeResult{
		AccessToken: idToken,
		ExpiresAt:   nil,
	}, nil
}

// IsAccessDeniedError checks if the error is an access denied error
func IsAccessDeniedError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "user does not have access to the requested organization"
}
