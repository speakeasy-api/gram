package remotesessions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
		"workspaces":[{"id":"W1","bot_token":"xoxb-secret"}],
		"incoming_webhook":{"channel":"#general","url":"https://hooks.example.com/services/T1/B1/secret"},
		"bot_token":"secret","UserToken":"secret","client_secret":"secret","device_code":"secret","client_assertion":"secret",
		"password":"secret","api_key":"secret","private_key":"secret","jwt":"secret","credentials":{"x":1},
		"big":9007199254740993
	}`)
	extras := (tokenResponse{raw: raw}).extras()
	require.Len(t, extras, 5)
	require.Equal(t, `9007199254740993`, string(extras["big"]), "large integers survive untouched")
	require.JSONEq(t, `true`, string(extras["ok"]))
	require.JSONEq(t, `{"id":"T1"}`, string(extras["team"]))
	require.JSONEq(t, `{"id":"U1","scope":"chat:write"}`, string(extras["authed_user"]), "nested credentials are stripped too, and the rule is by name so token_type goes with them")
	require.JSONEq(t, `[{"id":"W1"}]`, string(extras["workspaces"]))

	require.Nil(t, (tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}).extras(), "standard members only")
	require.Nil(t, (tokenResponse{raw: nil}).extras())
	require.Nil(t, (tokenResponse{raw: []byte(`[1,2]`)}).extras(), "a non-object body yields nothing")
}

func TestBuildEnrichment(t *testing.T) {
	t.Parallel()

	plain := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}
	withExtras := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer","ok":true}`)}
	identity := &UpstreamIdentity{
		Subject: "user-1",
		Source:  IdentitySourceIDToken,
		Claims:  rawClaims(t, `{"sub":"user-1","email":"grant-owner@example.com","api_key":"secret","at_hash":"h","address":{"locality":"Berlin","access_token":"nested"},"big":9007199254740993}`),
	}

	raw, err := buildEnrichment(plain, nil)
	require.NoError(t, err)
	require.Nil(t, raw, "nothing to keep yields no document")

	raw, err = buildEnrichment(withExtras, nil)
	require.NoError(t, err)
	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Nil(t, doc.IDToken)
	require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]))

	raw, err = buildEnrichment(plain, identity)
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
	other.Source = "userinfo"
	raw, err = buildEnrichment(plain, &other)
	require.NoError(t, err)
	require.Nil(t, raw, "only ID token claims are kept under id_token")
}

func TestBuildEnrichmentDropsEmailVerifiedWithoutEmail(t *testing.T) {
	t.Parallel()

	plain := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer"}`)}
	identity := &UpstreamIdentity{
		Subject: "user-1",
		Source:  IdentitySourceIDToken,
		Claims:  rawClaims(t, `{"sub":"user-1","email_verified":true,"name":"Ada"}`),
	}

	raw, err := buildEnrichment(plain, identity)
	require.NoError(t, err)
	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotContains(t, doc.IDToken, "email_verified")
	require.JSONEq(t, `"Ada"`, string(doc.IDToken["name"]))

	identity.Claims = rawClaims(t, `{"email_verified":true}`)
	raw, err = buildEnrichment(plain, identity)
	require.NoError(t, err)
	require.Nil(t, raw, "a flag with nothing to describe leaves no document")

	for _, email := range []string{`""`, `null`, `42`} {
		identity.Claims = rawClaims(t, `{"sub":"user-1","email":`+email+`,"email_verified":true}`)
		raw, err = buildEnrichment(plain, identity)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &doc))
		require.NotContains(t, doc.IDToken, "email_verified", "email %s", email)
	}

	identity.Claims = rawClaims(t, `{"sub":"user-1","email":"ada@example.com","email_verified":true}`)
	raw, err = buildEnrichment(plain, identity)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.JSONEq(t, `true`, string(doc.IDToken["email_verified"]))
}

func TestTokenResponseUnchanged(t *testing.T) {
	t.Parallel()

	stored := []byte(`{"id_token":{"sub":"u"},"token_response":{"ok":true,"team":{"id":"T1","n":1}}}`)
	extras := func(doc string) map[string]json.RawMessage {
		return (tokenResponse{raw: []byte(doc)}).extras()
	}

	require.True(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team": {"n": 1, "id": "T1"}, "ok": true}`)), "order and whitespace are not changes")
	require.True(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","ok":true}`)), "omitted members keep their stored value")
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team":{"id":"T2","n":1}}`)))
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","ok":true,"extra":1}`)))
	require.False(t, tokenResponseUnchanged(stored, extras(`{"access_token":"a","team":{"id":"T1","n":1.0}}`)), "numbers compare by their text")
	require.True(t, tokenResponseUnchanged(nil, nil))
	require.False(t, tokenResponseUnchanged(nil, extras(`{"access_token":"a","ok":true}`)))
	require.False(t, tokenResponseUnchanged([]byte(`not json`), nil))
}

func TestBuildEnrichmentRejectsOversizedDocument(t *testing.T) {
	t.Parallel()

	oversized := tokenResponse{raw: []byte(`{"access_token":"a","token_type":"Bearer","blob":"` + strings.Repeat("x", maxEnrichmentBytes) + `"}`)}
	raw, err := buildEnrichment(oversized, nil)
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
