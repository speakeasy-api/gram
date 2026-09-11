// Userinfo and introspection against a fake authorization server: identity
// precedence, the inactive verdict, and what each interface leaves on the row.

package remotesessions_test

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// enrichmentRequest is one request as the fake authorization server saw it.
type enrichmentRequest struct {
	path   string
	auth   string
	accept string
	form   map[string]string
}

// enrichmentAS serves a userinfo and an introspection endpoint over TLS,
// scripted per test and recording every request.
type enrichmentAS struct {
	userinfoURL      string
	introspectionURL string
	cert             *x509.Certificate

	mu         sync.Mutex
	userinfo   http.HandlerFunc
	introspect http.HandlerFunc
	requests   []enrichmentRequest
}

func newEnrichmentAS(t *testing.T) *enrichmentAS {
	t.Helper()

	as := &enrichmentAS{userinfoURL: "", introspectionURL: "", cert: nil, mu: sync.Mutex{}, userinfo: nil, introspect: nil, requests: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		as.serve(w, r, func() http.HandlerFunc { return as.userinfo })
	})
	mux.HandleFunc("/introspect", func(w http.ResponseWriter, r *http.Request) {
		as.serve(w, r, func() http.HandlerFunc { return as.introspect })
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	as.userinfoURL = server.URL + "/userinfo"
	as.introspectionURL = server.URL + "/introspect"
	as.cert = server.Certificate()
	return as
}

func (as *enrichmentAS) serve(w http.ResponseWriter, r *http.Request, pick func() http.HandlerFunc) {
	_ = r.ParseForm()
	form := map[string]string{}
	for name := range r.PostForm {
		form[name] = r.PostForm.Get(name)
	}
	as.mu.Lock()
	as.requests = append(as.requests, enrichmentRequest{path: r.URL.Path, auth: r.Header.Get("Authorization"), accept: r.Header.Get("Accept"), form: form})
	handler := pick()
	as.mu.Unlock()
	if handler == nil {
		http.Error(w, "not scripted", http.StatusNotFound)
		return
	}
	handler(w, r)
}

func (as *enrichmentAS) script(userinfo, introspect http.HandlerFunc) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.userinfo, as.introspect = userinfo, introspect
}

func (as *enrichmentAS) drain() []enrichmentRequest {
	as.mu.Lock()
	defer as.mu.Unlock()
	got := as.requests
	as.requests = nil
	return got
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func (env syntheticExpiryEnv) ref(sess repo.RemoteSession) remotesessions.RemoteSessionRef {
	return remotesessions.RemoteSessionRef{
		ID:             sess.ID,
		Subject:        env.subject,
		ClientID:       env.clientID,
		UpdatedAt:      sess.UpdatedAt.Time,
		ProjectID:      env.projectID,
		OrganizationID: env.organizationID,
	}
}

// plainTokenHandler answers the exchange with a bearer pair and no ID token.
func plainTokenHandler(refresh bool) http.HandlerFunc {
	body := `{"access_token":"access-plain","token_type":"Bearer","expires_in":3600,"workspace_name":"Acme Docs"}`
	if refresh {
		body = `{"access_token":"access-plain","refresh_token":"refresh-plain","token_type":"Bearer","expires_in":3600,"workspace_name":"Acme Docs"}`
	}
	return jsonHandler(http.StatusOK, body)
}

func TestRemoteLoginCapturesUserinfoIdentityWithoutIDToken(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com","name":"Grant Owner","picture":"https://idp.example.com/avatar.png"}`), nil)
	ctx, env := newSyntheticExpiryEnv(t, "userinfo", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))

	sess := env.session
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Grant Owner", sess.UpstreamDisplayName.String)
	require.Equal(t, remotesessions.IdentitySourceUserinfo, sess.IdentitySource.String)

	doc := decodeEnrichment(t, sess.Enrichment)
	require.Nil(t, doc.IDToken)
	require.JSONEq(t, `"https://idp.example.com/avatar.png"`, string(doc.Userinfo["picture"]))
	require.Equal(t, "ok", doc.Interfaces["userinfo"].Status)
	require.NotContains(t, doc.Interfaces, "introspection", "the exchange asks only userinfo; introspection runs on Verify")

	requests := as.drain()
	require.Len(t, requests, 1)
	require.Equal(t, "/userinfo", requests[0].path)
	require.Equal(t, "Bearer access-plain", requests[0].auth)

	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, sess.UserSessionIssuerID)
	require.NoError(t, err)
	require.Equal(t, "Grant Owner · grant-owner@example.com", states[env.clientID].ConnectedAs, "the identity line carries the name and the email")
	require.Equal(t, []string{"Acme Docs"}, states[env.clientID].AccountChips, "the provider chip rides beside it")
}

