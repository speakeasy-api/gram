package oauth

import (
	"time"

	"github.com/google/uuid"
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
	ToolsetID           uuid.UUID
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

func GrantCacheKey(toolsetId uuid.UUID, code string) string {
	return "oauthGrant:" + toolsetId.String() + ":" + code
}

func (g Grant) CacheKey() string {
	return GrantCacheKey(g.ToolsetID, g.Code)
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
	ToolsetID       uuid.UUID        `json:"-"`
	AccessToken     string           `json:"access_token"`
	TokenType       string           `json:"token_type"`
	Scope           string           `json:"scope,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	ExternalSecrets []ExternalSecret `json:"-"` // this should never be exposed in JSON
}

func TokenCacheKey(toolsetID uuid.UUID, token string) string {
	return "oauthToken:" + toolsetID.String() + ":" + token
}

func (t Token) CacheKey() string {
	return TokenCacheKey(t.ToolsetID, t.AccessToken)
}

func (t Token) AdditionalCacheKeys() []string {
	return []string{}
}

func (t Token) TTL() time.Duration {
	return time.Until(t.ExpiresAt)
}

var _ cache.CacheableObject[OauthProxyClientInfo] = (*OauthProxyClientInfo)(nil)

type OauthProxyClientInfo struct {
	MCPURL                  string
	ClientID                string
	ClientSecret            string
	ClientSecretExpiresAt   time.Time
	ClientName              string
	RedirectUris            []string
	GrantTypes              []string
	ResponseTypes           []string
	Scope                   string
	TokenEndpointAuthMethod string
	ApplicationType         string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func ClientInfoCacheKey(mcpURL string, clientID string) string {
	return "oauthClientInfo:" + mcpURL + ":" + clientID
}

func (o OauthProxyClientInfo) CacheKey() string {
	return ClientInfoCacheKey(o.MCPURL, o.ClientID)
}

func (o OauthProxyClientInfo) AdditionalCacheKeys() []string {
	return []string{}
}

func (o OauthProxyClientInfo) TTL() time.Duration {
	// we are responding to mcp clients wiht a client registration expiry of 15 days
	// it seems most respect this so we may start enforcing this TTL in our storage
	// we will not until we finish evaluating that
	return 0
}
