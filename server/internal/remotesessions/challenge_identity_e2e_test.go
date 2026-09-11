package remotesessions_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// idTokenIssuer stands in for an OpenID provider: it publishes an ES256 key
// set over TLS and mints ID tokens signed with the matching private key.
type idTokenIssuer struct {
	// issuerURL is unique per issuer so the key-set fetch budget, which is
	// charged per issuer, is never shared between tests.
	issuerURL string
	jwksURI   string
	pool      *x509.CertPool
	// cert is the key-set server's certificate, for a pool that trusts several fixtures.
	cert   *x509.Certificate
	signer jose.Signer
	key    *ecdsa.PrivateKey
	// rsaKey signs RS256 tokens; newSharedKidIDTokenIssuer publishes it under the ES256 key's kid.
	rsaKey *rsa.PrivateKey
	// keySet is the published JWK Set document.
	keySet []byte
	// fetches counts key-set downloads.
	fetches atomic.Int64
}

func newIDTokenIssuer(t *testing.T) *idTokenIssuer {
	t.Helper()

	return newIDTokenIssuerWithKeys(t, false)
}

// newSharedKidIDTokenIssuer publishes an undeclared-alg RSA key ahead of the
// ES256 key under the same kid, so a kid-only lookup picks the wrong one.
func newSharedKidIDTokenIssuer(t *testing.T) *idTokenIssuer {
	t.Helper()

	return newIDTokenIssuerWithKeys(t, true)
}

func newIDTokenIssuerWithKeys(t *testing.T, sharedKid bool) *idTokenIssuer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	keys := []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     "synthetic-kid",
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}}
	var rsaKey *rsa.PrivateKey
	if sharedKid {
		rsaKey, err = rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		keys = append([]jose.JSONWebKey{{
			Key:       rsaKey.Public(),
			KeyID:     "synthetic-kid",
			Algorithm: "",
			Use:       "sig",
		}}, keys...)
	}
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	require.NoError(t, err)

	issuer := &idTokenIssuer{
		issuerURL: "https://" + uuid.NewString() + ".idp.example.com",
		jwksURI:   "",
		pool:      nil,
		cert:      nil,
		signer:    nil,
		key:       key,
		rsaKey:    rsaKey,
		keySet:    body,
		fetches:   atomic.Int64{},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		issuer.fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "synthetic-kid"),
	)
	require.NoError(t, err)

	issuer.jwksURI = server.URL + "/jwks.json"
	issuer.pool = pool
	issuer.cert = server.Certificate()
	issuer.signer = signer
	return issuer
}

// claims is an ID token body that verifies against the synthetic fixture
// for clientID and nonce, for tests to mutate one member at a time.
func (i *idTokenIssuer) claims(clientID, nonce string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":            i.issuerURL,
		"sub":            "user-123",
		"aud":            clientID,
		"exp":            now.Add(5 * time.Minute).Unix(),
		"iat":            now.Unix(),
		"nonce":          nonce,
		"email":          "grant-owner@example.com",
		"email_verified": true,
		"name":           "Grant Owner",
		"picture":        "https://idp.example.com/avatar.png",
		"sid":            "sid-abc",
		"auth_time":      now.Add(-time.Minute).Unix(),
	}
}

func (i *idTokenIssuer) mint(t *testing.T, claims map[string]any) string {
	t.Helper()

	raw, err := jwt.Signed(i.signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

// mintWithKid signs with the issuer's key under a kid its key set never published.
func (i *idTokenIssuer) mintWithKid(t *testing.T, kid string, claims map[string]any) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: i.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

// mintRS256 signs with the RSA key under the shared kid.
func (i *idTokenIssuer) mintRS256(t *testing.T, claims map[string]any) string {
	t.Helper()

	require.NotNil(t, i.rsaKey, "only newSharedKidIDTokenIssuer publishes an RSA key")
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: i.rsaKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "synthetic-kid"),
	)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

func mintHS256(t *testing.T, claims map[string]any) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte("synthetic-hmac-key-of-32-bytes!!")},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "synthetic-kid"),
	)
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

func mintUnsigned(t *testing.T, claims map[string]any) string {
	t.Helper()

	body, err := json.Marshal(claims)
	require.NoError(t, err)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"synthetic-kid"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + "."
}

