package mockidp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OidcConfig holds OIDC provider configuration.
type OidcConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	ExternalURL  string
	GramSiteURL  string // Gram frontend URL for post-invite redirect (e.g. https://localhost:5173)
}

// IsOidcMode returns true when OIDC credentials are configured.
func (c OidcConfig) IsOidcMode() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != ""
}

// generateCodeVerifier creates a PKCE code verifier.
func generateCodeVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// computeCodeChallenge computes the S256 PKCE code challenge.
func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// buildAuthorizeURL constructs the WorkOS authorization URL with PKCE.
// WorkOS AuthKit uses https://api.workos.com/user_management/authorize
// rather than the OIDC discovery document's authorization_endpoint.
func buildAuthorizeURL(_ context.Context, cfg OidcConfig, state, codeVerifier string) (string, error) {
	redirectURI := cfg.ExternalURL + "/v1/speakeasy_provider/oidc/callback"
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {computeCodeChallenge(codeVerifier)},
		"code_challenge_method": {"S256"},
		"provider":              {"authkit"},
	}

	return "https://api.workos.com/user_management/authorize?" + params.Encode(), nil
}

// workosAuthRequest is the JSON body for the WorkOS authenticate endpoint.
type workosAuthRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier,omitempty"`
}

// workosAuthResponse is the response from the WorkOS authenticate endpoint.
type workosAuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	User         struct {
		ID                string `json:"id"`
		Email             string `json:"email"`
		FirstName         string `json:"first_name"`
		LastName          string `json:"last_name"`
		ProfilePictureURL string `json:"profile_picture_url"`
	} `json:"user"`
	OrganizationID string `json:"organization_id,omitempty"`
}

// exchangeOidcCode exchanges an authorization code for tokens with WorkOS.
// WorkOS uses POST https://api.workos.com/user_management/authenticate
// with a JSON body, rather than the standard OIDC token endpoint.
func exchangeOidcCode(ctx context.Context, cfg OidcConfig, code, codeVerifier string) (*workosAuthResponse, error) {
	reqBody := workosAuthRequest{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		GrantType:    "authorization_code",
		Code:         code,
		CodeVerifier: codeVerifier,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.workos.com/user_management/authenticate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WorkOS authenticate failed: %d %s", resp.StatusCode, string(respBody))
	}

	var authResp workosAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("decode auth response: %w", err)
	}
	return &authResp, nil
}

// OidcClaims holds the claims derived from a WorkOS authenticate response.
type OidcClaims struct {
	Sub     string   `json:"sub"`
	Email   string   `json:"email,omitempty"`
	Name    string   `json:"name,omitempty"`
	Picture string   `json:"picture,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	// WorkOS-specific
	OrgID   string `json:"org_id,omitempty"`
	OrgName string `json:"org_name,omitempty"`
}

// fetchWorkOSOrgName fetches an organization's name from the WorkOS API.
// The WorkOS organizations endpoint requires an API key, not a user access token.
func fetchWorkOSOrgName(ctx context.Context, apiKey, orgID string) (string, error) {
	endpoint := "https://api.workos.com/organizations/" + orgID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create org request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("org request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("org fetch failed: %d", resp.StatusCode)
	}

	var org struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return "", fmt.Errorf("decode org: %w", err)
	}
	return org.Name, nil
}

// extractJWTClaim decodes a JWT payload (without verification) and returns
// the value of the given claim as a string. Used to extract the "sid" (session
// ID) from the WorkOS access token for session revocation on logout.
func extractJWTClaim(jwt, claim string) (string, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("unmarshal JWT claims: %w", err)
	}

	val, ok := claims[claim]
	if !ok {
		return "", fmt.Errorf("claim %q not found", claim)
	}

	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("claim %q is not a string", claim)
	}
	return str, nil
}
