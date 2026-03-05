package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// OAuthService defines the OAuth operations needed by MCP service
type OAuthService interface {
	ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error)
	RefreshProxyToken(ctx context.Context, toolsetID uuid.UUID, token *oauth.Token, proxyProvider *oauth_repo.OauthProxyProvider, toolset *toolsets_repo.Toolset) (*oauth.Token, error)
}
