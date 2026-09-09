package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestRepeatedRefreshesKeepEnrichmentUnderCap(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-cap"
	var nonce atomic.Pointer[string]
	var refreshes atomic.Int64
	var refreshBody atomic.Pointer[string]
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-cap", func(w http.ResponseWriter, r *http.Request) {
		exchange := ""
		if r.FormValue("grant_type") != "refresh_token" {
			exchange = `,"id_token":"` + issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"`
		}
		refreshTokenHandler(t, &refreshes, exchange, &refreshBody)(w, r)
	}, withIDTokenIssuer(issuer), observeNonce(&nonce))
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)

	// Each refresh adds a distinct 3 KiB claim, so the merge would pass the cap on the sixth.
	const refreshCount = 8
	for n := 1; n <= refreshCount; n++ {
		claims := issuer.claims(clientID, "")
		delete(claims, "nonce")
		claims["blob_"+strconv.Itoa(n)] = strings.Repeat("x", 3<<10)
		body := `,"id_token":"` + issuer.mint(t, claims) + `"`
		refreshBody.Store(&body)
		_, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
		require.NoError(t, err)

		sess := restatedSession(t, env)
		require.LessOrEqual(t, len(sess.Enrichment), remotesessions.MaxEnrichmentBytes, "refresh %d", n)
		doc := decodeEnrichment(t, sess.Enrichment)
		require.Contains(t, doc.IDToken, "blob_"+strconv.Itoa(n), "the latest token's claims are present after refresh %d", n)
		require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
		require.Equal(t, "user-123", sess.UpstreamSubject.String)
	}
	require.EqualValues(t, refreshCount, refreshes.Load())

	doc := decodeEnrichment(t, restatedSession(t, env).Enrichment)
	require.NotContains(t, doc.IDToken, "blob_1", "a merge over the cap keeps only the incoming document")
	require.Contains(t, doc.IDToken, "blob_6")
	require.Contains(t, doc.IDToken, "blob_"+strconv.Itoa(refreshCount))
}

func TestOverlappingRefreshesKeepStoredIdentity(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const clientID = "synthetic-cid-idtoken-overlap"
	var nonce atomic.Pointer[string]
	var refreshes atomic.Int64
	var env syntheticExpiryEnv
	var armed atomic.Bool
	// nestedToken and nestedErr are written inside the restatement and read after the wait hook.
	var nestedToken atomic.Pointer[string]
	var nestedErr error
	// A second refresh, answered without an ID token, rotates the row while the first's ID token is verified.
	overlap := func(inner remotesessions.IDTokenVerifier) remotesessions.IDTokenVerifier {
		return verifierFunc(func(ctx context.Context, raw string, expect remotesessions.IDTokenExpectation) (remotesessions.UpstreamIdentity, error) {
			identity, err := inner.Verify(ctx, raw, expect)
			if err != nil {
				return identity, fmt.Errorf("verify: %w", err)
			}
			if !armed.Swap(false) {
				return identity, nil
			}
			var resolved string
			resolved, nestedErr = env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
			nestedToken.Store(&resolved)
			return identity, nil
		})
	}
	ctx, env := newSyntheticExpiryEnv(t, "idtoken-overlap", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"access-0","refresh_token":"refresh-0","token_type":"Bearer","expires_in":1,"id_token":"` +
				issuer.mint(t, issuer.claims(clientID, loadString(&nonce))) + `"}`))
			return
		}
		n := refreshes.Add(1)
		body := `{"access_token":"access-` + strconv.FormatInt(n, 10) + `","refresh_token":"refresh-` + strconv.FormatInt(n, 10) + `","token_type":"Bearer","expires_in":1`
		if n == 1 {
			refreshed := issuer.claims(clientID, "")
			delete(refreshed, "nonce")
			refreshed["email"] = "refreshed@example.com"
			body += `,"id_token":"` + issuer.mint(t, refreshed) + `"`
		}
		_, _ = w.Write([]byte(body + `}`))
	}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIDTokenVerifierWrapper(overlap))
	require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)

	armed.Store(true)
	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-1", resolved)

	sess := restatedSession(t, env)
	require.False(t, armed.Load(), "the second refresh ran inside the first's verification")
	require.NoError(t, nestedErr)
	require.Equal(t, "access-2", loadString(&nestedToken))
	require.EqualValues(t, 2, refreshes.Load())
	require.Equal(t, "grant-owner@example.com", sess.UpstreamEmail.String, "the rotated row keeps the identity verified at the exchange")
	require.Equal(t, "user-123", sess.UpstreamSubject.String)
	require.JSONEq(t, `"grant-owner@example.com"`, string(decodeEnrichment(t, sess.Enrichment).IDToken["email"]))
	require.NotContains(t, string(sess.Enrichment), "refreshed@example.com")
}

func TestIDTokenSigningAlgorithmPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// advertised is the issuer's id_token_signing_alg_values_supported.
		advertised []string
		// rs256 signs with the RSA key sharing the ES256 key's kid.
		rs256 bool
		// captured reports whether the exchange stores an identity.
		captured bool
	}{
		{name: "shared kid picks the ES256 key by algorithm", advertised: nil, rs256: false, captured: true},
		{name: "shared kid picks the RSA key by algorithm", advertised: nil, rs256: true, captured: true},
		{name: "issuer advertising only ES256 rejects RS256", advertised: []string{"ES256"}, rs256: true, captured: false},
		{name: "issuer advertising RS256 accepts RS256", advertised: []string{"RS256"}, rs256: true, captured: true},
		{name: "issuer advertising nothing acceptable rejects everything", advertised: []string{"HS256"}, rs256: false, captured: false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			issuer := newSharedKidIDTokenIssuer(t)
			mint := issuer.mint
			if tc.rs256 {
				mint = issuer.mintRS256
			}
			suffix := fmt.Sprintf("idtoken-alg-%d", i)
			clientID := "synthetic-cid-" + suffix
			var nonce atomic.Pointer[string]
			_, env := newSyntheticExpiryEnv(t, suffix, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"id_token":"` +
					mint(t, issuer.claims(clientID, loadString(&nonce))) + `"}`))
			}, withIDTokenIssuer(issuer), observeNonce(&nonce), withIDTokenSigningAlgs(tc.advertised...))

			if !tc.captured {
				require.False(t, env.session.UpstreamSubject.Valid, "a token outside the issuer's algorithm policy leaves no identity behind")
				require.False(t, env.session.IdentitySource.Valid)
				return
			}
			require.Equal(t, "user-123", env.session.UpstreamSubject.String)
			require.Equal(t, "grant-owner@example.com", env.session.UpstreamEmail.String)
			require.Equal(t, remotesessions.IdentitySourceIDToken, env.session.IdentitySource.String)
		})
	}
}
