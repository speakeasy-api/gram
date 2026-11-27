package redisconn

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/redisconn/redisconntest"
)

const (
	testToken    = "access-token-1"
	testPassword = "secret"
)

// tlsServer starts an instance that serves TLS with a certificate ca signed for
// 127.0.0.1 and requires the IAM user's token.
func tlsServer(t *testing.T, ca *redisconntest.CA, token string) *miniredis.Miniredis {
	t.Helper()

	server, err := miniredis.RunTLS(ca.LoopbackServerConfig(t))
	require.NoError(t, err)
	t.Cleanup(server.Close)
	server.RequireUserAuth(IAMUsername, token)
	return server
}

func newClient(t *testing.T, connector *Connector) *redis.Client {
	t.Helper()

	client := redis.NewClient(connector.Options(&redis.Options{
		DialTimeout:     time.Second,
		MaxRetries:      -1,
		DisableIdentity: true,
	}))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestValidate(t *testing.T) {
	t.Parallel()

	ca := "-----BEGIN CERTIFICATE-----"
	cases := []struct {
		name     string
		endpoint Endpoint
		wantErr  string
	}{
		{name: "no auth", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthNone}},
		{name: "password", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthPassword, Password: testPassword}},
		{name: "iam over tls", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthIAM, CACertPEM: ca}},
		{name: "no address", endpoint: Endpoint{Auth: AuthNone}, wantErr: "no address"},
		{name: "address without port", endpoint: Endpoint{Addr: "10.0.0.1", Auth: AuthNone}, wantErr: "address"},
		{name: "empty host", endpoint: Endpoint{Addr: ":6379", Auth: AuthNone}, wantErr: "no host"},
		{name: "named port", endpoint: Endpoint{Addr: "10.0.0.1:redis", Auth: AuthNone}, wantErr: "invalid port"},
		{name: "port zero", endpoint: Endpoint{Addr: "10.0.0.1:0", Auth: AuthNone}, wantErr: "invalid port"},
		{name: "port out of range", endpoint: Endpoint{Addr: "10.0.0.1:65536", Auth: AuthNone}, wantErr: "invalid port"},
		{name: "unknown mode", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: "token"}, wantErr: "unknown auth mode"},
		{name: "empty mode", endpoint: Endpoint{Addr: "10.0.0.1:6379"}, wantErr: "unknown auth mode"},
		{name: "password mode without password", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthPassword}, wantErr: "has none"},
		{name: "none with password", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthNone, Password: testPassword}, wantErr: "auth mode is none"},
		{name: "iam with password", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthIAM, Password: testPassword, CACertPEM: ca}, wantErr: "auth mode is iam"},
		{name: "iam without tls", endpoint: Endpoint{Addr: "10.0.0.1:6379", Auth: AuthIAM}, wantErr: ErrTokenOverPlaintext.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.endpoint.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestPasswordEndpoint(t *testing.T) {
	t.Parallel()

	require.Equal(t, AuthPassword, PasswordEndpoint("10.0.0.1:6379", testPassword).Auth)
	require.Equal(t, AuthNone, PasswordEndpoint("10.0.0.1:6379", "").Auth)
	require.False(t, PasswordEndpoint("10.0.0.1:6379", testPassword).TLS())
}

func TestNewConnectorRequiresATokenSourceForIAM(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	_, err := NewConnector(Endpoint{Addr: "127.0.0.1:6379", Auth: AuthIAM, CACertPEM: ca.PEM}, nil)
	require.ErrorContains(t, err, "no token source")
}

func TestNewConnectorRejectsACAWithoutCertificates(t *testing.T) {
	t.Parallel()

	_, err := NewConnector(Endpoint{Addr: "127.0.0.1:6379", Auth: AuthNone, CACertPEM: "not a certificate"}, nil)
	require.ErrorContains(t, err, "no PEM certificate")
}

func TestTLSConfigVerifiesTheServer(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	connector, err := NewConnector(Endpoint{Addr: "10.0.0.7:6379", Auth: AuthNone, CACertPEM: ca.PEM}, nil)
	require.NoError(t, err)

	config := connector.TLSConfig()
	require.NotNil(t, config)
	require.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
	require.False(t, config.InsecureSkipVerify)
	require.NotNil(t, config.RootCAs)
	require.Equal(t, "10.0.0.7", config.ServerName, "the name to verify defaults to the address's host")

	config.ServerName = "changed"
	require.Equal(t, "10.0.0.7", connector.TLSConfig().ServerName, "callers get a copy")
}

func TestPlaintextEndpointHasNoTLSConfig(t *testing.T) {
	t.Parallel()

	connector, err := NewConnector(PasswordEndpoint("10.0.0.7:6379", testPassword), nil)
	require.NoError(t, err)
	require.Nil(t, connector.TLSConfig())
}

func TestIAMClientAuthenticatesOverVerifiedTLS(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server := tlsServer(t, ca, testToken)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthIAM, CACertPEM: ca.PEM}, tokens)
	require.NoError(t, err)

	client := newClient(t, connector)
	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))

	opts := client.Options()
	require.Empty(t, opts.Password, "the token is read when a connection opens, not stored")
	require.NotNil(t, opts.CredentialsProviderContext)
}