// A rejected ID token is not traded for a weaker identity: userinfo is not asked.
func TestRemoteLoginRejectedIDTokenDoesNotFallBackToUserinfo(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com"}`), nil)
	const clientID = "synthetic-cid-idtoken-no-fallback"
	_, env := newSyntheticExpiryEnv(t, "idtoken-no-fallback", func(w http.ResponseWriter, _ *http.Request) {
		claims := issuer.claims(clientID, "stale-nonce")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` + issuer.mint(t, claims) + `"}`))
	}, withIDTokenIssuer(issuer), withEnrichmentAS(as))

	require.False(t, env.session.IdentitySource.Valid)
	require.Empty(t, as.drain())
	require.Equal(t, "rejected", decodeEnrichment(t, env.session.Enrichment).Interfaces["id_token"].Status, "the rejection is recorded on the grant")

	// A later verify may store what userinfo said, but the rejected grant takes no identity from it.
	ctx := t.Context()
	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.False(t, sess.IdentitySource.Valid)
	require.False(t, sess.UpstreamSubject.Valid)
	require.False(t, sess.UpstreamEmail.Valid)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Equal(t, "ok", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "rejected", doc.Interfaces["id_token"].Status, "the marker survives the merge")
}

// The exchange's id token rejection outlives a token response over the cap.
func TestRemoteLoginRejectedIDTokenMarkerSurvivesExchangeOverflow(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com"}`), nil)
	const clientID = "synthetic-cid-idtoken-reject-overflow"
	bulk := strings.Repeat("x", remotesessions.MaxEnrichmentBytes)
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-reject-overflow", func(w http.ResponseWriter, _ *http.Request) {
		claims := issuer.claims(clientID, "stale-nonce")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"workspace_name":"` + bulk + `","id_token":"` + issuer.mint(t, claims) + `"}`))
	}, withIDTokenIssuer(issuer), withEnrichmentAS(as))

	require.False(t, env.session.IdentitySource.Valid)
	require.Empty(t, as.drain())
	doc := decodeEnrichment(t, env.session.Enrichment)
	require.Nil(t, doc.TokenResponse, "the oversized token response is dropped")
	require.Equal(t, "rejected", doc.Interfaces["id_token"].Status, "the rejection outlives the overflow")

	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.False(t, sess.IdentitySource.Valid, "the rejected grant takes no identity from userinfo")
	require.False(t, sess.UpstreamSubject.Valid)
	require.False(t, sess.UpstreamEmail.Valid)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.Equal(t, "ok", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "rejected", doc.Interfaces["id_token"].Status)
}

// The exchange's id token rejection outlives a refresh restatement whose merge overflows the SQL cap.
func TestRefreshOverflowKeepsRejectedIDTokenMarker(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com"}`), nil)
	const clientID = "synthetic-cid-idtoken-reject-refresh-overflow"
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	// Each document fits the cap on its own; their merge does not.
	half := strings.Repeat("x", 9<<10)
	exchange := `,"id_token":"` + issuer.mint(t, issuer.claims(clientID, "stale-nonce")) + `","workspace_name":"` + half + `"`
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-reject-refresh-overflow", refreshTokenHandler(t, &refreshes, exchange, &refreshBody), withIDTokenIssuer(issuer), withEnrichmentAS(as))
	require.False(t, env.session.IdentitySource.Valid)
	require.Equal(t, "rejected", decodeEnrichment(t, env.session.Enrichment).Interfaces["id_token"].Status)

	body := `,"hub_domain":"` + half + `"`
	refreshBody.Store(&body)
	_, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	sess := restatedSession(t, env)
	require.EqualValues(t, 1, refreshes.Load())
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Contains(t, doc.TokenResponse, "hub_domain")
	require.NotContains(t, doc.TokenResponse, "workspace_name", "the overflowing merge keeps only the incoming document")
	require.Equal(t, "rejected", doc.Interfaces["id_token"].Status, "the rejection outlives the overflow")

	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	sess = reloadSession(t, env)
	require.False(t, sess.IdentitySource.Valid, "the rejected grant takes no identity from userinfo")
	require.False(t, sess.UpstreamSubject.Valid)
	require.False(t, sess.UpstreamEmail.Valid)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.Equal(t, "ok", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "rejected", doc.Interfaces["id_token"].Status)
}

