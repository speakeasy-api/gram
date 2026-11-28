package atecred_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	credprovider "go.ate.dev/substrate/api/credprovider/v1"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/atecred"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBuildAndParseCredentialURI(t *testing.T) {
	t.Parallel()

	uri, err := atecred.BuildCredentialURI("prod-assistants", "spk-kit-actor-1", "uid-abc_123.45")
	require.NoError(t, err)
	require.Equal(t, "ate-secret://speakeasy.com/v1/actors/prod-assistants/spk-kit-actor-1/uid-abc_123.45", uri)

	ref, err := atecred.ParseCredentialURI(uri)
	require.NoError(t, err)
	require.Equal(t, atecred.ActorRef{
		Atespace:  "prod-assistants",
		ActorName: "spk-kit-actor-1",
		ActorUID:  "uid-abc_123.45",
	}, ref)

	badURIs := []struct {
		name string
		raw  string
	}{
		{"wrong scheme", "https://speakeasy.com/v1/actors/prod/actor-1/uid-1"},
		{"wrong provider", "ate-secret://k8s.io/v1/actors/prod/actor-1/uid-1"},
		{"wrong version", "ate-secret://speakeasy.com/v2/actors/prod/actor-1/uid-1"},
		{"missing uid", "ate-secret://speakeasy.com/v1/actors/prod/actor-1"},
		{"extra segment", "ate-secret://speakeasy.com/v1/actors/prod/actor-1/uid-1/extra"},
		{"query string", "ate-secret://speakeasy.com/v1/actors/prod/actor-1/uid-1?foo=bar"},
		{"fragment", "ate-secret://speakeasy.com/v1/actors/prod/actor-1/uid-1#frag"},
		{"percent encoded", "ate-secret://speakeasy.com/v1/actors/prod/actor-1/uid%2F1"},
		{"invalid atespace uppercase", "ate-secret://speakeasy.com/v1/actors/Prod/actor-1/uid-1"},
	}
	for _, tc := range badURIs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := atecred.ParseCredentialURI(tc.raw)
			require.Error(t, err)
		})
	}
}

func TestParseActorSPIFFEID(t *testing.T) {
	t.Parallel()

	atespace, actorName, err := atecred.ParseActorSPIFFEID("spiffe://substrate-actor.local/actor/prod-assistants/spk-kit-1")
	require.NoError(t, err)
	require.Equal(t, "prod-assistants", atespace)
	require.Equal(t, "spk-kit-1", actorName)

	for _, bad := range []string{
		"",
		"https://substrate-actor.local/actor/prod/a1",
		"spiffe://cluster.local/actor/prod/a1",
		"spiffe://substrate-actor.local/worker-for-actor/prod/a1",
		"spiffe://substrate-actor.local/actor/prod/a1/extra",
		"spiffe://substrate-actor.local/actor/Prod/a1",
	} {
		_, _, err := atecred.ParseActorSPIFFEID(bad)
		require.Error(t, err, bad)
	}
}

type stubIDTokenValidator struct {
	validate func(ctx context.Context, rawToken, audience string) (*idtoken.Payload, error)
}

func (s stubIDTokenValidator) Validate(ctx context.Context, rawToken, audience string) (*idtoken.Payload, error) {
	return s.validate(ctx, rawToken, audience)
}

