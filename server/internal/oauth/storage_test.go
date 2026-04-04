package oauth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth"
)

func TestTokenTTLFloorWhenExpired(t *testing.T) {
	t.Parallel()
	token := oauth.Token{
		ToolsetID:       uuid.New(),
		AccessToken:     "test",
		RefreshToken:    "test",
		TokenType:       "Bearer",
		Scope:           "",
		CreatedAt:       time.Now().Add(-48 * time.Hour),
		ExpiresAt:       time.Now().Add(-48 * time.Hour),
		ExternalSecrets: nil,
	}
	ttl := token.TTL()
	require.GreaterOrEqual(t, ttl, time.Minute, "TTL should be at least 1 minute even when ExpiresAt is in the past")
}

func TestTokenTTLNormalExpiry(t *testing.T) {
	t.Parallel()
	token := oauth.Token{
		ToolsetID:       uuid.New(),
		AccessToken:     "test",
		RefreshToken:    "test",
		TokenType:       "Bearer",
		Scope:           "",
		CreatedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(30 * 24 * time.Hour),
		ExternalSecrets: nil,
	}
	ttl := token.TTL()
	// Should be ~31 days (30 days + 24h grace period)
	require.Greater(t, ttl, 30*24*time.Hour, "TTL should include grace period beyond ExpiresAt")
}

func TestOauthProxyClientInfoTTLFloorWhenExpired(t *testing.T) {
	t.Parallel()
	info := oauth.OauthProxyClientInfo{
		MCPURL:                  "https://example.com",
		ClientID:                "test",
		ClientSecret:            "test",
		ClientSecretExpiresAt:   time.Now().Add(-24 * time.Hour),
		ClientName:              "test",
		RedirectUris:            nil,
		GrantTypes:              nil,
		ResponseTypes:           nil,
		Scope:                   "",
		TokenEndpointAuthMethod: "",
		ApplicationType:         "",
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}
	ttl := info.TTL()
	require.GreaterOrEqual(t, ttl, time.Minute, "TTL should be at least 1 minute even when ClientSecretExpiresAt is in the past")
}

func TestGrantTTLFloorWhenExpired(t *testing.T) {
	t.Parallel()
	grant := oauth.Grant{
		ToolsetID:           uuid.New(),
		Code:                "test",
		ClientID:            "test",
		RedirectURI:         "https://example.com/callback",
		Scope:               "",
		State:               "",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		Props:               nil,
		CreatedAt:           time.Now().Add(-1 * time.Hour),
		ExpiresAt:           time.Now().Add(-1 * time.Hour),
		ExternalSecrets:     nil,
	}
	ttl := grant.TTL()
	require.GreaterOrEqual(t, ttl, time.Minute, "TTL should be at least 1 minute even when ExpiresAt is in the past")
}