// A redirect from an enrichment endpoint is not followed: the bearer and client secret stay put.
func TestEnrichRemoteSessionRefusesRedirects(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	var leaked atomic.Int64
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { leaked.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(elsewhere.Close)
	redirect := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/leak", http.StatusTemporaryRedirect)
	}
	as.script(redirect, redirect)
	ctx, env := newSyntheticExpiryEnv(t, "enrich-redirect", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.False(t, upstream.Inactive)
	require.Zero(t, leaked.Load(), "nothing followed the redirect")
	doc := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	for _, name := range []string{"userinfo", "introspection"} {
		require.Equal(t, "failed", doc.Interfaces[name].Status, name)
		require.Equal(t, http.StatusTemporaryRedirect, doc.Interfaces[name].HTTPStatus, name)
	}
}

// A provider that echoes the same refresh token without a lifetime has not rotated it: the known
// deadline stands and the record of having introspected it is kept, even though the write re-runs.
func TestRefreshEchoedTokenKeepsDeadlineAndIntrospectionMarker(t *testing.T) {
	t.Parallel()

	// The exchange reports no refresh lifetime; introspecting the refresh token backfills the deadline.
	as := newEnrichmentAS(t)
	exp := strconv.FormatInt(time.Now().Add(30*24*time.Hour).Unix(), 10)
	as.script(nil, jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","exp":`+exp+`}`))
	var refreshes atomic.Int64
	ctx, env := newSyntheticExpiryEnv(t, "refresh-echoed-token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"access-0","refresh_token":"refresh-0","token_type":"Bearer","expires_in":1}`))
			return
		}
		refreshes.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-0","token_type":"Bearer","expires_in":1}`))
	}, withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	require.False(t, env.session.RefreshExpiresAt.Valid, "the exchange reported no refresh deadline")

	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	before := reloadSession(t, env)
	require.True(t, before.RefreshExpiresAt.Valid, "introspecting the refresh token backfilled the deadline")
	require.Equal(t, "ok", decodeEnrichment(t, before.Enrichment).Interfaces["refresh_introspection"].Status)

	_, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, refreshes.Load())
	after := restatedSession(t, env)
	require.Equal(t, before.RefreshTokenEncrypted.String, after.RefreshTokenEncrypted.String, "the echoed token keeps its stored ciphertext")
	require.True(t, after.RefreshExpiresAt.Valid, "the known deadline survives a refresh that reports none")
	require.WithinDuration(t, before.RefreshExpiresAt.Time, after.RefreshExpiresAt.Time, time.Second)
	doc := decodeEnrichment(t, after.Enrichment)
	require.Contains(t, doc.Interfaces, "refresh_introspection", "the same refresh token is not introspected again")
	require.NotContains(t, doc.Interfaces, "introspection", "the answer about the replaced access token is gone")
}