// verifierFunc adapts a closure to IDTokenVerifier for withIDTokenVerifierWrapper.
type verifierFunc func(context.Context, string, remotesessions.IDTokenExpectation) (remotesessions.UpstreamIdentity, error)

func (f verifierFunc) Verify(ctx context.Context, raw string, expect remotesessions.IDTokenExpectation) (remotesessions.UpstreamIdentity, error) {
	identity, err := f(ctx, raw, expect)
	if err != nil {
		return identity, fmt.Errorf("wrapped verifier: %w", err)
	}
	return identity, nil
}

// enrichmentDoc mirrors the shape of the enrichment column for assertions.
type enrichmentDoc struct {
	IDToken       map[string]json.RawMessage `json:"id_token"`
	TokenResponse map[string]json.RawMessage `json:"token_response"`
	Userinfo      map[string]json.RawMessage `json:"userinfo"`
	Introspection map[string]json.RawMessage `json:"introspection"`
	Interfaces    map[string]struct {
		Status     string `json:"status"`
		HTTPStatus int    `json:"http_status"`
		Reason     string `json:"reason"`
	} `json:"interfaces"`
}

func decodeEnrichment(t *testing.T, raw []byte) enrichmentDoc {
	t.Helper()

	var doc enrichmentDoc
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func reloadSession(t *testing.T, env syntheticExpiryEnv) repo.RemoteSession {
	t.Helper()

	sess, err := env.q.GetActiveRemoteSession(t.Context(), repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	return sess
}

// restatedSession waits for the detached restatement a request-path refresh left behind, then reads the row.
func restatedSession(t *testing.T, env syntheticExpiryEnv) repo.RemoteSession {
	t.Helper()

	env.mgr.WaitIdentityRestatements()
	return reloadSession(t, env)
}

// refreshTokenHandler answers the code exchange with exchange and every refresh with the members
// currently in refreshBody, both expiring inside the skew so each resolve refreshes.
func refreshTokenHandler(t *testing.T, refreshes *atomic.Int64, exchange string, refreshBody *atomic.Pointer[string]) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"access-0","refresh_token":"refresh-0","token_type":"Bearer","expires_in":1` + exchange + `}`))
			return
		}
		n := strconv.FormatInt(refreshes.Add(1), 10)
		_, _ = w.Write([]byte(`{"access_token":"access-` + n + `","refresh_token":"refresh-` + n + `","token_type":"Bearer","expires_in":1` + loadString(refreshBody) + `}`))
	}
}

// loadString reads a string shared between the test and the fixture
// server's handler goroutine.
func loadString(p *atomic.Pointer[string]) string {
	if v := p.Load(); v != nil {
		return *v
	}
	return ""
}

// observeNonce records the nonce the authorize redirect carried, for the
// fixture server to echo in its ID token.
func observeNonce(into *atomic.Pointer[string]) syntheticLoginOption {
	return withAuthorizationURLObserver(func(u *url.URL) {
		nonce := u.Query().Get("nonce")
		into.Store(&nonce)
	})
}

func TestRemoteLoginCapturesIDTokenIdentity(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken"
	var nonce, rawIDToken atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "idtoken", func(w http.ResponseWriter, _ *http.Request) {
		minted := issuer.mint(t, issuer.claims(clientID, loadString(&nonce)))
		rawIDToken.Store(&minted)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600,` +
			`"id_token":"` + minted + `","app_id":"A1","team":{"id":"T1"},"bot_token":"xoxb-not-for-storage"}`))
	},
		withIDTokenIssuer(issuer),
		observeNonce(&nonce),
	)
	require.NotEmpty(t, loadString(&nonce), "the authorize request carries a nonce for the ID token to echo")

	sess := env.session
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)

	require.Equal(t, "Grant Owner", sess.UpstreamDisplayName.String)

	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	require.False(t, sess.ValidationStatus.Valid, "identity capture says nothing about token validity")
	require.False(t, sess.LastValidatedAt.Valid)

	doc := decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.JSONEq(t, `"user-123"`, string(doc.IDToken["sub"]))
	require.JSONEq(t, `"https://idp.example.com/avatar.png"`, string(doc.IDToken["picture"]), "claims without a column stay in the document")
	require.JSONEq(t, `"sid-abc"`, string(doc.IDToken["sid"]))
	require.JSONEq(t, `"A1"`, string(doc.TokenResponse["app_id"]))
	require.JSONEq(t, `{"id":"T1"}`, string(doc.TokenResponse["team"]))
	for _, member := range []string{"access_token", "refresh_token", "id_token", "bot_token"} {
		require.NotContains(t, doc.TokenResponse, member)
	}
	require.NotContains(t, string(sess.Enrichment), loadString(&rawIDToken), "the ID token itself is never persisted")
	require.NotContains(t, string(sess.Enrichment), "xoxb-not-for-storage")

	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, sess.UserSessionIssuerID)
	require.NoError(t, err)
	state := states[env.clientID]
	require.Equal(t, "Grant Owner · grant-owner@example.com", state.ConnectedAs)
	require.Equal(t, remotesessions.IdentitySourceIDToken, state.IdentitySource)

	// The tombstone keeps its credentials for upstream revocation but none
	// of the personal data.
	_, err = env.q.SoftDeleteRemoteSessionsByClientID(ctx, env.clientID)
	require.NoError(t, err)
	tombstone, err := env.q.GetRemoteSessionByIDIncludingDeleted(ctx, repo.GetRemoteSessionByIDIncludingDeletedParams{
		ID:        sess.ID,
		ProjectID: conv.ToNullUUID(env.projectID),
	})
	require.NoError(t, err)
	require.True(t, tombstone.Deleted)
	require.False(t, tombstone.UpstreamSubject.Valid, "revocation clears the identity columns")
	require.False(t, tombstone.UpstreamEmail.Valid)
	require.False(t, tombstone.UpstreamDisplayName.Valid)
	require.False(t, tombstone.IdentitySource.Valid)
	require.Nil(t, tombstone.Enrichment)
	require.NotEmpty(t, tombstone.AccessTokenEncrypted, "credentials stay for upstream revocation")
}