func TestIAMClientUsesTheTokenCurrentWhenItConnects(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server := tlsServer(t, ca, testToken)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthIAM, CACertPEM: ca.PEM}, tokens)
	require.NoError(t, err)
	client := newClient(t, connector)
	require.NoError(t, client.Ping(t.Context()).Err())

	// The instance now wants a new token. An open connection stays
	// authenticated; a new one must present the new token.
	server.RequireUserAuth(IAMUsername, "access-token-2")
	tokens.Set("access-token-2", time.Hour)
	require.NoError(t, client.Close())

	fresh := newClient(t, connector)
	require.NoError(t, fresh.Ping(t.Context()).Err())
}

func TestIAMClientFailsWhenTheTokenCannotBeFetched(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server := tlsServer(t, ca, testToken)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)
	tokens.Fail(errors.New("metadata server unavailable"))

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthIAM, CACertPEM: ca.PEM}, tokens)
	require.NoError(t, err)

	err = newClient(t, connector).Ping(t.Context()).Err()
	require.ErrorContains(t, err, "metadata server unavailable")
}

func TestTLSClientRefusesAServerSignedByAnotherCA(t *testing.T) {
	t.Parallel()

	server := tlsServer(t, redisconntest.NewCA(t), testToken)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)

	other := redisconntest.NewCA(t)
	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthIAM, CACertPEM: other.PEM}, tokens)
	require.NoError(t, err)

	err = newClient(t, connector).Ping(t.Context()).Err()
	var unknown x509.UnknownAuthorityError
	require.ErrorAs(t, err, &unknown)
	require.Zero(t, tokens.Served(), "no token is fetched for a server that failed verification")
}

func TestTLSClientRefusesACertificateForAnotherName(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server := tlsServer(t, ca, testToken)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthIAM, CACertPEM: ca.PEM, ServerName: "10.9.9.9"}, tokens)
	require.NoError(t, err)

	err = newClient(t, connector).Ping(t.Context()).Err()
	var mismatch x509.HostnameError
	require.ErrorAs(t, err, &mismatch)
	require.Zero(t, tokens.Served())
}

func TestTLSClientAcceptsADNSServerName(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server, err := miniredis.RunTLS(ca.ServerConfig(t, nil, []string{"redis.internal.test"}))
	require.NoError(t, err)
	t.Cleanup(server.Close)

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthNone, CACertPEM: ca.PEM, ServerName: "redis.internal.test"}, nil)
	require.NoError(t, err)
	require.NoError(t, newClient(t, connector).Ping(t.Context()).Err())
}

func TestTLSClientRefusesTLSBelow12(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	config := ca.LoopbackServerConfig(t)
	config.MinVersion = tls.VersionTLS10
	config.MaxVersion = tls.VersionTLS11
	server, err := miniredis.RunTLS(config)
	require.NoError(t, err)
	t.Cleanup(server.Close)

	connector, err := NewConnector(Endpoint{Addr: server.Addr(), Auth: AuthNone, CACertPEM: ca.PEM}, nil)
	require.NoError(t, err)
	require.Error(t, newClient(t, connector).Ping(t.Context()).Err())
}

