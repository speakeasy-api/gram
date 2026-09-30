package okta

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/dpop"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// specialSecret needs RFC 6749 §2.3.1 form-urlencoding before Basic encoding.
const specialSecret = "s3cr+t%2F/value"

func TestClient_ClientSecretBasic_EncodesCredentialsAndOmitsAssertion(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig(specialSecret))
	tc.stub.setApps(stubApps(1))

	require.Len(t, listApps(t, tc), 1)

	wantHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(stubClientID+":s3cr%2Bt%252F%2Fvalue"))
	headers := tc.stub.tokenAuthHeaders()
	require.Len(t, headers, 2, "nonce challenge, then token")
	for _, h := range headers {
		require.Equal(t, wantHeader, h)
	}
	for _, form := range tc.stub.recordedTokenForms() {
		require.Equal(t, oauthwire.GrantTypeClientCredentials, form.Get("grant_type"))
		require.Equal(t, "okta.apps.read okta.users.read okta.groups.read", form.Get("scope"))
		for _, param := range []string{oauthwire.ParamClientID, oauthwire.ParamClientSecret, oauthwire.ParamClientAssertion, oauthwire.ParamClientAssertionType} {
			require.False(t, form.Has(param), param)
		}
	}
	require.Equal(t, 0, tc.signer.Calls())
	require.Equal(t, 2, tc.decrypter.Calls(), "the secret is decrypted per token request")
}

func TestClient_ClientSecretBasic_DPoPTokenWithNonceRetry(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.stub.setApps(stubApps(1))

	require.Len(t, listApps(t, tc), 1)

	proofs := tokenProofs(tc)
	require.Len(t, proofs, 2)
	require.Empty(t, proofs[0].nonce)
	require.Equal(t, "nonce-1", proofs[1].nonce)
	require.NotEqual(t, proofs[0].jti, proofs[1].jti)
	require.Equal(t, []resourceRequest{{scheme: dpop.TokenType, dpopHeaders: 1}}, tc.stub.recordedResourceRequests())
}

func TestClient_ClientSecretBasic_BearerTokenSkipsProof(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.stub.setApps(stubApps(3))
	tc.stub.setPageSize(2)
	tc.stub.setTokenType("Bearer")

	require.Len(t, listApps(t, tc), 3)

	require.Len(t, tokenProofs(tc), 2, "token requests still carry a proof")
	require.Equal(t, []resourceRequest{
		{scheme: oauthwire.TokenTypeBearer, dpopHeaders: 0},
		{scheme: oauthwire.TokenTypeBearer, dpopHeaders: 0},
	}, tc.stub.recordedResourceRequests())
}

func TestClient_ClientSecretBasic_BearerRetries429(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.stub.setApps(stubApps(1))
	tc.stub.setTokenType("Bearer")
	tc.stub.setPending429(1)
	tc.stub.setRateLimit(100, 0, tc.clock.Now().Add(7*time.Second).Unix())

	require.Len(t, listApps(t, tc), 1)
	require.Equal(t, []time.Duration{7*time.Second + maxRateLimitJitter/2}, tc.sleeper.Waits())
	require.Len(t, tc.stub.recordedResourceRequests(), 2)
}

func TestClient_ClientSecretBasic_RequireDPoPRefusesBearer(t *testing.T) {
	t.Parallel()
	cfg := basicTestConfig("stub-secret")
	cfg.RequireDPoP = true
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), cfg)
	tc.stub.setApps(stubApps(1))
	tc.stub.setTokenType("Bearer")

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.EqualError(t, err, `okta token type "Bearer" is not accepted for client_secret_basic`)
	require.Empty(t, tc.stub.recordedResourceRequests(), "an unbound token is never presented")
}

func TestClient_ClientSecretBasic_WrongSecretIsInvalidClient(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.stub.setClientSecret("rotated-secret")

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	require.Equal(t, "invalid_client", apiErr.ErrorCode)
	require.NotContains(t, err.Error(), "stub-secret")
}

