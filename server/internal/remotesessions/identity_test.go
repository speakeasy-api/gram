package remotesessions

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func TestJWTVerificationRejectsSmallRSAKey(t *testing.T) {
	t.Parallel()
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	require.Error(t, validateJWTVerificationKeyStrength(&jose.JSONWebKey{Key: &weak.PublicKey}))
	strong, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	require.NoError(t, validateJWTVerificationKeyStrength(&jose.JSONWebKey{Key: &strong.PublicKey}))
}

func rawClaims(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()

	var claims map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(doc), &claims))
	return claims
}

func TestDisplayNameFromClaims(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"name wins over the parts", `{"name":"Ada Lovelace","given_name":"Ada","preferred_username":"ada"}`, "Ada Lovelace"},
		{"given and family name", `{"given_name":"Ada","family_name":"Lovelace"}`, "Ada Lovelace"},
		{"family name alone", `{"family_name":"Lovelace"}`, "Lovelace"},
		{"preferred username last", `{"preferred_username":"ada"}`, "ada"},
		{"non-string name is skipped", `{"name":42,"preferred_username":"ada"}`, "ada"},
		{"nothing usable", `{"sub":"1"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, displayNameFromClaims(rawClaims(t, tc.doc)))
		})
	}
}

func TestTokenResponseExtras(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600,"scope":"x","id_token":"i",
		"ok":true,"team":{"id":"T1"},
		"authed_user":{"id":"U1","scope":"chat:write","access_token":"xoxp-secret","token_type":"user"},
		"Workspace_Name":"Acme","hub_id":9007199254740993,
		"owner":{"user":{"id":"u1"},"api_key":"secret"},
		"workspaces":[{"id":"W1"}],
		"incoming_webhook":{"channel":"#general","url":"https://hooks.example.com/services/T1/B1/secret"},
		"value":"secret","refresh":"secret","url":"https://example.com/secret","bot_token":"secret"
	}`)
	extras := (tokenResponse{raw: raw}).extras()
	require.Len(t, extras, 5)
	require.JSONEq(t, `{"id":"T1"}`, string(extras["team"]))
	require.JSONEq(t, `{"id":"U1","scope":"chat:write"}`, string(extras["authed_user"]), "nested credentials are stripped too, and the rule is by name so token_type goes with them")
	require.JSONEq(t, `"Acme"`, string(extras["Workspace_Name"]), "the allowlist matches regardless of case")
	require.Equal(t, `9007199254740993`, string(extras["hub_id"]), "large integers survive untouched")
	require.JSONEq(t, `{"user":{"id":"u1"}}`, string(extras["owner"]))
	for _, name := range []string{"ok", "workspaces", "incoming_webhook", "value", "refresh", "url", "bot_token"} {
		require.NotContains(t, extras, name, "members off the allowlist are dropped whatever their name")
	}

	require.Nil(t, (tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}).extras(), "standard members only")
	require.Nil(t, (tokenResponse{raw: []byte(`{"access_token":"a","value":"x","ok":true}`)}).extras(), "unlisted members alone yield nothing")
	require.Nil(t, (tokenResponse{raw: nil}).extras())
	require.Nil(t, (tokenResponse{raw: []byte(`[1,2]`)}).extras(), "a non-object body yields nothing")
}

func TestAcceptedIDTokenAlgorithms(t *testing.T) {
	t.Parallel()

	all, err := acceptedIDTokenAlgorithms(nil)
	require.NoError(t, err)
	require.Equal(t, jwks.AllowedSignatureAlgorithms(), all, "an issuer advertising nothing gets the shared allowlist")

	narrowed, err := acceptedIDTokenAlgorithms([]string{"HS256", "ES256", "none", "RS256"})
	require.NoError(t, err)
	require.Equal(t, []jose.SignatureAlgorithm{jose.RS256, jose.ES256}, narrowed, "the intersection keeps the allowlist's order and drops HS* and none")

	_, err = acceptedIDTokenAlgorithms([]string{"HS256"})
	require.Error(t, err, "an empty intersection is a rejection, not a fallback")
}

