package proxy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/identity"
	"github.com/stretchr/testify/require"
)

func assertionProxy(t *testing.T, remoteURL string) (*Proxy, context.Context, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), "https://gram.example", false)
	require.NoError(t, err)
	target := mcpauthz.Target{OrganizationID: "org_test", ProjectID: uuid.New(), TunnelID: uuid.New(), MCPServerID: uuid.New()}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: target.OrganizationID, ProjectID: &target.ProjectID})
	ctx = mcpidentity.NewValidatorBoundary().StampAPIKey(ctx, uuid.NewString())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return &Proxy{GuardianPolicy: policy, Logger: testenv.NewLogger(t), Tracer: testenv.NewTracerProvider(t).Tracer("test"), NonStreamingTimeout: time.Second, StreamingTimeout: time.Second, RemoteURL: remoteURL, MaxBufferedBodyBytes: DefaultMaxBufferedBodyBytes, Identity: ServerIdentity{TunneledMCPServerID: target.TunnelID.String(), McpServerID: target.MCPServerID.String()}, CallerAssertion: func(ctx context.Context, cred *identity.UpstreamCredential) (string, error) {
		return issuer.Mint(ctx, target, cred)
	}}, ctx, &key.PublicKey
}

func TestCallerAssertionStripsSpoofingWithAndWithoutSigning(t *testing.T) {
	t.Parallel()
	p, ctx, key := assertionProxy(t, "http://upstream.example")
	p.AuthorizationOverride = "upstream-oauth"
	p.Headers = []ConfiguredHeader{{Name: "X-Speakeasy-Identity", StaticValue: "configured-forgery"}, {Name: "x-speakeasy-identity", StaticValue: "configured-forgery"}, {Name: "X-Leaked-Assertion", ValueFromRequestHeader: "X_speakeasy_identity", IsRequired: true}}
	inbound := httptest.NewRequest(http.MethodPost, "http://gram.example/mcp", nil)
	inbound.Header = http.Header{"X-Speakeasy-Identity": {"forged"}, "X_speakeasy_identity": {"forged"}, "X-Speakeasy_Identity": {"forged"}, "x-speakeasy-identity": {"forged"}, "Authorization": {"Bearer gram-secret"}}
	inboundHeaders := inbound.Header.Clone()
	for _, enabled := range []bool{true, false} {
		if !enabled {
			p.CallerAssertion = nil
		}
		outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
		require.NoError(t, p.applyRequestHeaders(ctx, inbound, outbound))
		require.Equal(t, inboundHeaders, inbound.Header)
		require.Equal(t, "Bearer upstream-oauth", outbound.Header.Get("Authorization"))
		require.Empty(t, outbound.Header.Get("X_Speakeasy_Identity"))
		require.Empty(t, outbound.Header.Get("X-Leaked-Assertion"))
		if enabled {
			token, err := jwt.Parse(outbound.Header.Get(identity.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("https://gram.example"))
			require.NoError(t, err)
			require.True(t, token.Valid)
		} else {
			require.Empty(t, outbound.Header.Get(identity.Header))
		}
	}
	response := httptest.NewRecorder()
	applyResponseHeaders(response, &http.Response{Header: inbound.Header}, "")
	for name := range response.Header() {
		require.False(t, identity.ReservedHeader(name))
	}
}

func TestCallerAssertionRetryMintsFreshTokenAndPreservesOAuth(t *testing.T) {
	t.Parallel()
	received := make(chan http.Header, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	p, ctx, key := assertionProxy(t, upstream.URL)
	p.AuthorizationOverride = "upstream-oauth"
	p.UpstreamResponseRetryer = func(context.Context, *http.Response) (*UpstreamResponseRetry, error) {
		return &UpstreamResponseRetry{RemoteURL: upstream.URL, Headers: []ConfiguredHeader{{Name: "X-Retry", StaticValue: "yes"}}}, nil
	}
	req := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil)
	_, resp, err := p.forwardRequestWithRetry(ctx, req, func() io.Reader { return nil }, nil)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	first, second := <-received, <-received
	require.Equal(t, "yes", second.Get("X-Retry"))
	require.Equal(t, "Bearer upstream-oauth", second.Get("Authorization"))
	require.NotEqual(t, first.Get(identity.Header), second.Get(identity.Header))
	ids := make([]string, 0, 2)
	for _, header := range []http.Header{first, second} {
		token, err := jwt.Parse(header.Get(identity.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}))
		require.NoError(t, err)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(t, ok)
		jti, ok := claims["jti"].(string)
		require.True(t, ok)
		ids = append(ids, jti)
	}
	require.NotEqual(t, ids[0], ids[1])
}

