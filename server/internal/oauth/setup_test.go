package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

const testMCPURL = "http://test.example.com/mcp/test"
const testRedirectURI = "http://localhost:8080/callback"

// tokenTestEnv provides a lightweight test environment (Redis only, no DB)
// for exercising the TokenService, ClientRegistrationService, and GrantManager.
type tokenTestEnv struct {
	tokenService *oauth.TokenService
	clientReg    *oauth.ClientRegistrationService
	grantMgr     *oauth.GrantManager
	enc          *encryption.Client
	cacheAdapter cache.Cache
	logger       *slog.Logger
}

func newTokenTestEnv(t *testing.T) *tokenTestEnv {
	t.Helper()

	logger := testenv.NewLogger(t)
	enc := testenv.NewEncryptionClient(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	clientReg := oauth.NewClientRegistrationService(cacheAdapter, logger)
	pkceService := oauth.NewPKCEService(logger)
	grantMgr := oauth.NewGrantManager(cacheAdapter, clientReg, pkceService, logger, enc)
	tokenService := oauth.NewTokenService(cacheAdapter, clientReg, grantMgr, pkceService, logger, enc)

	return &tokenTestEnv{
		tokenService: tokenService,
		clientReg:    clientReg,
		grantMgr:     grantMgr,
		enc:          enc,
		cacheAdapter: cacheAdapter,
		logger:       logger,
	}
}

// issueToken registers a client, creates an authorization grant with the given
// upstream external secret, and exchanges the code for a downstream token.
func (e *tokenTestEnv) issueToken(
	t *testing.T,
	ctx context.Context,
	toolsetID uuid.UUID,
	upstreamToken, upstreamRefreshToken string,
	upstreamExpiresAt *time.Time,
	securityKeys []string,
) (token *oauth.Token, clientID, clientSecret string) {
	t.Helper()

	client, err := e.clientReg.RegisterClient(ctx, &oauth.ClientInfo{
		ClientName:   "test-client",
		RedirectURIs: []string{testRedirectURI},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
	}, testMCPURL)
	require.NoError(t, err)

	grant, err := e.grantMgr.CreateAuthorizationGrant(ctx, &oauth.AuthorizationRequest{
		ResponseType: "code",
		ClientID:     client.ClientID,
		RedirectURI:  testRedirectURI,
		Scope:        "openid",
		State:        "test-state",
	}, testMCPURL, toolsetID, upstreamToken, upstreamRefreshToken, upstreamExpiresAt, securityKeys)
	require.NoError(t, err)

	token, err = e.tokenService.ExchangeAuthorizationCode(ctx, &oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         grant.Code,
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		RedirectURI:  testRedirectURI,
	}, testMCPURL, toolsetID)
	require.NoError(t, err)

	return token, client.ClientID, client.ClientSecret
}

// expireTokenInCache modifies an existing token's ExpiresAt to be in the past,
// simulating an expired downstream token without using synctest.
func (e *tokenTestEnv) expireTokenInCache(
	t *testing.T,
	ctx context.Context,
	toolsetID uuid.UUID,
	accessToken string,
	expiresAt time.Time,
) {
	t.Helper()

	// Compute the cache key using the hashed access token
	hash := sha256.Sum256([]byte(accessToken))
	accessTokenHash := base64.RawURLEncoding.EncodeToString(hash[:])
	cacheKey := oauth.TokenCacheKey(toolsetID, accessTokenHash) + ":"

	// Get the current token from cache
	var token oauth.Token
	err := e.cacheAdapter.Get(ctx, cacheKey, &token)
	require.NoError(t, err, "failed to get token from cache")

	// Modify the expiry
	token.ExpiresAt = expiresAt

	// Update the token in cache
	err = e.cacheAdapter.Update(ctx, cacheKey, token)
	require.NoError(t, err, "failed to update token in cache")
}

// oauthServiceTestEnv wraps a full oauth.Service whose internal TokenService
// shares the same Redis-backed cache as the lightweight tokenTestEnv.
type oauthServiceTestEnv struct {
	*tokenTestEnv
	service *oauth.Service
}

func newOAuthServiceTestEnv(t *testing.T) *oauthServiceTestEnv {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := noop.NewMeterProvider()
	enc := testenv.NewEncryptionClient(t)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	serverURL, _ := url.Parse("http://0.0.0.0")

	// The oauth.Service is created with nil DB / sessions / environments because
	// RefreshProxyToken only needs the cache and the CustomProvider (which
	// resolves credentials from provider.Secrets directly).
	svc := oauth.NewService(logger, tracerProvider, meterProvider, nil, serverURL, cacheAdapter, enc, nil, nil)

	// Build a token test env on the SAME cache so tokens issued here are
	// visible to the service's internal TokenService.
	clientReg := oauth.NewClientRegistrationService(cacheAdapter, logger)
	pkceService := oauth.NewPKCEService(logger)
	grantMgr := oauth.NewGrantManager(cacheAdapter, clientReg, pkceService, logger, enc)
	tokenService := oauth.NewTokenService(cacheAdapter, clientReg, grantMgr, pkceService, logger, enc)

	return &oauthServiceTestEnv{
		tokenTestEnv: &tokenTestEnv{
			tokenService: tokenService,
			clientReg:    clientReg,
			grantMgr:     grantMgr,
			enc:          enc,
			cacheAdapter: cacheAdapter,
			logger:       logger,
		},
		service: svc,
	}
}

func newLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return testenv.NewLogger(t)
}
