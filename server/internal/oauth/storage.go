package oauth

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

var _ cache.CacheableObject[Grant] = (*Grant)(nil)

type ExternalSecret struct {
	SecurityKeys []string   `json:"-"`
	Token        string     `json:"-"`
	ExpiresAt    *time.Time `json:"-"`
}

// Grant represents an OAuth authorization grant
type Grant struct {
	MCPSlug             string
	Code                string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Props               map[string]string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	ExternalSecrets     []ExternalSecret
}

func GrantCacheKey(mcpSlug string, code string) string {
	return "oauthGrant:" + mcpSlug + ":" + code
}

func (g Grant) CacheKey() string {
	return GrantCacheKey(g.MCPSlug, g.Code)
}

func (g Grant) AdditionalCacheKeys() []string {
	return []string{}
}

func (g Grant) TTL() time.Duration {
	return time.Until(g.ExpiresAt)
}

var _ cache.CacheableObject[Token] = (*Token)(nil)

// Token represents an OAuth access token
type Token struct {
	MCPSlug         string           `json:"mcp_slug"`
	AccessToken     string           `json:"access_token"`
	TokenType       string           `json:"token_type"`
	Scope           string           `json:"scope,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	ExternalSecrets []ExternalSecret `json:"-"` // this should never be exposed in JSON
}

func TokenCacheKey(mcpSlug string, token string) string {
	return "oauthToken:" + mcpSlug + ":" + token
}

func (t Token) CacheKey() string {
	return TokenCacheKey(t.MCPSlug, t.AccessToken)
}

func (t Token) AdditionalCacheKeys() []string {
	return []string{}
}

func (t Token) TTL() time.Duration {
	return time.Until(t.ExpiresAt)
}