// Slack answers 200 with an error body; without a sub there is no identity.
func TestRemoteLoginUserinfoWithoutSubLeavesNoIdentity(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"ok":false,"error":"invalid_auth"}`), nil)
	_, env := newSyntheticExpiryEnv(t, "userinfo-nosub", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))

	sess := env.session
	require.False(t, sess.UpstreamSubject.Valid)
	require.False(t, sess.IdentitySource.Valid)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Nil(t, doc.Userinfo)
	require.Equal(t, "failed", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "no sub", doc.Interfaces["userinfo"].Reason)
	require.Equal(t, http.StatusOK, doc.Interfaces["userinfo"].HTTPStatus)
}

// A stored ID token identity outranks userinfo, so Verify does not spend a call on it.
func TestEnrichRemoteSessionKeepsIDTokenIdentityOverUserinfo(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"other-alias@example.com","name":"Other Alias"}`), nil)
	const clientID = "synthetic-cid-precedence"
	var nonce atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "precedence", func(w http.ResponseWriter, _ *http.Request) {
		minted := issuer.mint(t, issuer.claims(clientID, loadString(&nonce)))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` + minted + `"}`))
	}, withIDTokenIssuer(issuer), withEnrichmentAS(as), observeNonce(&nonce))
	require.Equal(t, remotesessions.IdentitySourceIDToken, env.session.IdentitySource.String)
	require.Empty(t, as.drain(), "an ID token identity leaves userinfo unasked at the exchange")

	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.False(t, upstream.Inactive)

	sess := reloadSession(t, env)
	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	require.Equal(t, "Grant Owner", sess.UpstreamDisplayName.String)
	require.Equal(t, env.session.UpdatedAt.Time, sess.UpdatedAt.Time, "enrichment leaves the CAS token alone")
	doc := decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.Nil(t, doc.Userinfo)
	require.NotContains(t, doc.Interfaces, "userinfo", "an outranked interface is not asked")
	for _, req := range as.drain() {
		require.NotEqual(t, "/userinfo", req.path)
	}
}

// A userinfo answer for another subject is someone else's identity: nothing it said is kept.
func TestEnrichRemoteSessionRejectsUserinfoForAnotherSubject(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com"}`), nil)
	ctx, env := newSyntheticExpiryEnv(t, "userinfo-mismatch", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String)

	as.script(jsonHandler(http.StatusOK, `{"sub":"someone-else","email":"intruder@example.com"}`), nil)
	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)

	sess := reloadSession(t, env)
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Nil(t, doc.Userinfo, "the mismatched answer is not stored")
	require.Equal(t, "failed", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "subject mismatch", doc.Interfaces["userinfo"].Reason)
	require.NotContains(t, string(sess.Enrichment), "intruder")
}