func TestClient_ClientSecretBasic_DecryptFailureSendsNothing(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.decrypter.setErr(errors.New("cipher: message authentication failed"))

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.ErrorIs(t, err, ErrClientSecretUndecryptable)
	require.NotContains(t, err.Error(), "stub-secret")
	require.NotContains(t, err.Error(), stubCiphertextPrefix)
	require.Equal(t, 0, tc.stub.counts().tokenRequests)
}

func TestClient_ClientSecretBasic_NeverLogsOrTracesSecret(t *testing.T) {
	t.Parallel()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil}))

	tc := newTestClient(t, provider, logger, basicTestConfig(specialSecret))
	tc.stub.setApps(stubApps(1))
	tc.stub.setPending429(1)
	tc.stub.setRateLimit(100, 5, tc.clock.Now().Add(time.Second).Unix())
	listApps(t, tc)
	tc.stub.setClientSecret("rotated-secret")
	tc.client.evictToken()
	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.Error(t, err)

	output := telemetryDump(t, exporter, &logs)
	for _, secret := range []string{specialSecret, "s3cr%2Bt%252F%2Fvalue", base64.StdEncoding.EncodeToString([]byte(stubClientID + ":s3cr%2Bt%252F%2Fvalue"))} {
		require.NotContains(t, output, secret)
		require.NotContains(t, err.Error(), secret)
	}
	for _, tok := range tc.stub.issuedTokens() {
		require.NotContains(t, output, tok)
	}
	require.NotContains(t, output, "Basic")
	require.NotContains(t, output, "Authorization")
	require.NotContains(t, output, "client_secret")
}