func TestRemoteLoginStoresSessionWithoutIdentityWhenIDTokenIsRejected(t *testing.T) {
	t.Parallel()

	foreign := newIDTokenIssuer(t)
	cases := []struct {
		name string
		// mutate edits an otherwise valid claim set.
		mutate func(claims map[string]any)
		// signer, when set, signs with a key the fixture issuer never published.
		signer *idTokenIssuer
		// mint, when set, serializes the claims itself.
		mint func(t *testing.T, claims map[string]any) string
		// noVerifier leaves the manager without an ID token verifier.
		noVerifier bool
	}{
		{name: "wrong nonce", mutate: func(c map[string]any) { c["nonce"] = "stale" }},
		{name: "missing nonce", mutate: func(c map[string]any) { delete(c, "nonce") }},
		{name: "nbf in the future", mutate: func(c map[string]any) { c["nbf"] = time.Now().Add(5 * time.Minute).Unix() }},
		{name: "iat in the future", mutate: func(c map[string]any) { c["iat"] = time.Now().Add(5 * time.Minute).Unix() }},
		{name: "unsigned", mint: mintUnsigned},
		{name: "hmac signed", mint: mintHS256},
		{name: "wrong audience", mutate: func(c map[string]any) { c["aud"] = "someone-else" }},
		{name: "wrong issuer", mutate: func(c map[string]any) { c["iss"] = "https://other.example.com" }},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-5 * time.Minute).Unix() }},
		{name: "missing subject", mutate: func(c map[string]any) { delete(c, "sub") }},
		{name: "several audiences without azp", mutate: func(c map[string]any) { c["aud"] = []any{c["aud"], "someone-else"} }},
		{name: "azp naming another client", mutate: func(c map[string]any) { c["azp"] = "someone-else" }},
		{name: "signed by an unpublished key", signer: foreign},
		{name: "verifier not configured", noVerifier: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			issuer := newIDTokenIssuer(t)
			mint := issuer.mint
			if tc.signer != nil {
				mint = tc.signer.mint
			}
			if tc.mint != nil {
				mint = tc.mint
			}
			suffix := fmt.Sprintf("idtoken-reject-%d", i)
			clientID := "synthetic-cid-" + suffix
			var nonce atomic.Pointer[string]
			opts := []syntheticLoginOption{observeNonce(&nonce)}
			if !tc.noVerifier {
				opts = append(opts, withIDTokenIssuer(issuer))
			}

			ctx, env := newSyntheticExpiryEnv(t, suffix, func(w http.ResponseWriter, _ *http.Request) {
				claims := issuer.claims(clientID, loadString(&nonce))
				if tc.mutate != nil {
					tc.mutate(claims)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
					mint(t, claims) + `","app_id":"A1"}`))
			}, opts...)

			sess := env.session
			require.False(t, sess.UpstreamSubject.Valid, "a rejected ID token leaves no identity behind")
			require.False(t, sess.UpstreamEmail.Valid)
			require.False(t, sess.UpstreamDisplayName.Valid)
			require.False(t, sess.IdentitySource.Valid)

			doc := decodeEnrichment(t, sess.Enrichment)
			require.Nil(t, doc.IDToken)
			require.JSONEq(t, `"A1"`, string(doc.TokenResponse["app_id"]), "token response extras are kept regardless")

			states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, sess.UserSessionIssuerID)
			require.NoError(t, err)
			state := states[env.clientID]
			require.Empty(t, state.ConnectedAs)
			require.Empty(t, state.IdentitySource)
		})
	}
}