// A refused issuer budget is recorded as limited, not as a failure, and nothing else moves.
func TestEnrichRemoteSessionRecordsRateLimitedInterfaces(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"grant-owner@example.com"}`), jsonHandler(http.StatusOK, `{"active":false}`))
	ctx, env := newSyntheticExpiryEnv(t, "enrich-limited", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as), withEnrichmentRate(ratelimit.PerMinute(1)))
	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String, "the exchange spent the one token")
	as.drain()

	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.False(t, upstream.Inactive, "a refused introspection is no verdict")
	require.Empty(t, as.drain(), "nothing reached the issuer")
	doc := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.Equal(t, "limited", doc.Interfaces["userinfo"].Status)
	require.Equal(t, "limited", doc.Interfaces["introspection"].Status)
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.Userinfo["email"]), "a limited call retires nothing")
}

// Oversized answers are dropped, never what the exchange recorded.
func TestEnrichRemoteSessionOversizedAnswersKeepStoredDocument(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	bulk := strings.Repeat("x", 12<<10)
	ctx, env := newSyntheticExpiryEnv(t, "enrich-oversized", jsonHandler(http.StatusOK, `{"access_token":"access-plain","token_type":"Bearer","expires_in":3600,"workspace_name":"`+bulk+`"}`),
		withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	// The Go cap: an incoming document over the limit persists only its interface records.
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","name":"`+strings.Repeat("y", 20<<10)+`"}`), nil)
	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.Equal(t, remotesessions.IdentitySourceUserinfo, sess.IdentitySource.String, "the typed columns still take the identity")
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Nil(t, doc.Userinfo)
	require.Equal(t, "ok", doc.Interfaces["userinfo"].Status)
	require.Len(t, string(doc.TokenResponse["workspace_name"]), len(bulk)+2)

	// A stale answer is still retired when the document that retires it is over the cap.
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","name":"small"}`), jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","scope":"read"}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	doc = decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.JSONEq(t, `"read"`, string(doc.Introspection["scope"]))
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","name":"`+strings.Repeat("y", 20<<10)+`"}`), jsonHandler(http.StatusForbidden, `{}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	doc = decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.Nil(t, doc.Introspection, "the 403 retires the stored introspection answer despite the oversized userinfo answer")
	require.Equal(t, "failed", doc.Interfaces["introspection"].Status)

	// The SQL cap: a merge that would overflow keeps the stored document and takes only the interfaces.
	as.script(nil, jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","scope":"`+strings.Repeat("z", 6<<10)+`"}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	doc = decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.Len(t, string(doc.TokenResponse["workspace_name"]), len(bulk)+2, "the exchange's members survive")
	require.Nil(t, doc.Introspection)
	require.Equal(t, "ok", doc.Interfaces["introspection"].Status)
	require.Equal(t, "failed", doc.Interfaces["userinfo"].Status, "the unscripted userinfo endpoint answered 404")

	// The SQL cap keeps the nulls too: a stale answer is retired even when the merge would overflow.
	as.script(nil, jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","scope":"read"}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	stored := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.JSONEq(t, `"read"`, string(stored.Introspection["scope"]))
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","name":"`+strings.Repeat("w", 5<<10)+`"}`), jsonHandler(http.StatusForbidden, `{}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	doc = decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.Len(t, string(doc.TokenResponse["workspace_name"]), len(bulk)+2)
	require.Nil(t, doc.Userinfo, "the oversized merge drops the new answer")
	require.Nil(t, doc.Introspection, "and still retires the stale one")
	require.Equal(t, "failed", doc.Interfaces["introspection"].Status)
}

// A refresh introspection the provider failed is retried; one it answered without exp is asked once.
func TestEnrichRemoteSessionRefreshIntrospectionAskedOnce(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	var refreshAnswer atomic.Pointer[string]
	refreshAnswer.Store(conv.PtrEmpty(""))
	as.script(nil, func(w http.ResponseWriter, r *http.Request) {
		if r.PostForm.Get("token_type_hint") == "refresh_token" {
			if answer := loadString(&refreshAnswer); answer != "" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(answer))
				return
			}
			// A transport failure: the connection is dropped without a response.
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"sub":"user-123"}`))
	})
	ctx, env := newSyntheticExpiryEnv(t, "refresh-noexp", plainTokenHandler(true), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"access_token", "refresh_token"}, introspectionHints(as.drain()))
	sess := reloadSession(t, env)
	doc := decodeEnrichment(t, sess.Enrichment)
	require.Equal(t, "failed", doc.Interfaces["refresh_introspection"].Status)
	require.Equal(t, "unreachable", doc.Interfaces["refresh_introspection"].Reason)

	refreshAnswer.Store(conv.PtrEmpty(`{"active":true}`))
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"access_token", "refresh_token"}, introspectionHints(as.drain()), "a transport failure is retried")
	sess = reloadSession(t, env)
	require.False(t, sess.RefreshExpiresAt.Valid)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.Equal(t, "failed", doc.Interfaces["refresh_introspection"].Status)
	require.Equal(t, "no exp", doc.Interfaces["refresh_introspection"].Reason)

	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	require.Equal(t, []string{"access_token"}, introspectionHints(as.drain()), "a provider that reports no exp is not asked again")
}

func introspectionHints(requests []enrichmentRequest) []string {
	var hints []string
	for _, req := range requests {
		if req.path == "/introspect" {
			hints = append(hints, req.form["token_type_hint"])
		}
	}
	return hints
}

func TestEnrichRemoteSessionIntrospectionJSON(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	exp := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	as.script(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("token_type_hint") == "refresh_token" {
			_, _ = w.Write([]byte(`{"active":true,"exp":` + strconv.FormatInt(exp.Unix(), 10) + `}`))
			return
		}
		_, _ = w.Write([]byte(`{"active":true,"sub":"user-123","username":"grant","scope":"read write","client_id":"synthetic-cid-introspect","exp":` + strconv.FormatInt(exp.Unix(), 10) + `,"secret_member":"never stored"}`))
	})
	ctx, env := newSyntheticExpiryEnv(t, "introspect", plainTokenHandler(true), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	require.False(t, env.session.RefreshExpiresAt.Valid, "the exchange reported no refresh deadline")
	doc := decodeEnrichment(t, env.session.Enrichment)
	require.Equal(t, "failed", doc.Interfaces["userinfo"].Status, "the unscripted userinfo endpoint answers 404")
	require.Equal(t, http.StatusNotFound, doc.Interfaces["userinfo"].HTTPStatus)
	as.drain()

	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.False(t, upstream.Inactive)

	requests := as.drain()
	hints := map[string]string{}
	for _, req := range requests {
		if req.path == "/introspect" {
			hints[req.form["token_type_hint"]] = req.form["token"]
			require.Equal(t, "synthetic-cid-introspect", req.form["client_id"], "a public client identifies itself in the body")
			require.Contains(t, req.accept, "application/token-introspection+jwt")
		}
	}
	require.Equal(t, map[string]string{"access_token": "access-plain", "refresh_token": "refresh-plain"}, hints)

	sess := reloadSession(t, env)
	require.Equal(t, remotesessions.IdentitySourceIntrospection, sess.IdentitySource.String)
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.Equal(t, "grant", sess.UpstreamDisplayName.String)
	require.False(t, sess.UpstreamEmail.Valid)
	require.True(t, sess.RefreshExpiresAt.Valid)
	require.WithinDuration(t, exp, sess.RefreshExpiresAt.Time, time.Second)
	require.Equal(t, env.session.UpdatedAt.Time, sess.UpdatedAt.Time)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `"read write"`, string(doc.Introspection["scope"]))
	require.JSONEq(t, `true`, string(doc.Introspection["active"]))
	require.NotContains(t, doc.Introspection, "secret_member")
	require.Equal(t, "ok", doc.Interfaces["introspection"].Status)
	require.Equal(t, "failed", doc.Interfaces["userinfo"].Status)

	// The provider now reports the token dead: inactive, and the identity it once asserted stays.
	as.script(nil, jsonHandler(http.StatusOK, `{"active":false}`))
	upstream, err = env.mgr.EnrichRemoteSession(ctx, env.ref(sess))
	require.NoError(t, err)
	require.True(t, upstream.Inactive)
	sess = reloadSession(t, env)
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	doc = decodeEnrichment(t, sess.Enrichment)
	require.JSONEq(t, `false`, string(doc.Introspection["active"]))
	require.NotContains(t, doc.Introspection, "scope")
}

// A 403 (PostHog's answer for a dead token) and a body without active are not verdicts.
func TestEnrichRemoteSessionIntrospectionWithoutActiveIsUnknown(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(nil, jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","scope":"read"}`))
	ctx, env := newSyntheticExpiryEnv(t, "introspect-403", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()
	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	prior := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.JSONEq(t, `"read"`, string(prior.Introspection["scope"]))

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		status  int
		reason  string
	}{
		{name: "403", handler: jsonHandler(http.StatusForbidden, `{"detail":"revoked"}`), status: http.StatusForbidden, reason: "status 403"},
		{name: "no active member", handler: jsonHandler(http.StatusOK, `{"sub":"user-123"}`), status: http.StatusOK, reason: "no active member"},
		{name: "not json", handler: jsonHandler(http.StatusOK, `<html>`), status: http.StatusOK, reason: "unverifiable response"},
	} {
		as.script(nil, tc.handler)
		upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
		require.NoError(t, err, tc.name)
		require.False(t, upstream.Inactive, tc.name)
		sess := reloadSession(t, env)
		require.Equal(t, remotesessions.IdentitySourceIntrospection, sess.IdentitySource.String, tc.name)
		doc := decodeEnrichment(t, sess.Enrichment)
		require.Nil(t, doc.Introspection, "%s: a failed call retires the stale answer", tc.name)
		require.Equal(t, "failed", doc.Interfaces["introspection"].Status, tc.name)
		require.Equal(t, tc.status, doc.Interfaces["introspection"].HTTPStatus, tc.name)
		require.Equal(t, tc.reason, doc.Interfaces["introspection"].Reason, tc.name)
	}
}

