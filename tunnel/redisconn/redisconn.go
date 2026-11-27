// Package redisconn describes how to reach one Redis or Valkey instance: its
// address, how it authenticates a connection, and whether the connection is
// encrypted. The server, the tunnel gateway, the instance switch and the
// command-line tools share it, so every client of an instance connects the
// same way.
//
// An endpoint authenticates with nothing, a static password, or a Google
// Cloud IAM access token sent as the password of the user "default", which is
// how Memorystore for Valkey accepts IAM authentication. A token is only ever
// sent over TLS that verifies the server against the endpoint's CA and name.
package redisconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// AuthMode is how an endpoint authenticates a new connection.
type AuthMode string

const (
	// AuthNone sends no credentials.
	AuthNone AuthMode = "none"

	// AuthPassword sends the endpoint's static password.
	AuthPassword AuthMode = "password"

	// AuthIAM sends a Google Cloud IAM access token as the password of
	// IAMUsername.
	AuthIAM AuthMode = "iam"
)

const (
	// IAMUsername is the user an IAM access token authenticates as on
	// Memorystore for Valkey.
	IAMUsername = "default"

	// cloudPlatformScope is the OAuth scope Memorystore requires of an IAM
	// access token.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

	// tokenEarlyRefresh is how long before its expiry a cached access token is
	// replaced. Tokens last an hour and the instance checks one only when a
	// connection authenticates, so five minutes leaves room for a slow metadata
	// server and clock skew without refreshing often.
	tokenEarlyRefresh = 5 * time.Minute
)

// ErrTokenOverPlaintext is returned for an IAM endpoint without TLS: its
// access token would cross the network unencrypted.
var ErrTokenOverPlaintext = errors.New("redis endpoint sends an IAM token but has no CA certificate for TLS")

// Endpoint describes one instance and how to connect to it.
type Endpoint struct {
	// Addr is the instance's host:port.
	Addr string

	// Auth is how a new connection authenticates.
	Auth AuthMode

	// Password is the static password for AuthPassword. It is empty for the
	// other modes.
	Password string

	// CACertPEM holds the PEM certificates that sign the instance's server
	// certificate. When it is set the connection uses TLS and trusts only
	// these certificates; when it is empty the connection is plaintext.
	CACertPEM string

	// ServerName is the name or IP address the server certificate must carry
	// in its subject alternative names. It defaults to the host in Addr.
	ServerName string
}

// PasswordEndpoint describes a plaintext instance that takes password, or no
// credentials when password is empty.
func PasswordEndpoint(addr, password string) Endpoint {
	auth := AuthPassword
	if password == "" {
		auth = AuthNone
	}
	return Endpoint{Addr: addr, Auth: auth, Password: password, CACertPEM: "", ServerName: ""}
}

// EndpointFromEnv reads the address, credentials and CA for a Redis endpoint.
// The prefix includes the final underscore.
func EndpointFromEnv(prefix string) Endpoint {
	endpoint := PasswordEndpoint(strings.TrimSpace(os.Getenv(prefix+"ADDR")), os.Getenv(prefix+"PASSWORD"))
	if auth := strings.TrimSpace(os.Getenv(prefix + "AUTH_MODE")); auth != "" {
		endpoint.Auth = AuthMode(auth)
	}
	endpoint.CACertPEM = os.Getenv(prefix + "CA_CERT")
	return endpoint
}

// TLS reports whether connections to the endpoint are encrypted.
func (e Endpoint) TLS() bool {
	return strings.TrimSpace(e.CACertPEM) != ""
}

// Validate reports a description that cannot be connected to safely.
func (e Endpoint) Validate() error {
	if e.Addr == "" {
		return errors.New("redis endpoint has no address")
	}
	host, port, err := net.SplitHostPort(e.Addr)
	if err != nil {
		return fmt.Errorf("redis endpoint address: %w", err)
	}
	// An empty host would also leave TLS with no name to verify.
	if host == "" {
		return errors.New("redis endpoint address has no host")
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("redis endpoint address has an invalid port %q", port)
	}

	switch e.Auth {
	case AuthNone:
		if e.Password != "" {
			return errors.New("redis endpoint has a password but its auth mode is none")
		}
	case AuthPassword:
		if e.Password == "" {
			return errors.New("redis endpoint auth mode is password but it has none")
		}
	case AuthIAM:
		if e.Password != "" {
			return errors.New("redis endpoint has a password but its auth mode is iam")
		}
		if !e.TLS() {
			return ErrTokenOverPlaintext
		}
	default:
		return fmt.Errorf("redis endpoint has unknown auth mode %q", e.Auth)
	}
	return nil
}