func TestRefreshRestatesIdentityOnlyWhenIDTokenReturned(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-refresh"
	var nonce atomic.Pointer[string]
	var refreshes atomic.Int64
	var refreshIDToken atomic.Pointer[string]
	// expires_in of one second lands every stored token inside the refresh
	// skew, so each resolve goes back to the token endpoint.
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-refresh", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") == "refresh_token" {
			n := strconv.FormatInt(refreshes.Add(1), 10)
			body := `{"access_token":"access-` + n + `","refresh_token":"refresh-` + n + `","token_type":"Bearer","expires_in":1,"app_id":"A1"`
			if tok := refreshIDToken.Load(); tok != nil {
				body += `,"id_token":"` + *tok + `"`
			}
			_, _ = w.Write([]byte(body + `}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access-0","refresh_token":"refresh-0","token_type":"Bearer","expires_in":1,"id_token":"` +
			issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"}`))
	},
		withIDTokenIssuer(issuer),
		observeNonce(&nonce),
	)
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-1", resolved)
	require.EqualValues(t, 1, refreshes.Load())
	sess := restatedSession(t, env)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String, "a refresh without an ID token keeps the stored identity")
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]), "the exchange's claims survive a refresh that returned none")
	require.JSONEq(t, `"A1"`, string(doc.TokenResponse["app_id"]))

	// A token for another subject is rejected (§12.2), twice in a row; the
	// exchange identity stands.
	stranger := issuer.claims(clientID, "")
	delete(stranger, "nonce")
	stranger["sub"] = "user-456"
	stranger["email"] = "stranger@example.com"
	strangerToken := issuer.mint(t, stranger)
	refreshIDToken.Store(&strangerToken)
	for _, access := range []string{"access-2", "access-3"} {
		resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		require.NoError(t, err)
		require.Equal(t, access, resolved)
		sess = restatedSession(t, env)
		require.Equal(t, "user-123", sess.UpstreamSubject.String)
		require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
		require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
		doc = decodeEnrichment(t, sess.Enrichment)
		require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
		require.NotContains(t, string(sess.Enrichment), "stranger@example.com")
	}

	// Omitted claims are kept in both the columns and the document.
	partial := issuer.claims(clientID, "")
	delete(partial, "nonce")
	delete(partial, "email")
	delete(partial, "picture")
	partial["name"] = "Renamed Owner"
	partialToken := issuer.mint(t, partial)
	refreshIDToken.Store(&partialToken)
	resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-4", resolved)
	sess = restatedSession(t, env)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Renamed Owner", sess.UpstreamDisplayName.String)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.JSONEq(t, `"Renamed Owner"`, string(doc.IDToken["name"]))
	require.JSONEq(t, `"https://idp.example.com/avatar.png"`, string(doc.IDToken["picture"]))
	require.JSONEq(t, `"A1"`, string(doc.TokenResponse["app_id"]))

	// What a refresh restates replaces the stored value.
	renamed := issuer.claims(clientID, "")
	delete(renamed, "nonce")
	renamed["email"] = "renamed@example.com"
	tok := issuer.mint(t, renamed)
	refreshIDToken.Store(&tok)
	resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-5", resolved)
	sess = restatedSession(t, env)
	require.Equal(t, "renamed@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Grant Owner", sess.UpstreamDisplayName.String)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"renamed@example.com"`, string(doc.IDToken["email"]))
	require.NotContains(t, string(sess.Enrichment), tok)
}

// Every soft-delete path clears the identity and enrichment columns while
// keeping the credentials an upstream revocation still needs.
func TestRevocationPathsClearIdentity(t *testing.T) {
	t.Parallel()

	paths := map[string]func(t *testing.T, ctx context.Context, env syntheticExpiryEnv){
		"by-subject-and-client": func(t *testing.T, ctx context.Context, env syntheticExpiryEnv) {
			t.Helper()
			_, err := env.q.SoftDeleteRemoteSessionBySubjectAndClient(ctx, repo.SoftDeleteRemoteSessionBySubjectAndClientParams{
				SubjectUrn:            env.subject,
				RemoteSessionClientID: env.clientID,
				UserSessionIssuerID:   env.session.UserSessionIssuerID,
				ProjectID:             env.projectID,
			})
			require.NoError(t, err)
		},
		"revoke-by-id": func(t *testing.T, ctx context.Context, env syntheticExpiryEnv) {
			t.Helper()
			_, err := env.q.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
				ID:             env.session.ID,
				ProjectID:      env.projectID,
				OrganizationID: env.organizationID,
			})
			require.NoError(t, err)
		},
	}
	for name, revoke := range paths {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			issuer := newIDTokenIssuer(t)
			suffix := "idtoken-" + name
			clientID := "synthetic-cid-" + suffix
			var nonce atomic.Pointer[string]
			ctx, env := newSyntheticExpiryEnv(t, suffix, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600,"id_token":"` +
					issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `","app_id":"A1"}`))
			}, withIDTokenIssuer(issuer), observeNonce(&nonce))
			require.Equal(t, "user-123", env.session.UpstreamSubject.String)
			require.NotNil(t, env.session.Enrichment)

			revoke(t, ctx, env)

			tombstone, err := env.q.GetRemoteSessionByIDIncludingDeleted(ctx, repo.GetRemoteSessionByIDIncludingDeletedParams{
				ID:        env.session.ID,
				ProjectID: conv.ToNullUUID(env.projectID),
			})
			require.NoError(t, err)
			require.True(t, tombstone.Deleted)
			require.False(t, tombstone.UpstreamSubject.Valid)
			require.False(t, tombstone.UpstreamEmail.Valid)
			require.False(t, tombstone.UpstreamDisplayName.Valid)
			require.False(t, tombstone.IdentitySource.Valid)
			require.Nil(t, tombstone.Enrichment)
			require.NotEmpty(t, tombstone.AccessTokenEncrypted, "credentials stay for upstream revocation")
		})
	}
}