// RFC 9701: a signed introspection response is trusted only once it verifies against the issuer's key set.
func TestEnrichRemoteSessionIntrospectionJWT(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	const clientID = "synthetic-cid-introspect-jwt"
	ctx, env := newSyntheticExpiryEnv(t, "introspect-jwt", plainTokenHandler(false), withIDTokenIssuer(issuer), withEnrichmentAS(as))
	as.drain()

	signed := func(t *testing.T, claims map[string]any, typ string) http.HandlerFunc {
		t.Helper()
		signer, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.ES256, Key: issuer.key},
			(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader(jose.HeaderKey("kid"), "synthetic-kid"),
		)
		require.NoError(t, err)
		raw, err := jwt.Signed(signer).Claims(claims).Serialize()
		require.NoError(t, err)
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/token-introspection+jwt")
			_, _ = w.Write([]byte(raw))
		}
	}
	valid := func() map[string]any {
		return map[string]any{
			"iss":                 issuer.issuerURL,
			"aud":                 clientID,
			"iat":                 time.Now().Unix(),
			"token_introspection": map[string]any{"active": false, "sub": "user-123"},
		}
	}

	as.script(nil, signed(t, valid(), "token-introspection+jwt"))
	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.True(t, upstream.Inactive)
	doc := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.JSONEq(t, `false`, string(doc.Introspection["active"]))
	require.Equal(t, "ok", doc.Interfaces["introspection"].Status)

	for _, tc := range []struct {
		name   string
		claims map[string]any
		typ    string
	}{
		{name: "wrong audience", claims: func() map[string]any { c := valid(); c["aud"] = "someone-else"; return c }(), typ: "token-introspection+jwt"},
		{name: "wrong issuer", claims: func() map[string]any { c := valid(); c["iss"] = "https://other.example.com"; return c }(), typ: "token-introspection+jwt"},
		{name: "missing iat", claims: func() map[string]any { c := valid(); delete(c, "iat"); return c }(), typ: "token-introspection+jwt"},
		{name: "iat too old", claims: func() map[string]any { c := valid(); c["iat"] = time.Now().Add(-10 * time.Minute).Unix(); return c }(), typ: "token-introspection+jwt"},
		{name: "wrong typ", claims: valid(), typ: "JWT"},
	} {
		as.script(nil, signed(t, tc.claims, tc.typ))
		upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
		require.NoError(t, err, tc.name)
		require.False(t, upstream.Inactive, tc.name)
		doc := decodeEnrichment(t, reloadSession(t, env).Enrichment)
		require.Equal(t, "failed", doc.Interfaces["introspection"].Status, tc.name)
		require.Equal(t, "unverifiable response", doc.Interfaces["introspection"].Reason, tc.name)
	}
}