// tlsConfig builds the client TLS configuration, or returns nil for a
// plaintext endpoint.
func (e Endpoint) tlsConfig() (*tls.Config, error) {
	if !e.TLS() {
		return nil, nil
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(e.CACertPEM)) {
		return nil, errors.New("redis endpoint CA certificate holds no PEM certificate")
	}

	serverName := e.ServerName
	if serverName == "" {
		host, _, err := net.SplitHostPort(e.Addr)
		if err != nil {
			return nil, fmt.Errorf("redis endpoint address: %w", err)
		}
		serverName = host
	}

	// ServerName is checked against the certificate's DNS or IP subject
	// alternative names; verification is never skipped.
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: serverName,
	}, nil
}

// Connector binds an endpoint to the credentials it needs. It is safe for
// concurrent use.
type Connector struct {
	endpoint  Endpoint
	tlsConfig *tls.Config
	tokens    oauth2.TokenSource
}

// NewConnector checks endpoint and prepares its TLS configuration. tokens
// supplies access tokens for an IAM endpoint and is ignored otherwise.
func NewConnector(endpoint Endpoint, tokens oauth2.TokenSource) (*Connector, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if endpoint.Auth == AuthIAM && tokens == nil {
		return nil, errors.New("redis endpoint auth mode is iam but no token source was given")
	}

	config, err := endpoint.tlsConfig()
	if err != nil {
		return nil, err
	}
	if endpoint.Auth != AuthIAM {
		tokens = nil
	}

	return &Connector{endpoint: endpoint, tlsConfig: config, tokens: tokens}, nil
}

// Endpoint returns the endpoint the connector was built for.
func (c *Connector) Endpoint() Endpoint {
	return c.endpoint
}

// Addr returns the endpoint's host:port.
func (c *Connector) Addr() string {
	return c.endpoint.Addr
}

// TLSConfig returns a copy of the client TLS configuration, or nil for a
// plaintext endpoint.
func (c *Connector) TLSConfig() *tls.Config {
	if c.tlsConfig == nil {
		return nil
	}
	return c.tlsConfig.Clone()
}

// Credentials returns the username and password a new connection sends. Both
// are empty for an endpoint without authentication. An IAM token comes from
// the cache unless it is close to expiry; a refresh is bounded by the token
// source's own client, not by ctx.
func (c *Connector) Credentials(_ context.Context) (string, string, error) {
	switch c.endpoint.Auth {
	case AuthPassword:
		return "", c.endpoint.Password, nil
	case AuthIAM:
		token, err := c.tokens.Token()
		if err != nil {
			return "", "", fmt.Errorf("get redis iam token: %w", err)
		}
		return IAMUsername, token.AccessToken, nil
	case AuthNone:
		return "", "", nil
	default:
		return "", "", fmt.Errorf("redis endpoint has unknown auth mode %q", c.endpoint.Auth)
	}
}

// Options returns a copy of base that connects to the endpoint: its address,
// TLS configuration and credentials replace whatever base carried. Pool sizes,
// timeouts and the rest of base are kept. The client the options build always
// connects to this endpoint, so its credentials can be read at dial time.
func (c *Connector) Options(base *redis.Options) *redis.Options {
	opts := *base
	opts.Addr = c.endpoint.Addr
	opts.TLSConfig = c.TLSConfig()
	opts.Username = ""
	opts.Password = ""
	opts.CredentialsProvider = nil
	opts.CredentialsProviderContext = nil
	opts.StreamingCredentialsProvider = nil

	switch c.endpoint.Auth {
	case AuthPassword:
		opts.Password = c.endpoint.Password
	case AuthIAM:
		opts.CredentialsProviderContext = c.Credentials
	case AuthNone:
	}
	return &opts
}

// NewIAMTokenSource returns the process's Application Default Credentials for
// the cloud-platform scope (Workload Identity on GKE), cached and replaced
// tokenEarlyRefresh before expiry. It fetches the first token before
// returning, so missing credentials fail startup rather than the first
// connection. ctx bounds only that first fetch.
func NewIAMTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	// Token refreshes happen long after startup, so they must not inherit the
	// caller's cancellation.
	base, err := google.DefaultTokenSource(context.WithoutCancel(ctx), cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("find google default credentials: %w", err)
	}
	return cachedTokenSource(ctx, base)
}

// cachedTokenSource caches tokens from base, replacing each tokenEarlyRefresh
// before it expires, and fetches the first one before returning, giving up
// when ctx ends.
func cachedTokenSource(ctx context.Context, base oauth2.TokenSource) (oauth2.TokenSource, error) {
	tokens := oauth2.ReuseTokenSourceWithExpiry(nil, base, tokenEarlyRefresh)

	type result struct {
		token *oauth2.Token
		err   error
	}
	fetched := make(chan result, 1)
	go func() {
		token, err := tokens.Token()
		fetched <- result{token: token, err: err}
	}()

	select {
	case got := <-fetched:
		if got.err != nil {
			return nil, fmt.Errorf("fetch first redis iam token: %w", got.err)
		}
		if got.token.AccessToken == "" {
			return nil, errors.New("fetch first redis iam token: token is empty")
		}
		return tokens, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("fetch first redis iam token: %w", ctx.Err())
	}
}
