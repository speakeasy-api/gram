package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oauth"
)

// OAuthService defines the OAuth operations needed by MCP service
type OAuthService interface {
	ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error)
}