// A 404 from an advertised endpoint asks for the issuer's metadata to be refreshed.
func TestEnrichRemoteSession404RequestsIssuerMetadataRefresh(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	var refreshed atomic.Int64
	var refreshedIssuer atomic.Pointer[uuid.UUID]
	ctx, env := newSyntheticExpiryEnv(t, "introspect-404", plainTokenHandler(false),
		withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as),
		withIssuerMetadataRefreshSeam(func(_ context.Context, issuerID uuid.UUID) {
			refreshed.Add(1)
			refreshedIssuer.Store(&issuerID)
		}),
	)
	require.EqualValues(t, 1, refreshed.Load(), "the unscripted userinfo endpoint answered 404 at the exchange")
	require.Equal(t, env.issuerID, *refreshedIssuer.Load())

	upstream, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.False(t, upstream.Inactive)
	require.EqualValues(t, 3, refreshed.Load(), "both unscripted endpoints answered 404 on verify")
	doc := decodeEnrichment(t, reloadSession(t, env).Enrichment)
	require.Equal(t, http.StatusNotFound, doc.Interfaces["userinfo"].HTTPStatus)
	require.Equal(t, http.StatusNotFound, doc.Interfaces["introspection"].HTTPStatus)
}

// A grant that moved since the verify started is not written to.
func TestEnrichRemoteSessionSkipsMovedGrant(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(nil, jsonHandler(http.StatusOK, `{"active":false}`))
	ctx, env := newSyntheticExpiryEnv(t, "introspect-moved", plainTokenHandler(false), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	stale := env.ref(env.session)
	stale.UpdatedAt = stale.UpdatedAt.Add(-time.Second)
	upstream, err := env.mgr.EnrichRemoteSession(ctx, stale)
	require.NoError(t, err)
	require.False(t, upstream.Inactive)
	require.Empty(t, as.drain(), "nothing is presented upstream for a grant that moved")
}

// A refresh replaces the access token, so the introspection answer about the old one is dropped with it.
func TestRefreshDropsStaleIntrospection(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(nil, jsonHandler(http.StatusOK, `{"active":true,"sub":"user-123","exp":`+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)+`}`))
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "refresh-drops-introspection", refreshTokenHandler(t, &refreshes, "", &refreshBody), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	before := reloadSession(t, env)
	token, ok := remotesessions.IntrospectedTokenFromEnrichment(before.Enrichment)
	require.True(t, ok)
	require.True(t, token.Active)
	require.Equal(t, remotesessions.IdentitySourceIntrospection, before.IdentitySource.String)

	// The exchange's token expires inside the skew, so resolving refreshes.
	_, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, refreshes.Load())

	after := restatedSession(t, env)
	require.True(t, after.UpdatedAt.Time.After(before.UpdatedAt.Time), "the refresh rotated the grant")
	_, ok = remotesessions.IntrospectedTokenFromEnrichment(after.Enrichment)
	require.False(t, ok, "the answer about the replaced token is gone")
	doc := decodeEnrichment(t, after.Enrichment)
	require.Nil(t, doc.Introspection)
	require.NotContains(t, doc.Interfaces, "introspection", "its interface stamp goes with it")
	require.Equal(t, "user-123", after.UpstreamSubject.String, "the identity it supplied stays")

	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, after.UserSessionIssuerID)
	require.NoError(t, err)
	require.Nil(t, states[env.clientID].Token, "the card has no token line until the next verify")

	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(after))
	require.NoError(t, err)
	_, ok = remotesessions.IntrospectedTokenFromEnrichment(reloadSession(t, env).Enrichment)
	require.True(t, ok, "the next verify introspects the new token")
}