func TestBuildEnrichment(t *testing.T) {
	t.Parallel()

	plain := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}
	withExtras := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer","app_id":"A1"}`)}
	identity := &UpstreamIdentity{
		Subject: "user-1",
		Source:  IdentitySourceIDToken,
		Claims:  rawClaims(t, `{"sub":"user-1","email":"grant-owner@example.com","api_key":"secret","at_hash":"h","address":{"locality":"Berlin","access_token":"nested"},"big":9007199254740993}`),
	}

	raw, err := buildEnrichment(plain, nil, nil, nil)
	require.NoError(t, err)
	require.Nil(t, raw, "nothing to keep yields no document")

	raw, err = buildEnrichment(withExtras, nil, nil, nil)
	require.NoError(t, err)
	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Nil(t, doc.IDToken)
	require.JSONEq(t, `"A1"`, string(doc.TokenResponse["app_id"]))

	raw, err = buildEnrichment(plain, identity, nil, nil)
	require.NoError(t, err)
	doc = enrichmentDocument{IDToken: nil, TokenResponse: nil}
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.NotContains(t, doc.IDToken, "api_key", "credential-shaped claims are dropped")
	require.JSONEq(t, `{"locality":"Berlin"}`, string(doc.IDToken["address"]), "at every nesting level")
	require.Equal(t, `9007199254740993`, string(doc.IDToken["big"]), "large integers survive untouched")
	require.Contains(t, doc.IDToken, "at_hash")
	require.Nil(t, doc.TokenResponse)

	other := *identity
	other.Source = IdentitySourceUserinfo
	raw, err = buildEnrichment(plain, &other, nil, nil)
	require.NoError(t, err)
	var fromUserinfo enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &fromUserinfo))
	require.Nil(t, fromUserinfo.IDToken, "only ID token claims are kept under id_token")
	require.JSONEq(t, `"user-1"`, string(fromUserinfo.Userinfo["sub"]), "userinfo claims live under their own key")
}

func TestBuildEnrichmentDropsEmailVerifiedWithoutEmail(t *testing.T) {
	t.Parallel()

	plain := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}
	identity := &UpstreamIdentity{
		Subject: "user-1",
		Source:  IdentitySourceIDToken,
		Claims:  rawClaims(t, `{"sub":"user-1","email_verified":true,"name":"Ada"}`),
	}

	raw, err := buildEnrichment(plain, identity, nil, nil)
	require.NoError(t, err)
	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotContains(t, doc.IDToken, "email_verified")
	require.JSONEq(t, `"Ada"`, string(doc.IDToken["name"]))

	identity.Claims = rawClaims(t, `{"email_verified":true}`)
	raw, err = buildEnrichment(plain, identity, nil, nil)
	require.NoError(t, err)
	require.Nil(t, raw, "a flag with nothing to describe leaves no document")

	for _, email := range []string{`""`, `null`, `42`} {
		identity.Claims = rawClaims(t, `{"sub":"user-1","email":`+email+`,"email_verified":true}`)
		raw, err = buildEnrichment(plain, identity, nil, nil)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &doc))
		require.NotContains(t, doc.IDToken, "email_verified", "email %s", email)
	}

	identity.Claims = rawClaims(t, `{"sub":"user-1","email":"ada@example.com","email_verified":true}`)
	raw, err = buildEnrichment(plain, identity, nil, nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.JSONEq(t, `true`, string(doc.IDToken["email_verified"]))

	jwtClaims := rawClaims(t, `{"sub":"user-1","email_verified":true}`)
	raw, err = buildEnrichment(plain, identity, nil, jwtClaims)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotContains(t, doc.JWTAccessToken, "email_verified", "fallback JWT claims get the same cleanup")
	require.JSONEq(t, `"user-1"`, string(doc.JWTAccessToken["sub"]))
}

func TestTokenResponseUnchanged(t *testing.T) {
	t.Parallel()

	stored := []byte(`{"id_token":{"sub":"u"},"token_response":{"app_id":"A1","team":{"id":"T1","n":1}}}`)
	extras := func(doc string) map[string]json.RawMessage {
		return (tokenResponse{raw: []byte(doc)}).extras()
	}

	require.True(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team": {"n": 1, "id": "T1"}, "app_id": "A1"}`)), "order and whitespace are not changes")
	require.True(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","app_id":"A1"}`)), "omitted members keep their stored value")
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team":{"id":"T2","n":1}}`)))
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","app_id":"A1","hub_id":1}`)))
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team":{"id":"T1","n":1.0}}`)), "numbers compare by their text")
	require.True(t, tokenResponseUnchanged(nil, nil))
	require.False(t, tokenResponseUnchanged(nil, extras(`{"access_token":"a","app_id":"A1"}`)))
	require.False(t, tokenResponseUnchanged([]byte(`not json`), nil))
}

func TestBuildEnrichmentRejectsOversizedDocument(t *testing.T) {
	t.Parallel()

	oversized := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer","hub_domain":"` + strings.Repeat("x", maxEnrichmentBytes) + `"}`)}
	raw, err := buildEnrichment(oversized, nil, nil, nil)
	require.ErrorIs(t, err, errEnrichmentTooLarge)
	require.Nil(t, raw)
}

func TestCredentialMemberName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"access_token", "refreshToken", "client_secret", "Authorization", "session_state", "cookie", "signature", "webhook_url", "api_key", "device_code", "jwt", "client_assertion", "password"} {
		require.True(t, credentialMemberName(name), name)
	}
	for _, name := range []string{"ok", "team", "scope", "workspace_name", "email", "locality", "bot_user_id"} {
		require.False(t, credentialMemberName(name), name)
	}
}