func TestAuthenticator_GoogleIDTokenChecksAndLocalRules(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		aud = "https://api.speakeasy.com/internal/assistants/ate/credentials"
		sa  = "ate-cred-provider@speakeasy-prod.iam.gserviceaccount.com"
	)

	require.Error(t, atecred.ValidateServerAuthConfig("prod", "", ""))
	require.Error(t, atecred.ValidateServerAuthConfig("prod", sa, "local-secret"))
	require.NoError(t, atecred.ValidateServerAuthConfig("prod", sa, ""))
	require.NoError(t, atecred.ValidateServerAuthConfig("local", "", "local-secret"))
	require.Error(t, atecred.ValidateServerAuthConfig("local", "", ""))

	cases := []struct {
		name    string
		cfg     atecred.AuthenticatorConfig
		header  string
		wantErr bool
	}{
		{
			name: "valid Google ID token passes",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, raw, gotAud string) (*idtoken.Payload, error) {
					require.Equal(t, "good-token", raw)
					require.Equal(t, aud, gotAud)
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: aud,
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer good-token", wantErr: false,
		},
		{
			name: "wrong issuer rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://evil.example.com", Audience: aud,
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "wrong audience host rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: "https://other.speakeasy.com" + atecred.MintEndpointPath,
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "wrong audience path on same host rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: "https://api.speakeasy.com/internal/assistants/ate/other",
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "root-only audience without mint path rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: "https://api.speakeasy.com",
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "trailing-slash audience path rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: aud + "/",
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "unverified email rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: aud,
						Claims: map[string]any{"email": sa, "email_verified": false},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "wrong service account rejected",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa,
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: aud,
						Claims: map[string]any{"email": "other@speakeasy-prod.iam.gserviceaccount.com", "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "empty service account fails closed outside local",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: "",
				Validator: stubIDTokenValidator{validate: func(_ context.Context, _, _ string) (*idtoken.Payload, error) {
					return &idtoken.Payload{
						Issuer: "https://accounts.google.com", Audience: aud,
						Claims: map[string]any{"email": sa, "email_verified": true},
					}, nil
				}},
			},
			header: "Bearer tok", wantErr: true,
		},
		{
			name: "local token rejected outside local even if configured",
			cfg: atecred.AuthenticatorConfig{
				Environment: "prod", ExpectedAudience: aud, ServiceAccount: sa, LocalToken: "dev-only",
			},
			header: "Bearer dev-only", wantErr: true,
		},
		{
			name: "local token accepted in local environment",
			cfg: atecred.AuthenticatorConfig{
				Environment: "local", ExpectedAudience: aud, LocalToken: "dev-only",
			},
			header: "Bearer dev-only", wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := atecred.NewAuthenticator(tc.cfg).Authenticate(ctx, tc.header)
			if tc.wantErr {
				require.ErrorIs(t, err, atecred.ErrAuthRejected)
				return
			}
			require.NoError(t, err)
		})
	}
}

type fakeMinter struct {
	fn func(ctx context.Context, atespace, actorName, actorUID string) (string, time.Time, error)
}

func (f fakeMinter) MintATEActorCredential(ctx context.Context, atespace, actorName, actorUID string) (string, time.Time, error) {
	return f.fn(ctx, atespace, actorName, actorUID)
}

func TestProviderServer_CacheIdentityChecksErrorMappingAndNoCredentialLogging(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	const (
		atespace     = "prod-assistants"
		actorName    = "spk-kit-actor-1"
		actorUID     = "uid-actor-1"
		secretToken  = "eyJhbGciOiJSUzI1NiJ9.super-secret-turn-credential.sig"
		localAuthTok = "local-provider-auth-tok"
	)

	var (
		mu       sync.Mutex
		now      = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
		mintMode = "ok"
		mints    atomic.Int32
	)
	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	var srvURL string
	mux := http.NewServeMux()
	mux.Handle(atecred.MintEndpointPath, atecred.NewMintHandler(
		logger,
		atecred.NewAuthenticator(atecred.AuthenticatorConfig{
			Environment:      "local",
			ExpectedAudience: "",
			LocalToken:       localAuthTok,
		}),
		fakeMinter{fn: func(_ context.Context, gotAtespace, gotName, gotUID string) (string, time.Time, error) {
			mints.Add(1)
			require.Equal(t, atespace, gotAtespace)
			require.Equal(t, actorName, gotName)
			require.Equal(t, actorUID, gotUID)
			mu.Lock()
			mode := mintMode
			cur := now
			mu.Unlock()
			switch mode {
			case "denied":
				return "", time.Time{}, atecred.ErrActorCredentialDenied
			case "unavailable":
				return "", time.Time{}, atecred.ErrActorCredentialUnavailable
			default:
				return secretToken, cur.Add(5 * time.Minute), nil
			}
		}},
	))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	srvURL = ts.URL

	parsedURL, err := url.Parse(srvURL)
	require.NoError(t, err)
	endpoint, err := atecred.MintEndpointURL(parsedURL)
	require.NoError(t, err)

	client := atecred.NewHTTPMintClient(atecred.HTTPMintClientConfig{
		EndpointURL:    endpoint,
		TokenSource:    oauth2.StaticTokenSource(&oauth2.Token{AccessToken: localAuthTok}),
		HTTPClient:     ts.Client(),
		RequestTimeout: 3 * time.Second,
	})
	provider := atecred.NewServer(atecred.ServerConfig{
		Logger:   logger,
		Atespace: atespace,
		CacheTTL: time.Minute,
		Client:   client,
		Now:      getNow,
	})

	uri, err := atecred.BuildCredentialURI(atespace, actorName, actorUID)
	require.NoError(t, err)
	spiffeID := "spiffe://substrate-actor.local/actor/" + atespace + "/" + actorName

	// 1. First FetchSecret mints; second within 1-minute cache TTL hits cache.
	resp1, err := provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: uri, ActorSpiffeId: spiffeID})
	require.NoError(t, err)
	require.Equal(t, []byte(secretToken), resp1.GetOpaqueBytes())
	require.EqualValues(t, 1, mints.Load())

	resp2, err := provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: uri, ActorSpiffeId: spiffeID})
	require.NoError(t, err)
	require.Equal(t, []byte(secretToken), resp2.GetOpaqueBytes())
	require.EqualValues(t, 1, mints.Load(), "second fetch inside cache TTL must hit cache")

	// 2. Advancing past cache TTL re-mints.
	mu.Lock()
	now = now.Add(61 * time.Second)
	mu.Unlock()
	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: uri, ActorSpiffeId: spiffeID})
	require.NoError(t, err)
	require.EqualValues(t, 2, mints.Load())

	// 3. Mismatched SPIFFE ID / wrong atespace / worker-for-actor rejected with PermissionDenied.
	otherActorURI, err := atecred.BuildCredentialURI(atespace, "other-actor", "uid-other")
	require.NoError(t, err)
	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: otherActorURI, ActorSpiffeId: spiffeID})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	otherSpaceURI, err := atecred.BuildCredentialURI("other-space", actorName, actorUID)
	require.NoError(t, err)
	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{
		Uri:           otherSpaceURI,
		ActorSpiffeId: "spiffe://substrate-actor.local/actor/other-space/" + actorName,
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{
		Uri:           uri,
		ActorSpiffeId: "spiffe://substrate-actor.local/worker-for-actor/" + atespace + "/" + actorName,
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// 4. Server 403 maps to PermissionDenied; server 503 maps to Unavailable.
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mintMode = "denied"
	mu.Unlock()
	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: uri, ActorSpiffeId: spiffeID})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	mu.Lock()
	mintMode = "unavailable"
	mu.Unlock()
	_, err = provider.FetchSecret(ctx, &credprovider.FetchSecretRequest{Uri: uri, ActorSpiffeId: spiffeID})
	require.Equal(t, codes.Unavailable, status.Code(err))

	// 5. Credential must NEVER appear in provider or handler logs.
	require.NotContains(t, logBuf.String(), secretToken, "minted credential must never be logged")
}