// A rotated refresh token that reports no deadline is introspected afresh: the "asked once" record went with the old token.
func TestRefreshRotationReintrospectsTheNewRefreshToken(t *testing.T) {
	t.Parallel()

	as := newEnrichmentAS(t)
	as.script(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("token_type_hint") == "refresh_token" {
			_, _ = w.Write([]byte(`{"active":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"active":true,"sub":"user-123"}`))
	})
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "refresh-rotation-reintrospect", refreshTokenHandler(t, &refreshes, "", &refreshBody), withIDTokenIssuer(newIDTokenIssuer(t)), withEnrichmentAS(as))
	as.drain()

	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"access_token", "refresh_token"}, introspectionHints(as.drain()))
	before := reloadSession(t, env)
	require.Equal(t, "no exp", decodeEnrichment(t, before.Enrichment).Interfaces["refresh_introspection"].Reason)

	// The refresh rotates the refresh token and reports no refresh_expires_in.
	_, err = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, refreshes.Load())
	after := restatedSession(t, env)
	require.False(t, after.RefreshExpiresAt.Valid)
	require.NotEqual(t, before.RefreshTokenEncrypted.String, after.RefreshTokenEncrypted.String, "the refresh token rotated")
	require.NotContains(t, decodeEnrichment(t, after.Enrichment).Interfaces, "refresh_introspection", "the old token's record is gone")

	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(after))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"access_token", "refresh_token"}, introspectionHints(as.drain()), "the new refresh token is introspected")
	require.Equal(t, "no exp", decodeEnrichment(t, reloadSession(t, env).Enrichment).Interfaces["refresh_introspection"].Reason)
}
