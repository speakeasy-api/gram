package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/workos/workos-go/v6/pkg/mfa"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// DefaultGoogleUserInfoURL is Google's OpenID Connect userinfo endpoint.
const DefaultGoogleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// evidenceTimeout bounds the lookups that establish sign-in evidence. A lookup
// that fails or times out leaves the evidence empty, which never qualifies.
const evidenceTimeout = 2 * time.Second

// SignInEvidence is what the identity provider vouched for at sign-in, beyond
// the authentication method.
type SignInEvidence struct {
	// MFAVerified is set for a password sign-in by a user with an enrolled
	// second factor, which AuthKit requires on every password sign-in.
	MFAVerified bool
	// HostedDomain is the domain a social provider says manages the account:
	// Google's Workspace hosted domain (hd) or a Microsoft work account's UPN
	// domain.
	HostedDomain string
	// TenantID is a Microsoft account's Entra tenant.
	TenantID string
}

func (a *WorkOSAdapter) signInEvidence(ctx context.Context, resp usermanagement.AuthenticateResponse) SignInEvidence {
	var ev SignInEvidence
	ctx, cancel := context.WithTimeout(ctx, evidenceTimeout)
	defer cancel()

	switch resp.AuthenticationMethod {
	case usermanagement.Password:
		factors, err := a.client.ListAuthFactors(ctx, usermanagement.ListAuthFactorsOpts{User: resp.User.ID})
		if err != nil {
			return ev
		}
		ev.MFAVerified = slices.ContainsFunc(factors.Data, func(f mfa.Factor) bool { return f.Type == mfa.TOTP })
		return ev
	case usermanagement.GoogleOAuth:
		if resp.OAuthTokens == nil || resp.OAuthTokens.AccessToken == "" {
			return ev
		}
		hd, err := a.googleHostedDomain(ctx, resp.OAuthTokens.AccessToken)
		if err != nil {
			return ev
		}
		ev.HostedDomain = hd
		return ev
	case usermanagement.MicrosoftOAuth:
		if resp.OAuthTokens == nil {
			return ev
		}
		return microsoftEvidence(resp.OAuthTokens.AccessToken)
	default:
		return ev
	}
}

// googleHostedDomain asks Google which Workspace manages the account. A
// personal Google account has no hd, even when its email is a company address.
func (a *WorkOSAdapter) googleHostedDomain(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.googleUserInfoURL, nil)
	if err != nil {
		return "", fmt.Errorf("build google userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if a.client.HTTPClient == nil {
		return "", errors.New("google userinfo: no http client")
	}
	res, err := a.client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("google userinfo: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google userinfo: status %d", res.StatusCode)
	}
	var info struct {
		HD string `json:"hd"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&info); err != nil {
		return "", fmt.Errorf("decode google userinfo: %w", err)
	}
	return strings.ToLower(info.HD), nil
}

// microsoftEvidence reads the tenant and user principal name from the Graph
// access token WorkOS obtained server to server. Tokens that aren't readable
// JWTs (personal accounts get opaque ones) yield no evidence.
func microsoftEvidence(accessToken string) SignInEvidence {
	var ev SignInEvidence
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ev
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ev
	}
	var claims struct {
		TID               string `json:"tid"`
		UPN               string `json:"upn"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ev
	}
	principal := claims.UPN
	if principal == "" {
		principal = claims.PreferredUsername
	}
	at := strings.LastIndexByte(principal, '@')
	if claims.TID == "" || at <= 0 || at == len(principal)-1 {
		return ev
	}
	ev.TenantID = claims.TID
	ev.HostedDomain = strings.ToLower(principal[at+1:])
	return ev
}
