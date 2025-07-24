package oauth

import (
	"time"
)

// ClientInfo represents an OAuth client registration
type ClientInfo struct {
	MCPSlug                 string    `json:"mcp_slug"`
	ClientID                string    `json:"client_id"`
	ClientSecret            string    `json:"client_secret"`
	ClientSecretExpiresAt   int64     `json:"client_secret_expires_at"`
	ClientName              string    `json:"client_name"`
	RedirectURIs            []string  `json:"redirect_uris"`
	GrantTypes              []string  `json:"grant_types"`
	ResponseTypes           []string  `json:"response_types"`
	Scope                   string    `json:"scope"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method"`
	ApplicationType         string    `json:"application_type"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// AuthorizationRequest represents an OAuth authorization request
type AuthorizationRequest struct {
	ResponseType        string `json:"response_type"`
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Nonce               string `json:"nonce"`
}

// TokenRequest represents an OAuth token request
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	CodeVerifier string `json:"code_verifier"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}
