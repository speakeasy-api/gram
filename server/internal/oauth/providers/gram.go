package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

var ErrAccessDenied = errors.New("user does not have access to the requested organization")

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
		return nil, ErrAccessDenied
	}

	session := sessions.Session{
		SessionID:            idToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: toolset.OrganizationID,
	}

	if err := p.sessions.StoreSession(ctx, session); err != nil {
		p.logger.ErrorContext(ctx, "failed to store session from oauth gram provider",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))
	}

	// Use idToken as access token for gram providers
	return &TokenExchangeResult{
		AccessToken: idToken,
		ExpiresAt:   conv.Ptr(time.Now().Add(session.TTL())),
	}, nil
}

// IsAccessDeniedError checks if the error is an access denied error
func IsAccessDeniedError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, ErrAccessDenied)
}
