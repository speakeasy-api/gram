package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// selfClientMisconfiguredDescription is the text an MCP client sees when
// the MCP server's own upstream credential cannot be used.
const selfClientMisconfiguredDescription = "The upstream credential this MCP server uses is misconfigured or was rejected by the upstream. Contact the MCP server administrator."

// scriptedClientCredentials answers Credential from scripted answers, in
// order with the last repeating, and counts Forget calls. err fails every
// call; remintErr fails every call after the first.
type scriptedClientCredentials struct {
	mu          sync.Mutex
	tokens      []string
	err         error
	remintErr   error
	forgettable bool
	minted      int
	forgets     int
}

func (s *scriptedClientCredentials) Credential(context.Context, remotesessions.ClientCredentialRequest) (remotesessions.ClientCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return remotesessions.ClientCredential{}, s.err
	}
	if s.remintErr != nil && s.minted > 0 {
		return remotesessions.ClientCredential{}, s.remintErr
	}
	token := s.tokens[min(s.minted, len(s.tokens)-1)]
	s.minted++

	return remotesessions.NewClientCredential(token, remotesessions.ClientCredentialSchemeBearer, time.Now().Add(time.Hour), func(context.Context) (bool, error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.forgets++
		return s.forgettable, nil
	}), nil
}

func (s *scriptedClientCredentials) counts() (minted, forgets int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.minted, s.forgets
}

// authorizationGate fronts an MCP upstream, recording the Authorization header
// of every MCP request and answering 401 to bearers it does not accept.
type authorizationGate struct {
	*httptest.Server

	mu   sync.Mutex
	seen []string
}

func newAuthorizationGate(t *testing.T, accept func(authorization string) bool) *authorizationGate {
	t.Helper()

	return newAuthorizationGateWithStatus(t, http.StatusUnauthorized, accept)
}

// newAuthorizationGateWithStatus is newAuthorizationGate refusing with status.
func newAuthorizationGateWithStatus(t *testing.T, status int, accept func(authorization string) bool) *authorizationGate {
	t.Helper()

	target, err := url.Parse(newStatelessRemoteMCPUpstream(t, "ping", nil).URL)
	require.NoError(t, err)
	upstream := httputil.NewSingleHostReverseProxy(target)

	gate := &authorizationGate{Server: nil, mu: sync.Mutex{}, seen: nil}
	gate.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Speakeasy's own protected resource metadata probe is anonymous and is
		// not an MCP request.
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			http.NotFound(w, r)
			return
		}

		authorization := r.Header.Get("Authorization")
		gate.mu.Lock()
		gate.seen = append(gate.seen, authorization)
		gate.mu.Unlock()

		if !accept(authorization) {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(status)
			return
		}
		upstream.ServeHTTP(w, r)
	}))
	t.Cleanup(gate.Close)

	return gate
}

func (g *authorizationGate) authorizations() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string(nil), g.seen...)
}

// selfClientEndpoint is an issuer-gated remote MCP endpoint whose upstream
// is reached with a self client's credential, and a bearer for a member.
type selfClientEndpoint struct {
	slug  string
	token string
}

func newSelfClientEndpoint(t *testing.T, ctx context.Context, ti *testInstance, upstreamURL string) selfClientEndpoint {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstreamURL, slug, "private", issuerID)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mcpServer.ID.String())

	client := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuerID)
	fixtures := testrepo.New(ti.conn)
	rows, err := fixtures.ForceRemoteSessionClientAuthMethodFixture(ctx, testrepo.ForceRemoteSessionClientAuthMethodFixtureParams{
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"),
		ID:                      client.ID,
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	rows, err = fixtures.ForceRemoteSessionClientCredentialOwnerFixture(ctx, testrepo.ForceRemoteSessionClientCredentialOwnerFixtureParams{
		CredentialOwner: string(remotesessions.CredentialOwnerSelf),
		ID:              client.ID,
		ProjectID:       conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:  conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	// The client handlers stamp the server's issuer after each binding change;
	// a self credential routes by it.
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{issuerID}))

	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject(mockidp.MockUserID),
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/x/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, urn.NewUserSubject(mockidp.MockUserID), jti)

	return selfClientEndpoint{slug: slug, token: token}
}