func TestServerTLSCredentials_PinsInjectorSPIFFESAN(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	dir := t.TempDir()
	caKey, caCert, caPEM := generateTestCA(t)
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, caPEM, 0o600))

	serverBundle := filepath.Join(dir, "server-bundle.pem")
	writeSignedBundle(t, serverBundle, caKey, caCert, []string{"127.0.0.1", "localhost"}, nil)

	allowedCert := signClientCert(t, caKey, caCert, atecred.DefaultInjectorIdentity)
	wrongCert := signClientCert(t, caKey, caCert, "spiffe://cluster.local/ns/ate-system/sa/other-workload")

	creds, err := atecred.NewServerTLSCredentials(serverBundle, caFile, atecred.DefaultInjectorIdentity)
	require.NoError(t, err)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	grpcSrv := grpc.NewServer(grpc.Creds(creds))
	credprovider.RegisterCredentialProviderServer(grpcSrv, atecred.NewServer(atecred.ServerConfig{
		Logger:   testenv.NewLogger(t),
		Atespace: "prod-assistants",
		Client:   stubMintClient{cred: "tok-mtls-ok", exp: time.Now().Add(5 * time.Minute)},
	}))
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)

	caPool := x509.NewCertPool()
	require.True(t, caPool.AppendCertsFromPEM(caPEM))

	dialWith := func(clientCert tls.Certificate) credprovider.CredentialProviderClient {
		conn, dialErr := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      caPool,
			ServerName:   "localhost",
			Certificates: []tls.Certificate{clientCert},
		})))
		require.NoError(t, dialErr)
		t.Cleanup(func() { _ = conn.Close() })
		return credprovider.NewCredentialProviderClient(conn)
	}

	uri, err := atecred.BuildCredentialURI("prod-assistants", "actor-1", "uid-1")
	require.NoError(t, err)
	req := &credprovider.FetchSecretRequest{
		Uri:           uri,
		ActorSpiffeId: "spiffe://substrate-actor.local/actor/prod-assistants/actor-1",
	}

	okResp, err := dialWith(allowedCert).FetchSecret(ctx, req)
	require.NoError(t, err)
	require.Equal(t, []byte("tok-mtls-ok"), okResp.GetOpaqueBytes())

	_, err = dialWith(wrongCert).FetchSecret(ctx, req)
	require.Error(t, err, "caller with wrong SPIFFE SAN must be rejected at TLS handshake")
}