func TestPasswordClient(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	server.RequireAuth(testPassword)

	connector, err := NewConnector(PasswordEndpoint(server.Addr(), testPassword), nil)
	require.NoError(t, err)

	client := newClient(t, connector)
	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.Equal(t, testPassword, client.Options().Password)
	require.Nil(t, client.Options().TLSConfig)
}

func TestOptionsReplaceTheBaseEndpointAndCredentials(t *testing.T) {
	t.Parallel()

	connector, err := NewConnector(PasswordEndpoint("10.0.0.7:6379", ""), nil)
	require.NoError(t, err)

	opts := connector.Options(&redis.Options{
		Addr:                "10.0.0.1:6379",
		Username:            "someone",
		Password:            "stale",
		CredentialsProvider: func() (string, string) { return "someone", "stale" },
		TLSConfig:           &tls.Config{MinVersion: tls.VersionTLS13},
		PoolSize:            7,
	})
	require.Equal(t, "10.0.0.7:6379", opts.Addr)
	require.Empty(t, opts.Username)
	require.Empty(t, opts.Password)
	require.Nil(t, opts.CredentialsProvider)
	require.Nil(t, opts.TLSConfig)
	require.Equal(t, 7, opts.PoolSize, "other options are kept")
}

func TestCredentials(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	tokens := redisconntest.NewTokenSource(testToken, time.Hour)

	iam, err := NewConnector(Endpoint{Addr: "10.0.0.7:6379", Auth: AuthIAM, CACertPEM: ca.PEM}, tokens)
	require.NoError(t, err)
	user, pass, err := iam.Credentials(t.Context())
	require.NoError(t, err)
	require.Equal(t, IAMUsername, user)
	require.Equal(t, testToken, pass)

	password, err := NewConnector(PasswordEndpoint("10.0.0.7:6379", testPassword), tokens)
	require.NoError(t, err)
	user, pass, err = password.Credentials(t.Context())
	require.NoError(t, err)
	require.Empty(t, user)
	require.Equal(t, testPassword, pass)

	none, err := NewConnector(PasswordEndpoint("10.0.0.7:6379", ""), tokens)
	require.NoError(t, err)
	user, pass, err = none.Credentials(t.Context())
	require.NoError(t, err)
	require.Empty(t, user)
	require.Empty(t, pass)

	require.Equal(t, 1, tokens.Served(), "only the IAM endpoint asks for a token")
}

func TestCachedTokenSourceFetchesTheFirstTokenUpFront(t *testing.T) {
	t.Parallel()

	base := redisconntest.NewTokenSource(testToken, time.Hour)
	tokens, err := cachedTokenSource(t.Context(), base)
	require.NoError(t, err)
	require.Equal(t, 1, base.Served())

	token, err := tokens.Token()
	require.NoError(t, err)
	require.Equal(t, testToken, token.AccessToken)
	require.Equal(t, 1, base.Served(), "a token far from expiry is reused")
}

func TestCachedTokenSourceRefreshesBeforeExpiry(t *testing.T) {
	t.Parallel()

	// Valid, but inside the early-refresh window.
	base := redisconntest.NewTokenSource(testToken, tokenEarlyRefresh-time.Minute)
	tokens, err := cachedTokenSource(t.Context(), base)
	require.NoError(t, err)

	base.Set("access-token-2", time.Hour)
	token, err := tokens.Token()
	require.NoError(t, err)
	require.Equal(t, "access-token-2", token.AccessToken)
	require.Equal(t, 2, base.Served())
}

func TestCachedTokenSourceFailsWhenTheFirstFetchFails(t *testing.T) {
	t.Parallel()

	base := redisconntest.NewTokenSource(testToken, time.Hour)
	base.Fail(errors.New("no credentials"))

	_, err := cachedTokenSource(t.Context(), base)
	require.ErrorContains(t, err, "no credentials")
}

func TestCachedTokenSourceRejectsAnEmptyToken(t *testing.T) {
	t.Parallel()

	_, err := cachedTokenSource(t.Context(), redisconntest.NewTokenSource("", time.Hour))
	require.ErrorContains(t, err, "token is empty")
}