func toolsCallBody(t *testing.T) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params":  map[string]any{"name": "ping", "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	return body
}

func TestServePublic_SelfClientProxiesWithClientCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{tokens: []string{"self-token"}}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return true })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	// The member never connected the upstream, yet the call goes through.
	initResp, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, makeInitializeBody(), endpoint.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initResp.Code, "initialize: %s", initResp.Body.String())

	callResp, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call: %s", callResp.Body.String())
	decodeMCPResult(t, callResp.Body.Bytes())

	require.Equal(t, []string{"Bearer self-token", "Bearer self-token"}, gate.authorizations())
}

func TestServePublic_SelfClientRetriesOnceWithReplacementCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{tokens: []string{"revoked-token", "fresh-token"}, forgettable: true}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(authorization string) bool { return authorization == "Bearer fresh-token" })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	callResp, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call: %s", callResp.Body.String())
	decodeMCPResult(t, callResp.Body.Bytes())

	require.Equal(t, []string{"Bearer revoked-token", "Bearer fresh-token"}, gate.authorizations())
	minted, forgets := source.counts()
	require.Equal(t, 2, minted)
	require.Equal(t, 1, forgets)
}

func TestServePublic_SelfClientRejectedAfterRetryNamesAdministrator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{tokens: []string{"revoked-token", "also-revoked"}, forgettable: true}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return false })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.Equal(t, selfClientMisconfiguredDescription, requireOopsCode(t, err, oops.CodeFailedPrecondition))
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "no reauthorization repairs the MCP server's own credential")

	require.Equal(t, []string{"Bearer revoked-token", "Bearer also-revoked"}, gate.authorizations(), "exactly one retry")
}

func TestServePublic_SelfClientRejectionOfFreshCredentialIsFinal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	// The source keeps a credential it just minted: a replacement would be
	// rejected too.
	source := &scriptedClientCredentials{tokens: []string{"fresh-but-rejected"}, forgettable: false}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return false })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	_, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.Equal(t, selfClientMisconfiguredDescription, requireOopsCode(t, err, oops.CodeFailedPrecondition))

	require.Equal(t, []string{"Bearer fresh-but-rejected"}, gate.authorizations())
}

func TestServePublic_SelfClientCredentialFailureNamesAdministrator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{err: &remotesessions.TokenEndpointError{StatusCode: http.StatusUnauthorized, Code: oautherr.CodeInvalidClient}}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return true })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, makeInitializeBody(), endpoint.token, nil)
	require.Equal(t, selfClientMisconfiguredDescription, requireOopsCode(t, err, oops.CodeFailedPrecondition))
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Empty(t, gate.authorizations(), "the upstream is never called without its credential")
}

func TestServePublic_SelfClientCredentialOutageAsksForRetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{err: &remotesessions.TokenEndpointError{Transport: true}}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return true })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, makeInitializeBody(), endpoint.token, nil)
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}

func TestServePublic_SelfClientForbiddenNamesAdministrator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{tokens: []string{"under-scoped-token"}, forgettable: true}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGateWithStatus(t, http.StatusForbidden, func(string) bool { return false })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.Equal(t, selfClientMisconfiguredDescription, requireOopsCode(t, err, oops.CodeFailedPrecondition))
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Equal(t, []string{"Bearer under-scoped-token"}, gate.authorizations(), "a refusal that is not a rejected credential is not retried")
}

func TestServePublic_SelfClientRenewalOutageAsksForRetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	source := &scriptedClientCredentials{tokens: []string{"revoked-token"}, remintErr: &remotesessions.TokenEndpointError{Transport: true}, forgettable: true}
	ti.clientCredentials.use(source)
	gate := newAuthorizationGate(t, func(string) bool { return false })
	endpoint := newSelfClientEndpoint(t, ctx, ti, gate.URL)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Equal(t, []string{"Bearer revoked-token"}, gate.authorizations())
}