type stubMintClient struct {
	cred string
	exp  time.Time
}

func (s stubMintClient) Mint(_ context.Context, _ atecred.MintRequest) (atecred.MintResponse, error) {
	return atecred.MintResponse{Credential: s.cred, ExpiresAt: s.exp}, nil
}

func generateTestCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ate-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeSignedBundle(t *testing.T, path string, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, dnsNames []string, uriSANs []string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	var uris []*url.URL
	for _, raw := range uriSANs {
		u, parseErr := url.Parse(raw)
		require.NoError(t, parseErr)
		uris = append(uris, u)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-credprovider"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		URIs:         uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	var bundle bytes.Buffer
	require.NoError(t, pem.Encode(&bundle, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, pem.Encode(&bundle, &pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, os.WriteFile(path, bundle.Bytes(), 0o600))
}

func signClientCert(t *testing.T, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, spiffeURI string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	u, err := url.Parse(spiffeURI)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-injector"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	require.NoError(t, err)
	return cert
}

func TestMintEndpointURL(t *testing.T) {
	t.Parallel()
	want := fmt.Sprintf("https://api.dev.speakeasy.com%s", atecred.MintEndpointPath)
	for _, raw := range []string{
		"https://api.dev.speakeasy.com",
		"https://api.dev.speakeasy.com/",
		"https://api.dev.speakeasy.com///",
		"https://api.dev.speakeasy.com/extra?x=1",
	} {
		u, err := url.Parse(raw)
		require.NoError(t, err, raw)
		got, err := atecred.MintEndpointURL(u)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, bad := range []*url.URL{
		nil,
		{Scheme: "ftp", Host: "api.dev.speakeasy.com"},
		{Scheme: "https", Host: ""},
	} {
		_, err := atecred.MintEndpointURL(bad)
		require.Error(t, err)
	}
}

func TestMintHandler_AudienceNormalizationAndHostPathRefusal(t *testing.T) {
	t.Parallel()

	const sa = "ate-cred-provider@speakeasy-prod.iam.gserviceaccount.com"

	serverBase, err := url.Parse("https://api.speakeasy.com/")
	require.NoError(t, err)
	providerBase, err := url.Parse("https://api.speakeasy.com")
	require.NoError(t, err)

	serverAud, err := atecred.MintEndpointURL(serverBase)
	require.NoError(t, err)
	providerAud, err := atecred.MintEndpointURL(providerBase)
	require.NoError(t, err)
	require.Equal(t, serverAud, providerAud)

	// Simulate Google's idtoken.Validator verifying the token's aud claim against
	// the expected audience passed by the server.
	tokenAudiences := map[string]string{
		"tok-matching":       providerAud,
		"tok-different-host": "https://other.speakeasy.com" + atecred.MintEndpointPath,
		"tok-different-path": "https://api.speakeasy.com/internal/other",
		"tok-root-only":      "https://api.speakeasy.com",
	}
	auth := atecred.NewAuthenticator(atecred.AuthenticatorConfig{
		Environment:      "prod",
		ExpectedAudience: serverAud,
		ServiceAccount:   sa,
		Validator: stubIDTokenValidator{validate: func(_ context.Context, rawToken, expectedAud string) (*idtoken.Payload, error) {
			tokAud, ok := tokenAudiences[rawToken]
			if !ok {
				return nil, fmt.Errorf("unknown token")
			}
			if tokAud != expectedAud {
				return nil, fmt.Errorf("idtoken: audience provided does not match aud claim in the JWT")
			}
			return &idtoken.Payload{
				Issuer:   "https://accounts.google.com",
				Audience: tokAud,
				Claims:   map[string]any{"email": sa, "email_verified": true},
			}, nil
		}},
	})
	handler := atecred.NewMintHandler(testenv.NewLogger(t), auth, fakeMinter{
		fn: func(_ context.Context, atespace, actorName, actorUID string) (string, time.Time, error) {
			return "minted-" + atespace + "-" + actorName + "-" + actorUID, time.Now().Add(5 * time.Minute), nil
		},
	})

	call := func(tok string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, atecred.MintEndpointPath, bytes.NewBufferString(
			`{"atespace":"prod-assistants","actor_name":"spk-kit-1","actor_uid":"uid-1"}`,
		))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, call("tok-matching").Code)
	require.Equal(t, http.StatusUnauthorized, call("tok-different-host").Code)
	require.Equal(t, http.StatusUnauthorized, call("tok-different-path").Code)
	require.Equal(t, http.StatusUnauthorized, call("tok-root-only").Code)
}