func TestCallerAssertionNeverFollowsRedirect(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(second.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, second.URL, http.StatusFound) }))
	t.Cleanup(first.Close)
	p, ctx, _ := assertionProxy(t, first.URL)
	req := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil)
	_, resp, err := p.forwardRequest(ctx, req, nil, nil)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Zero(t, redirected.Load())
}

func TestCallerAssertionSigningFailureStopsForward(t *testing.T) {
	t.Parallel()
	p, ctx, _ := assertionProxy(t, "http://upstream.example")
	p.CallerAssertion = func(context.Context, *identity.UpstreamCredential) (string, error) { return "", io.ErrUnexpectedEOF }
	outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
	err := p.applyRequestHeaders(ctx, httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil), outbound)
	require.Error(t, err)
	require.Empty(t, outbound.Header.Get(identity.Header))
	// The response never includes a raw assertion or key.
	encoded, _ := json.Marshal(err)
	require.NotContains(t, string(encoded), "PRIVATE KEY")
}

func TestCallerAssertionThroughPostStripsInboundAndResponseEcho(t *testing.T) {
	t.Parallel()
	received := make(chan http.Header, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(identity.Header, r.Header.Get(identity.Header))
		w.Header().Set("X_Speakeasy_Identity", "echo-forgery")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)
	p, ctx, key := assertionProxy(t, upstream.URL)
	p.Headers = []ConfiguredHeader{{Name: identity.Header, StaticValue: "forged-config"}}
	for _, enabled := range []bool{true, false} {
		if !enabled {
			p.CallerAssertion = nil
		}
		request := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
		request.Header = http.Header{"X-Speakeasy-Identity": {"forged"}, "X_speakeasy_identity": {"forged"}, "X_Speakeasy-Identity": {"forged"}, "x-speakeasy-identity": {"forged"}, "Content-Type": {"application/json"}}
		response := httptest.NewRecorder()
		require.NoError(t, p.Post(response, request))
		require.Equal(t, http.StatusOK, response.Code)
		require.Empty(t, response.Header().Get(identity.Header))
		require.Empty(t, response.Header().Get("X_Speakeasy_Identity"))
		actual := <-received
		require.Empty(t, actual.Get("X_Speakeasy_Identity"))
		if enabled {
			_, err := jwt.Parse(actual.Get(identity.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}))
			require.NoError(t, err)
		} else {
			require.Empty(t, actual.Get(identity.Header))
		}
	}
}

func TestCallerAssertionTenantMismatchFailsBeforeUpstream(t *testing.T) {
	t.Parallel()
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	t.Cleanup(upstream.Close)
	p, ctx, _ := assertionProxy(t, upstream.URL)
	authenticated, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := *authenticated
	other.ProjectID = new(uuid.New())
	ctx = contextvalues.SetAuthContext(ctx, &other)
	request := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	err := p.Post(httptest.NewRecorder(), request)
	require.ErrorContains(t, err, "could not establish tunnel caller identity")
	require.Zero(t, forwarded.Load())
}

// syntheticCredential is a subject credential describing token.
func syntheticCredential(token string) *identity.UpstreamCredential {
	return &identity.UpstreamCredential{
		Owner:           identity.OwnerSubject,
		ClientID:        uuid.NewString(),
		GrantID:         uuid.NewString(),
		GrantGeneration: 1,
		TokenSHA256:     identity.TokenSHA256(token),
		TokenExpiresAt:  nil,
	}
}