func TestNewClient_ValidatesPerAuthMethod(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	httpClient := policy.PooledClient()
	signer := &stubSigner{clock: &fakeClock{mu: sync.Mutex{}, now: time.Now()}, mu: sync.Mutex{}, calls: 0, requests: nil, jtis: nil, replay: false, last: ""}
	decrypter := &stubDecrypter{mu: sync.Mutex{}, err: nil, calls: 0}

	base := testConfig()
	base.OrgURL = "https://example.okta.com"
	base.ClientID = stubClientID
	base.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	base.RemoteSessionClientID = uuid.New()

	basic := basicTestConfig("stub-secret")
	basic.OrgURL, basic.ClientID, basic.AudienceFormat, basic.RemoteSessionClientID = base.OrgURL, base.ClientID, base.AudienceFormat, base.RemoteSessionClientID

	noKeySet := base
	noKeySet.JSONWebKeySetID = uuid.Nil
	noSecret := basic
	noSecret.ClientSecretEncrypted = ""
	post := basic
	post.AuthMethod = remotesessions.TokenEndpointAuthMethodPost

	cases := []struct {
		name      string
		cfg       Config
		signer    remotesessions.TokenEndpointAssertionSigner
		decrypter SecretDecrypter
		wantErr   string
	}{
		{name: "private_key_jwt by default", cfg: base, signer: signer, decrypter: nil, wantErr: ""},
		{name: "private_key_jwt without signer", cfg: base, signer: nil, decrypter: decrypter, wantErr: "okta: private_key_jwt requires an assertion signer"},
		{name: "private_key_jwt without key set", cfg: noKeySet, signer: signer, decrypter: decrypter, wantErr: "okta: private_key_jwt requires a key set"},
		{name: "client_secret_basic without signer or key set", cfg: basic, signer: nil, decrypter: decrypter, wantErr: ""},
		{name: "client_secret_basic without secret", cfg: noSecret, signer: signer, decrypter: decrypter, wantErr: "okta: client_secret_basic requires a client secret"},
		{name: "client_secret_basic without decrypter", cfg: basic, signer: signer, decrypter: nil, wantErr: "okta: client_secret_basic requires a secret decrypter"},
		{name: "client_secret_post unsupported", cfg: post, signer: signer, decrypter: decrypter, wantErr: `okta: unsupported token endpoint auth method "client_secret_post"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewClient(testenv.NewLogger(t), httpClient, tc.signer, tc.decrypter, tc.cfg)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "stub-secret")
		})
	}
}

func TestClientFactory_RebuildsOnConfigChange(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	factory := NewClientFactory(testenv.NewLogger(t), policy, nil, &stubDecrypter{mu: sync.Mutex{}, err: nil, calls: 0})

	cfg := basicTestConfig("stub-secret")
	cfg.OrgURL = "https://example.okta.com"
	cfg.ClientID = stubClientID
	cfg.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	cfg.RemoteSessionClientID = uuid.New()

	a, err := factory.Client(cfg)
	require.NoError(t, err)
	same, err := factory.Client(cfg)
	require.NoError(t, err)
	require.Same(t, a, same)

	replaced := cfg
	replaced.ClientSecretEncrypted = stubCiphertextPrefix + "replaced-secret"
	b, err := factory.Client(replaced)
	require.NoError(t, err)
	require.NotSame(t, a, b)
	again, err := factory.Client(replaced)
	require.NoError(t, err)
	require.Same(t, b, again)

	reissued := replaced
	reissued.ClientID = "0oa-new-client"
	c, err := factory.Client(reissued)
	require.NoError(t, err)
	require.NotSame(t, b, c)

	invalid := reissued
	invalid.ClientSecretEncrypted = ""
	_, err = factory.Client(invalid)
	require.Error(t, err)
	d, err := factory.Client(reissued)
	require.NoError(t, err)
	require.NotSame(t, c, d, "a failed rebuild drops the stale client")
}

func TestFake_RequireClientSecret(t *testing.T) {
	t.Parallel()
	const orgURL = "https://a.okta.com"
	factory := NewFakeFactory(map[string]Fixtures{orgURL: {Users: nil, Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: []string{"okta.apps.read"}}})
	fake := factory.Fake(orgURL)
	fake.RequireClientSecret("stub-secret", &stubDecrypter{mu: sync.Mutex{}, err: nil, calls: 0})

	cfg := basicTestConfig("stub-secret")
	cfg.OrgURL = orgURL
	client, err := factory.Client(cfg)
	require.NoError(t, err)
	_, err = client.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, fake.LastAuthMethod())

	cfg.ClientSecretEncrypted = stubCiphertextPrefix + "wrong-secret"
	client, err = factory.Client(cfg)
	require.NoError(t, err)
	_, err = client.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	require.Equal(t, "invalid_client", apiErr.ErrorCode)

	pkJWT := testConfig()
	pkJWT.OrgURL = orgURL
	client, err = factory.Client(pkJWT)
	require.NoError(t, err)
	_, err = client.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.NoError(t, err, "only client_secret_basic configs are checked")
	require.Equal(t, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, fake.LastAuthMethod())
}

func TestFakeFactory_ClientsKeepTheirOwnConfig(t *testing.T) {
	t.Parallel()
	const orgURL = "https://a.okta.com"
	factory := NewFakeFactory(map[string]Fixtures{orgURL: {Users: nil, Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: []string{"okta.apps.read"}}})
	factory.Fake(orgURL).RequireClientSecret("stub-secret", &stubDecrypter{mu: sync.Mutex{}, err: nil, calls: 0})

	good := basicTestConfig("stub-secret")
	good.OrgURL = orgURL
	goodClient, err := factory.Client(good)
	require.NoError(t, err)
	bad := good
	bad.ClientSecretEncrypted = stubCiphertextPrefix + "wrong-secret"
	badClient, err := factory.Client(bad)
	require.NoError(t, err)

	_, err = goodClient.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.NoError(t, err, "a later client for the same org does not change this client's secret")
	_, err = badClient.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.Error(t, err)
}

func TestFake_SetBearerOnly(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{Users: nil, Apps: nil, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: []string{"okta.apps.read"}})
	fake.SetBearerOnly(true)

	v, err := fake.VerifyScopes(t.Context(), []string{"okta.apps.read"})
	require.NoError(t, err)
	require.Empty(t, v.Missing)
	require.False(t, v.DPoPBound)
	require.False(t, v.ExpiresAt.IsZero())
}

func TestClient_ClientSecretBasic_LatchesDPoPAfterTokenExpiry(t *testing.T) {
	t.Parallel()
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), basicTestConfig("stub-secret"))
	tc.stub.setApps(stubApps(1))
	require.Len(t, listApps(t, tc), 1)
	tc.clock.advance(2 * time.Hour)
	tc.stub.setTokenType("Bearer")
	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.ErrorContains(t, err, "not accepted for client_secret_basic")
	require.Len(t, tc.stub.recordedResourceRequests(), 1, "downgraded token must never reach the resource")
}

// fakeCredentials is a comparable CredentialProvider whose pin and Observe
// outcome the test controls, standing in for the durable connection record.
type fakeCredentials struct {
	secret     string
	pinned     atomic.Bool
	observeErr atomic.Pointer[error]
	mu         sync.Mutex
	observed   []bool
}

func (f *fakeCredentials) Acquire(context.Context, Config) (CredentialLease, error) {
	return fakeLease{provider: f}, nil
}

func (f *fakeCredentials) RequireDPoP(context.Context, Config) (bool, error) {
	return f.pinned.Load(), nil
}

func (f *fakeCredentials) observations() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.observed)
}

type fakeLease struct{ provider *fakeCredentials }

func (l fakeLease) EncryptedSecret() string { return l.provider.secret }
func (l fakeLease) RequireDPoP() bool       { return l.provider.pinned.Load() }
func (l fakeLease) Close()                  {}
func (l fakeLease) Observe(_ context.Context, bound bool) error {
	if err := l.provider.observeErr.Load(); err != nil {
		return *err
	}
	l.provider.mu.Lock()
	defer l.provider.mu.Unlock()
	l.provider.observed = append(l.provider.observed, bound)
	if bound {
		l.provider.pinned.Store(true)
	}
	return nil
}

func TestClient_ClientSecretBasic_CachedBearerStopsAtConcurrentPin(t *testing.T) {
	t.Parallel()
	credentials := &fakeCredentials{secret: stubCiphertextPrefix + "stub-secret", pinned: atomic.Bool{}, observeErr: atomic.Pointer[error]{}, mu: sync.Mutex{}, observed: nil}
	cfg := basicTestConfig("stub-secret")
	cfg.Credentials = credentials
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), cfg)
	tc.stub.setApps(stubApps(1))
	tc.stub.setTokenType("Bearer")
	require.Len(t, listApps(t, tc), 1)
	require.Equal(t, []bool{false}, credentials.observations())
	exchanges := len(tc.stub.recordedTokenForms())

	// Another process pins the connection while this client's Bearer token is unexpired.
	credentials.pinned.Store(true)
	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.ErrorContains(t, err, "not accepted for client_secret_basic")
	require.Greater(t, len(tc.stub.recordedTokenForms()), exchanges, "the pinned client mints again instead of reusing Bearer")
	require.Len(t, tc.stub.recordedResourceRequests(), 1, "the cached Bearer token must not reach the resource after the pin")

	// Okta now binds tokens, so the same client recovers under the pin.
	tc.stub.setTokenType(dpop.TokenType)
	require.Len(t, listApps(t, tc), 1)
	require.Equal(t, dpop.TokenType, tc.stub.recordedResourceRequests()[1].scheme)
}

func TestClient_ClientSecretBasic_ObserveFailureDiscardsTokenWithoutLatching(t *testing.T) {
	t.Parallel()
	credentials := &fakeCredentials{secret: stubCiphertextPrefix + "stub-secret", pinned: atomic.Bool{}, observeErr: atomic.Pointer[error]{}, mu: sync.Mutex{}, observed: nil}
	cfg := basicTestConfig("stub-secret")
	cfg.Credentials = credentials
	tc := newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), cfg)
	tc.stub.setApps(stubApps(1))
	observeErr := errors.New("transient pin write failure")
	credentials.observeErr.Store(&observeErr)

	_, err := tc.client.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 0, MaxPages: 0})
	require.ErrorIs(t, err, observeErr)
	require.Empty(t, tc.stub.recordedResourceRequests(), "an unrecorded token must not reach the resource")
	require.False(t, tc.client.dpopObserved, "the in-memory latch must not run ahead of the durable record")

	// With the record unwritten, the client's requirement still follows the
	// record: a Bearer token is accepted until a binding is recorded.
	credentials.observeErr.Store(nil)
	tc.stub.setTokenType("Bearer")
	require.Len(t, listApps(t, tc), 1)
	require.Equal(t, []bool{false}, credentials.observations())
}
