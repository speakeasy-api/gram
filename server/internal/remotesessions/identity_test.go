package remotesessions

import (
	"encoding/json"
	"testing"
	"time"

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

func TestClaimBool(t *testing.T) {
	t.Parallel()

	claims := rawClaims(t, `{"t":true,"f":false,"st":"true","sf":"False","word":"yes","n":1}`)
	truth, falsehood := true, false
	require.Equal(t, &truth, claimBool(claims, "t"))
	require.Equal(t, &falsehood, claimBool(claims, "f"))
	require.Equal(t, &truth, claimBool(claims, "st"), "string spellings are accepted")
	require.Equal(t, &falsehood, claimBool(claims, "sf"))
	require.Nil(t, claimBool(claims, "word"))
	require.Nil(t, claimBool(claims, "n"))
	require.Nil(t, claimBool(claims, "absent"))
}

func TestClaimTime(t *testing.T) {
	t.Parallel()

	claims := rawClaims(t, `{"ok":1700000000,"fraction":1700000000.9,"zero":0,"text":"1700000000"}`)
	want := time.Unix(1700000000, 0).UTC()
	require.Equal(t, &want, claimTime(claims, "ok"))
	require.Equal(t, &want, claimTime(claims, "fraction"), "sub-second precision is dropped")
	require.Nil(t, claimTime(claims, "zero"))
	require.Nil(t, claimTime(claims, "text"))
	require.Nil(t, claimTime(claims, "absent"))
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
		"password":"secret","api_key":"secret","private_key":"secret","jwt":"secret","credentials":{"x":1}
	}`)
	extras := (tokenResponse{raw: raw}).extras()
	require.Len(t, extras, 4)
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
		Claims:  rawClaims(t, `{"sub":"user-1","email":"grant-owner@example.com","api_key":"secret","at_hash":"h"}`),
	}

	require.Nil(t, buildEnrichment(plain, nil), "nothing to keep yields no document")

	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(buildEnrichment(withExtras, nil), &doc))
	require.Nil(t, doc.IDToken)
	require.JSONEq(t, `true`, string(doc.TokenResponse["ok"]))

	doc = enrichmentDocument{IDToken: nil, TokenResponse: nil}
	require.NoError(t, json.Unmarshal(buildEnrichment(plain, identity), &doc))
	require.JSONEq(t, `"grant-owner@example.com"`, string(doc.IDToken["email"]))
	require.NotContains(t, doc.IDToken, "api_key", "credential-shaped claims are dropped")
	require.Contains(t, doc.IDToken, "at_hash")
	require.Nil(t, doc.TokenResponse)

	other := *identity
	other.Source = "userinfo"
	require.Nil(t, buildEnrichment(plain, &other), "only ID token claims are kept under id_token")
}
