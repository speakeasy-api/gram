package remotesessions_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
)

// idTokenIssuer stands in for an OpenID provider: it publishes an ES256 key
// set over TLS and mints ID tokens signed with the matching private key.
type idTokenIssuer struct {
	// issuerURL is unique per issuer so the key-set fetch budget, which is
	// charged per issuer, is never shared between tests.
	issuerURL string
	jwksURI   string
	pool      *x509.CertPool
	signer    jose.Signer
}

func newIDTokenIssuer(t *testing.T) *idTokenIssuer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     "synthetic-kid",
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}}}
	body, err := json.Marshal(set)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	return &idTokenIssuer{
		issuerURL: "https://" + uuid.NewString() + ".idp.example.com",
		jwksURI:   server.URL + "/jwks.json",
		pool:      pool,
		signer:    signer,
	}
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

// enrichmentDoc mirrors the shape of the enrichment column for assertions.
type enrichmentDoc struct {
	IDToken       map[string]json.RawMessage `json:"id_token"`
	TokenResponse map[string]json.RawMessage `json:"token_response"`
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
			`"id_token":"` + minted + `","ok":true,"team":{"id":"T1"},"bot_token":"xoxb-not-for-storage"}`))
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
	require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]))
	require.JSONEq(t, `{"id":"T1"}`, string(doc.TokenResponse["team"]))
	for _, member := range []string{"access_token", "refresh_token", "id_token", "bot_token"} {
		require.NotContains(t, doc.TokenResponse, member)
	}
	require.NotContains(t, string(sess.Enrichment), loadString(&rawIDToken), "the ID token itself is never persisted")
	require.NotContains(t, string(sess.Enrichment), "xoxb-not-for-storage")

	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, sess.UserSessionIssuerID)
	require.NoError(t, err)
	state := states[env.clientID]
	require.Equal(t, "grant-owner@example.com", state.ConnectedAs)
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
		// noVerifier leaves the manager without an ID token verifier.
		noVerifier bool
	}{
		{name: "wrong nonce", mutate: func(c map[string]any) { c["nonce"] = "stale" }},
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
			signer := issuer
			if tc.signer != nil {
				signer = tc.signer
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
					signer.mint(t, claims) + `","ok":true}`))
			}, opts...)

			sess := env.session
			require.False(t, sess.UpstreamSubject.Valid, "a rejected ID token leaves no identity behind")
			require.False(t, sess.UpstreamEmail.Valid)
			require.False(t, sess.UpstreamDisplayName.Valid)
			require.False(t, sess.IdentitySource.Valid)

			doc := decodeEnrichment(t, sess.Enrichment)
			require.Nil(t, doc.IDToken)
			require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]), "token response extras are kept regardless")

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
			body := `{"access_token":"access-` + n + `","refresh_token":"refresh-` + n + `","token_type":"Bearer","expires_in":1,"ok":true`
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
	sess := reloadSession(t, env)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String, "a refresh without an ID token keeps the stored identity")
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]), "the exchange's claims survive a refresh that returned none")
	require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]))

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
		sess = reloadSession(t, env)
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
	sess = reloadSession(t, env)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Renamed Owner", sess.UpstreamDisplayName.String)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.JSONEq(t, `"Renamed Owner"`, string(doc.IDToken["name"]))
	require.JSONEq(t, `"https://idp.example.com/avatar.png"`, string(doc.IDToken["picture"]))
	require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]))

	// What a refresh restates replaces the stored value.
	renamed := issuer.claims(clientID, "")
	delete(renamed, "nonce")
	renamed["email"] = "renamed@example.com"
	tok := issuer.mint(t, renamed)
	refreshIDToken.Store(&tok)
	resolved, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-5", resolved)
	sess = reloadSession(t, env)
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
					issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `","ok":true}`))
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