// signedUpstreamCredential verifies the assertion in header and returns its
// upstream_credential claim, nil when the assertion carries none.
func signedUpstreamCredential(t *testing.T, header http.Header, key *rsa.PublicKey) map[string]any {
	t.Helper()
	token, err := jwt.Parse(header.Get(identity.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	raw, present := claims[identity.ClaimUpstreamCredential]
	if !present {
		return nil
	}
	cred, ok := raw.(map[string]any)
	require.True(t, ok)
	return cred
}

// sentBearerSHA256 is the hash of the bearer actually presented upstream.
func sentBearerSHA256(t *testing.T, header http.Header) string {
	t.Helper()
	bearer, ok := strings.CutPrefix(header.Get("Authorization"), "Bearer ")
	require.True(t, ok)
	return identity.TokenSHA256(bearer)
}

// retryingUpstream records each request's headers and, through the returned
// proxy, retries the first request once as retry describes.
func retryingUpstream(t *testing.T, retry *UpstreamResponseRetry) (*Proxy, context.Context, *rsa.PublicKey, chan http.Header) {
	t.Helper()
	received := make(chan http.Header, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	p, ctx, key := assertionProxy(t, upstream.URL)
	if retry.RemoteURL == "" {
		retry.RemoteURL = upstream.URL
	}
	p.UpstreamResponseRetryer = func(context.Context, *http.Response) (*UpstreamResponseRetry, error) { return retry, nil }
	return p, ctx, key, received
}

func forwardOnceWithRetry(t *testing.T, ctx context.Context, p *Proxy) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil)
	_, resp, err := p.forwardRequestWithRetry(ctx, req, func() io.Reader { return nil }, nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestCallerAssertionCredentialHashMatchesSentBearer(t *testing.T) {
	t.Parallel()
	p, ctx, key := assertionProxy(t, "http://upstream.example")
	cred := syntheticCredential("synthetic-routed-token")
	p.SetUpstreamAuthorization("synthetic-routed-token", cred)
	outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
	require.NoError(t, p.applyRequestHeaders(ctx, httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil), outbound))
	claim := signedUpstreamCredential(t, outbound.Header, key)
	require.Equal(t, sentBearerSHA256(t, outbound.Header), claim["token_sha256"])
	require.Equal(t, cred.ClientID, claim["client_id"])
	require.Equal(t, cred.GrantID, claim["grant_id"])
	require.InDelta(t, 1, claim["grant_generation"], 0)
}

func TestCallerAssertionRouteOnlyRetryKeepsCredential(t *testing.T) {
	t.Parallel()
	p, ctx, key, received := retryingUpstream(t, &UpstreamResponseRetry{RemoteURL: "", Headers: nil, AuthorizationOverride: "", UpstreamCredential: nil})
	cred := syntheticCredential("synthetic-routed-token")
	p.SetUpstreamAuthorization("synthetic-routed-token", cred)
	forwardOnceWithRetry(t, ctx, p)
	first, second := <-received, <-received
	require.Equal(t, first.Get("Authorization"), second.Get("Authorization"))
	firstClaim, secondClaim := signedUpstreamCredential(t, first, key), signedUpstreamCredential(t, second, key)
	require.Equal(t, firstClaim, secondClaim)
	require.Equal(t, sentBearerSHA256(t, second), secondClaim["token_sha256"])
}

func TestCallerAssertionBearerSwapRetryReplacesCredential(t *testing.T) {
	t.Parallel()
	renewed := syntheticCredential("synthetic-renewed-token")
	p, ctx, key, received := retryingUpstream(t, &UpstreamResponseRetry{RemoteURL: "", Headers: nil, AuthorizationOverride: "synthetic-renewed-token", UpstreamCredential: renewed})
	p.SetUpstreamAuthorization("synthetic-rejected-token", syntheticCredential("synthetic-rejected-token"))
	forwardOnceWithRetry(t, ctx, p)
	first, second := <-received, <-received
	require.Equal(t, sentBearerSHA256(t, first), signedUpstreamCredential(t, first, key)["token_sha256"])
	claim := signedUpstreamCredential(t, second, key)
	require.Equal(t, identity.TokenSHA256("synthetic-renewed-token"), sentBearerSHA256(t, second))
	require.Equal(t, sentBearerSHA256(t, second), claim["token_sha256"])
	require.Equal(t, renewed.ClientID, claim["client_id"])
}

func TestCallerAssertionBearerSwapRetryWithoutCredentialClearsIt(t *testing.T) {
	t.Parallel()
	p, ctx, key, received := retryingUpstream(t, &UpstreamResponseRetry{RemoteURL: "", Headers: nil, AuthorizationOverride: "synthetic-unattested-token", UpstreamCredential: nil})
	p.SetUpstreamAuthorization("synthetic-rejected-token", syntheticCredential("synthetic-rejected-token"))
	forwardOnceWithRetry(t, ctx, p)
	first, second := <-received, <-received
	require.NotNil(t, signedUpstreamCredential(t, first, key))
	require.Equal(t, identity.TokenSHA256("synthetic-unattested-token"), sentBearerSHA256(t, second))
	require.Nil(t, signedUpstreamCredential(t, second, key))
	require.Nil(t, p.UpstreamCredential)
}

func TestCallerAssertionCredentialMismatchStopsForward(t *testing.T) {
	t.Parallel()
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	t.Cleanup(upstream.Close)
	p, ctx, _ := assertionProxy(t, upstream.URL)
	p.SetUpstreamAuthorization("synthetic-sent-token", syntheticCredential("synthetic-other-token"))
	request := httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	err := p.Post(httptest.NewRecorder(), request)
	require.ErrorContains(t, err, "upstream credential does not match the forwarded bearer")
	require.Zero(t, forwarded.Load())
}

func TestCallerAssertionCredentialWithoutBearerIsRejected(t *testing.T) {
	t.Parallel()
	p, ctx, _ := assertionProxy(t, "http://upstream.example")
	p.SetUpstreamAuthorization("", syntheticCredential("synthetic-routed-token"))
	p.Headers = []ConfiguredHeader{{Name: "Authorization", StaticValue: "Bearer synthetic-routed-token"}}
	outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
	err := p.applyRequestHeaders(ctx, httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil), outbound)
	require.ErrorContains(t, err, "upstream credential does not match the forwarded bearer")
	require.Empty(t, outbound.Header.Get(identity.Header))
}

func TestCallerAssertionConfiguredAuthorizationIsNeverAttested(t *testing.T) {
	t.Parallel()
	p, ctx, key := assertionProxy(t, "http://upstream.example")
	p.Headers = []ConfiguredHeader{{Name: "Authorization", StaticValue: "Bearer synthetic-configured-token"}}
	outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
	require.NoError(t, p.applyRequestHeaders(ctx, httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil), outbound))
	require.Equal(t, identity.TokenSHA256("synthetic-configured-token"), sentBearerSHA256(t, outbound.Header))
	require.Nil(t, signedUpstreamCredential(t, outbound.Header, key))
}

func TestCallerAssertionRoutedBearerWinsConfiguredAuthorization(t *testing.T) {
	t.Parallel()
	p, ctx, key := assertionProxy(t, "http://upstream.example")
	p.Headers = []ConfiguredHeader{{Name: "Authorization", StaticValue: "Bearer synthetic-configured-token"}}
	p.SetUpstreamAuthorization("synthetic-routed-token", syntheticCredential("synthetic-routed-token"))
	outbound := httptest.NewRequest(http.MethodPost, p.RemoteURL, nil)
	require.NoError(t, p.applyRequestHeaders(ctx, httptest.NewRequest(http.MethodPost, "https://gram.example/mcp", nil), outbound))
	require.Equal(t, identity.TokenSHA256("synthetic-routed-token"), sentBearerSHA256(t, outbound.Header))
	require.Equal(t, sentBearerSHA256(t, outbound.Header), signedUpstreamCredential(t, outbound.Header, key)["token_sha256"])
}
