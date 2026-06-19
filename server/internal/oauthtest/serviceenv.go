package oauthtest

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// OAuthServiceEnv wraps a full oauth.Service whose internal TokenService
// shares the same Redis-backed cache as the embedded TokenIssuer, so
// tokens minted via TokenIssuer.IssueToken are visible to the service's
// validation path.
type OAuthServiceEnv struct {
	*TokenIssuer
	Service *oauth.Service
}

// NewOAuthServiceEnv builds an OAuthServiceEnv backed by the given cache
// and encryption client. The oauth.Service is created with nil DB /
// sessions / environments — those dependencies aren't exercised by
// RefreshProxyToken or ValidateAccessToken, which resolve credentials
// from provider.Secrets and the cache directly.
func NewOAuthServiceEnv(t *testing.T, cacheAdapter cache.Cache, enc *encryption.Client) *OAuthServiceEnv {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	serverURL, err := url.Parse("http://0.0.0.0")
	require.NoError(t, err)

	svc := oauth.NewService(logger, tracerProvider, meterProvider, nil, serverURL, cacheAdapter, enc, nil, nil, nil, nil)

	return &OAuthServiceEnv{
		TokenIssuer: NewTokenIssuer(t, cacheAdapter, enc),
		Service:     svc,
	}
}
