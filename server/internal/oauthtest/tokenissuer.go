package oauthtest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth"
)

const (
	testMCPURL      = "http://test.example.com/mcp/test"
	testRedirectURI = "http://localhost:8080/callback"
)

// TokenIssuer creates real OAuth tokens in the cache for integration testing.
// It uses the same underlying components (TokenService, ClientRegistration,
// GrantManager) as the production oauth.Service, so tokens it creates are
// discoverable by ValidateAccessToken.
//
// This exists because the full OAuth dance (DCR → authorize → token exchange)
// requires driving Gram's OAuth HTTP endpoints end-to-end. The TokenIssuer
// provides a shortcut: it creates Gram-layer tokens directly in Redis using the
// real token service internals, letting tests focus on the ServePublic code
// paths without orchestrating the full browser-based flow.
type TokenIssuer struct {
	tokenService *oauth.TokenService
	clientReg    *oauth.ClientRegistrationService
	grantMgr     *oauth.GrantManager
	cacheAdapter cache.Cache
}

// NewTokenIssuer creates a TokenIssuer backed by the given cache and encryption
// client. The cache MUST be the same instance used by the oauth.Service that
// will later validate tokens — otherwise lookups won't find issued tokens.
func NewTokenIssuer(t *testing.T, cacheAdapter cache.Cache, enc *encryption.Client) *TokenIssuer {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clientReg := oauth.NewClientRegistrationService(cacheAdapter, logger)
	pkceService := oauth.NewPKCEService(logger)
	grantMgr := oauth.NewGrantManager(cacheAdapter, clientReg, pkceService, logger, enc)
	tokenService := oauth.NewTokenService(cacheAdapter, clientReg, grantMgr, pkceService, logger, enc)
	return &TokenIssuer{
		tokenService: tokenService,
		clientReg:    clientReg,
		grantMgr:     grantMgr,
		cacheAdapter: cacheAdapter,
	}
}

// IssuedToken contains the result of issuing a test token.
type IssuedToken struct {
	// AccessToken is the raw (unhashed) access token to pass as a Bearer header.
	AccessToken string
	// RefreshToken is the raw (unhashed) refresh token.
	RefreshToken string
	// Token is the full token record as stored.
	Token *oauth.Token
}

// IssueToken creates a real OAuth token for the given toolset, with the
// specified upstream (external) credentials. The token is stored in the
// cache and will be found by oauth.Service.ValidateAccessToken.
func (ti *TokenIssuer) IssueToken(
	t *testing.T,
	ctx context.Context,
	toolsetID uuid.UUID,
	upstreamToken string,
	upstreamRefreshToken string,
	upstreamExpiresAt *time.Time,
	securityKeys []string,
) IssuedToken {
	t.Helper()

	client, err := ti.clientReg.RegisterClient(ctx, &oauth.ClientInfo{
		ClientName:   "test-client",
		RedirectURIs: []string{testRedirectURI},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
	}, testMCPURL)
	require.NoError(t, err)

	grant, err := ti.grantMgr.CreateAuthorizationGrant(ctx, &oauth.AuthorizationRequest{
		ResponseType: "code",
		ClientID:     client.ClientID,
		RedirectURI:  testRedirectURI,
		Scope:        "openid",
		State:        "test-state",
	}, testMCPURL, toolsetID, upstreamToken, upstreamRefreshToken, upstreamExpiresAt, securityKeys)
	require.NoError(t, err)

	token, err := ti.tokenService.ExchangeAuthorizationCode(ctx, &oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         grant.Code,
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		RedirectURI:  testRedirectURI,
	}, testMCPURL, toolsetID)
	require.NoError(t, err)

	return IssuedToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Token:        token,
	}
}

// ExpireToken modifies an existing token's ExpiresAt in the cache, simulating
// an expired Gram access token.
func (ti *TokenIssuer) ExpireToken(
	t *testing.T,
	ctx context.Context,
	toolsetID uuid.UUID,
	accessToken string,
	expiresAt time.Time,
) {
	t.Helper()

	hash := sha256.Sum256([]byte(accessToken))
	accessTokenHash := base64.RawURLEncoding.EncodeToString(hash[:])
	cacheKey := oauth.TokenCacheKey(toolsetID, accessTokenHash) + ":"

	var token oauth.Token
	err := ti.cacheAdapter.Get(ctx, cacheKey, &token)
	require.NoError(t, err, "token not found in cache")

	token.ExpiresAt = expiresAt

	err = ti.cacheAdapter.Update(ctx, cacheKey, token)
	require.NoError(t, err, "update token expiry in cache")
}

// ExpireExternalSecrets modifies the ExpiresAt of all external secrets on a
// cached token, simulating expired upstream credentials.
func (ti *TokenIssuer) ExpireExternalSecrets(
	t *testing.T,
	ctx context.Context,
	toolsetID uuid.UUID,
	accessToken string,
	expiresAt time.Time,
) {
	t.Helper()

	hash := sha256.Sum256([]byte(accessToken))
	accessTokenHash := base64.RawURLEncoding.EncodeToString(hash[:])
	cacheKey := oauth.TokenCacheKey(toolsetID, accessTokenHash) + ":"

	var token oauth.Token
	err := ti.cacheAdapter.Get(ctx, cacheKey, &token)
	require.NoError(t, err, "token not found in cache")

	for i := range token.ExternalSecrets {
		token.ExternalSecrets[i].ExpiresAt = &expiresAt
	}

	err = ti.cacheAdapter.Update(ctx, cacheKey, token)
	require.NoError(t, err, "update external secrets expiry in cache")
}