func TestRemoteLoginRefetchesKeySetForUnknownKid(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-unknown-kid"
	var nonce atomic.Pointer[string]
	// A fresh cached set last confirmed outside the negative-cache window, so the unknown kid re-fetches.
	keyCache := jwks.NewMemoryCache()
	require.NoError(t, keyCache.Put(t.Context(), issuer.jwksURI, jwks.CacheState{
		Document:    issuer.keySet,
		ETag:        "",
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now().Add(-time.Minute),
	}))
	_, env := newSyntheticExpiryEnv(t, "idtoken-unknown-kid", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			issuer.mintWithKid(t, "rotated-kid", issuer.claims(clientID, loadString(&nonce))) + `","app_id":"A1"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIDTokenKeyCache(keyCache))

	require.EqualValues(t, 1, issuer.fetches.Load(), "an unknown kid re-fetches the key set once")
	require.False(t, env.session.UpstreamSubject.Valid)
	require.False(t, env.session.IdentitySource.Valid)
	require.Nil(t, decodeEnrichment(t, env.session.Enrichment).IDToken)
}

func TestRemoteLoginAcceptsIDTokenNamingSeveralAudiencesWithAzp(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-azp"
	var nonce atomic.Pointer[string]
	_, env := newSyntheticExpiryEnv(t, "idtoken-azp", func(w http.ResponseWriter, _ *http.Request) {
		claims := issuer.claims(clientID, loadString(&nonce))
		claims["aud"] = []any{clientID, "someone-else"}
		claims["azp"] = clientID
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			issuer.mint(t, claims) + `"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce))

	require.Equal(t, "user-123", env.session.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)
	require.Equal(t, remotesessions.IdentitySourceIDToken, env.session.IdentitySource.String)
}

func TestRemoteLoginStoresIdentityWithoutOversizedEnrichment(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-oversized"
	var nonce atomic.Pointer[string]
	_, env := newSyntheticExpiryEnv(t, "idtoken-oversized", func(w http.ResponseWriter, _ *http.Request) {
		claims := issuer.claims(clientID, loadString(&nonce))
		claims["blob"] = strings.Repeat("x", 16<<10)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
			issuer.mint(t, claims) + `","app_id":"A1"}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce))

	require.Equal(t, "user-123", env.session.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)
	require.Equal(t, remotesessions.IdentitySourceIDToken, env.session.IdentitySource.String)
	require.Nil(t, env.session.Enrichment, "a document over the cap is dropped whole")
}

func TestRefreshGivesLegacySessionIdentity(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-legacy"
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-legacy", refreshTokenHandler(t, &refreshes, "", &refreshBody), withIDTokenIssuer(issuer))
	require.False(t, env.session.UpstreamSubject.Valid, "the exchange returned no ID token")
	require.False(t, env.session.IdentitySource.Valid)

	claims := issuer.claims(clientID, "")
	delete(claims, "nonce")
	body := `,"id_token":"` + issuer.mint(t, claims) + `"`
	refreshBody.Store(&body)
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-1", resolved)

	sess := restatedSession(t, env)
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Grant Owner", sess.UpstreamDisplayName.String)
	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	require.JSONEq(t, `"grant-owner@example.com"`, string(decodeEnrichment(t, sess.Enrichment).IDToken["email"]))
}

func TestReconnectDuringRestatementKeepsItsIdentity(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-reconnect-race"
	var nonce atomic.Pointer[string]
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	var env syntheticExpiryEnv
	var armed atomic.Bool
	// reconnectErr is written inside the restatement and read after the wait hook.
	var reconnectErr error
	// A reconnect lands between the refresh's token CAS and its identity CAS.
	reconnect := func(inner remotesessions.IDTokenVerifier) remotesessions.IDTokenVerifier {
		return verifierFunc(func(ctx context.Context, raw string, expect remotesessions.IDTokenExpectation) (remotesessions.UpstreamIdentity, error) {
			identity, err := inner.Verify(ctx, raw, expect)
			if err != nil {
				return identity, fmt.Errorf("verify: %w", err)
			}
			if !armed.Swap(false) {
				return identity, nil
			}
			current, gerr := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			if gerr != nil {
				reconnectErr = gerr
				return identity, nil
			}
			_, reconnectErr = env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
				SubjectUrn:             env.subject,
				UserSessionIssuerID:    current.UserSessionIssuerID,
				RemoteSessionClientID:  env.clientID,
				AccessTokenEncrypted:   current.AccessTokenEncrypted,
				AccessExpiresAt:        current.AccessExpiresAt,
				RefreshTokenEncrypted:  current.RefreshTokenEncrypted,
				AuthorizationExpiresAt: current.AuthorizationExpiresAt,
				RefreshExpiresAt:       current.RefreshExpiresAt,
				Scopes:                 current.Scopes,
				Resource:               current.Resource,
				AutoRefresh:            current.AutoRefresh,
				UpstreamSubject:        conv.ToPGText("user-123"),
				UpstreamEmail:          conv.ToPGText("reconnect@example.com"),
				UpstreamDisplayName:    conv.ToPGText("Reconnected Owner"),
				IdentitySource:         conv.ToPGText(remotesessions.IdentitySourceIDToken),
				Enrichment:             []byte(`{"id_token":{"sub":"user-123","email":"reconnect@example.com"}}`),
			})
			return identity, nil
		})
	}
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-reconnect-race", func(w http.ResponseWriter, r *http.Request) {
		exchange := ""
		if r.FormValue("grant_type") != "refresh_token" {
			exchange = `,"id_token":"` + issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"`
		}
		refreshTokenHandler(t, &refreshes, exchange, &refreshBody)(w, r)
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIDTokenVerifierWrapper(reconnect))
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)

	refreshed := issuer.claims(clientID, "")
	delete(refreshed, "nonce")
	refreshed["email"] = "refreshed@example.com"
	body := `,"id_token":"` + issuer.mint(t, refreshed) + `"`
	refreshBody.Store(&body)
	armed.Store(true)
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-1", resolved)

	sess := restatedSession(t, env)
	require.NoError(t, reconnectErr)
	require.False(t, armed.Load(), "the reconnect ran inside verification")
	require.Equal(t, "reconnect@example.com", sess.UpstreamEmail.String, "the reconnect's identity stands")
	require.Equal(t, "Reconnected Owner", sess.UpstreamDisplayName.String)
	require.JSONEq(t, `"reconnect@example.com"`, string(decodeEnrichment(t, sess.Enrichment).IDToken["email"]))
	require.NotContains(t, string(sess.Enrichment), "refreshed@example.com")
}

func TestRefreshDropsEmailVerifiedWhenEmailChanges(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-email-verified"
	var nonce atomic.Pointer[string]
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-email-verified", func(w http.ResponseWriter, r *http.Request) {
		exchange := ""
		if r.FormValue("grant_type") != "refresh_token" {
			exchange = `,"id_token":"` + issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"`
		}
		refreshTokenHandler(t, &refreshes, exchange, &refreshBody)(w, r)
	}, withIDTokenIssuer(issuer), observeNonce(&nonce))
	require.JSONEq(t, `true`, string(decodeEnrichment(t, env.session.Enrichment).IDToken["email_verified"]))

	restate := func(mutate func(map[string]any)) enrichmentDoc {
		t.Helper()
		claims := issuer.claims(clientID, "")
		delete(claims, "nonce")
		delete(claims, "email_verified")
		mutate(claims)
		body := `,"id_token":"` + issuer.mint(t, claims) + `"`
		refreshBody.Store(&body)
		_, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		require.NoError(t, err)
		return decodeEnrichment(t, restatedSession(t, env).Enrichment)
	}

	doc := restate(func(map[string]any) {})
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.JSONEq(t, `true`, string(doc.IDToken["email_verified"]), "the same email restated keeps its flag")

	doc = restate(func(c map[string]any) { c["email"] = "renamed@example.com" })
	require.JSONEq(t, `"renamed@example.com"`, string(doc.IDToken["email"]))
	require.NotContains(t, doc.IDToken, "email_verified", "a new email drops the stale flag")

	doc = restate(func(c map[string]any) { c["email"] = "renamed@example.com"; c["email_verified"] = false })
	require.JSONEq(t, `false`, string(doc.IDToken["email_verified"]))
	require.EqualValues(t, 3, refreshes.Load())
}

func TestRefreshWithUnchangedExtrasSkipsIdentityWrite(t *testing.T) {
	t.Parallel()

	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "unchanged-extras", refreshTokenHandler(t, &refreshes, `,"app_id":"A1","team":{"id":"T1","n":1}`, &refreshBody))
	before := decodeEnrichment(t, env.session.Enrichment)
	require.JSONEq(t, `{"id":"T1","n":1}`, string(before.TokenResponse["team"]))

	require.NoError(t, testrepo.New(env.db).InstallRemoteSessionIdentityWriteMarkerFixture(ctx))
	identityWrites := func(sess repo.RemoteSession) int {
		return strings.Count(sess.ValidationReason.String, "identity-write;")
	}

	same := `, "team": {"n": 1, "id": "T1"}, "app_id": "A1"`
	refreshBody.Store(&same)
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-1", resolved)
	sess := restatedSession(t, env)
	require.Equal(t, 0, identityWrites(sess), "reordered and reformatted extras are not a change")
	require.Equal(t, before, decodeEnrichment(t, sess.Enrichment))

	changed := `,"app_id":"A1","team":{"id":"T2","n":1}`
	refreshBody.Store(&changed)
	resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-2", resolved)
	sess = restatedSession(t, env)
	require.Equal(t, 1, identityWrites(sess))
	require.JSONEq(t, `{"id":"T2","n":1}`, string(decodeEnrichment(t, sess.Enrichment).TokenResponse["team"]))
}
