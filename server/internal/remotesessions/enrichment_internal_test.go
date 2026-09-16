package remotesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func TestOverwritableIdentitySources(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{IdentitySourceIDToken, IdentitySourceIntrospection, IdentitySourceJWTAccessToken, IdentitySourceUserinfo}, overwritableIdentitySources(IdentitySourceIDToken))
	require.Equal(t, []string{IdentitySourceIntrospection, IdentitySourceJWTAccessToken, IdentitySourceUserinfo}, overwritableIdentitySources(IdentitySourceUserinfo))
	require.Equal(t, []string{IdentitySourceIntrospection, IdentitySourceJWTAccessToken}, overwritableIdentitySources(IdentitySourceIntrospection))
	require.Equal(t, []string{IdentitySourceJWTAccessToken}, overwritableIdentitySources(IdentitySourceJWTAccessToken))
	require.Empty(t, overwritableIdentitySources(""), "no identity never moves the typed columns")
}

func TestRemoteSessionAccountLabel(t *testing.T) {
	t.Parallel()

	require.Empty(t, RemoteSessionAccountLabel("", "", "", "", nil).String())
	require.Empty(t, RemoteSessionAccountLabel("", "Friendly Name", "opaque-subject", IdentitySourceJWTAccessToken, nil).String())
	require.Equal(t, "Friendly Name · owner@example.com", RemoteSessionAccountLabel("", "Friendly Name", "owner@example.com", IdentitySourceJWTAccessToken, nil).String())
	require.Equal(t, "Grant Owner", RemoteSessionAccountLabel("", "Grant Owner", "", "", nil).String())
	require.Equal(t, "Grant Owner · owner@example.com", RemoteSessionAccountLabel("owner@example.com", "Grant Owner", "", "", []byte(`not json`)).String(), "a name and an email are both shown, name first")

	notion := []byte(`{"token_response":{"workspace_name":"Acme Docs","workspace_id":"w1"}}`)
	require.Equal(t, AccountLabel{Name: "owner@example.com", Email: "", Chips: []string{"Acme Docs"}, Caveat: ""}, RemoteSessionAccountLabel("owner@example.com", "", "", "", notion))
	require.Equal(t, "owner@example.com", RemoteSessionAccountLabel("owner@example.com", "", "", "", notion).Identity(), "the identity line carries no chips")
	require.Equal(t, []string{"Acme Docs"}, RemoteSessionAccountLabel("owner@example.com", "", "", "", notion).Context())

	owner := []byte(`{"token_response":{"workspace_name":"Acme Docs","owner":{"type":"user","user":{"object":"user","id":"u1","name":"Grant Owner","type":"person","person":{"email":"owner@example.com"}}}}}`)
	require.Equal(t, AccountLabel{Name: "Grant Owner", Email: "owner@example.com", Chips: []string{"Acme Docs"}, Caveat: ""}, RemoteSessionAccountLabel("", "", "", "", owner), "a token-response owner names the account when no identity interface did")
	require.Equal(t, AccountLabel{Name: "Verified Name", Email: "", Chips: []string{"Acme Docs"}, Caveat: ""}, RemoteSessionAccountLabel("", "Verified Name", "", IdentitySourceUserinfo, owner), "an identity interface outranks the token-response owner")
	require.Equal(t, AccountLabel{Name: "", Email: "", Chips: []string{"Acme Docs"}, Caveat: ""}, RemoteSessionAccountLabel("", "", "", "", []byte(`{"token_response":{"workspace_name":"Acme Docs","owner":{"type":"workspace","workspace":true}}}`)), "a workspace owner carries no user")

	rejected := []byte(`{"interfaces":{"id_token":{"status":"rejected","reason":"rejected at exchange"},"userinfo":{"status":"ok"}},"userinfo":{"sub":"u1","email":"owner@example.com","given_name":"Grant","family_name":"Owner"},"token_response":{"posthog_region":"us"}}`)
	require.Equal(t, AccountLabel{Name: "Grant Owner", Email: "owner@example.com", Chips: nil, Caveat: idTokenRejectedCaveat}, RemoteSessionAccountLabel("", "", "", "", rejected), "userinfo under a rejected id token is shown with a caveat")
	require.Equal(t, AccountLabel{Name: "Recorded Name", Email: "", Chips: nil, Caveat: ""}, RemoteSessionAccountLabel("", "Recorded Name", "", IdentitySourceIntrospection, rejected), "a recorded identity needs no caveat")
	require.Equal(t, AccountLabel{Name: "", Email: "", Chips: nil, Caveat: ""}, RemoteSessionAccountLabel("", "", "", "", []byte(`{"interfaces":{"id_token":{"status":"rejected"}},"userinfo":{"sub":"u1"}}`)), "a subject alone names nobody")
	require.Equal(t, AccountLabel{Name: "", Email: "", Chips: nil, Caveat: ""}, RemoteSessionAccountLabel("", "", "", "", []byte(`{"interfaces":{"id_token":{"status":"ok"}},"userinfo":{"email":"owner@example.com"}}`)), "stored userinfo is not shown unless the id token was rejected")

	contradicted := []byte(`{"interfaces":{"userinfo":{"status":"failed","reason":"subject mismatch"}},"id_token":{"sub":"u1","email":"owner@example.com"}}`)
	require.Equal(t, AccountLabel{Name: "owner@example.com", Email: "", Chips: nil, Caveat: subjectMismatchCaveat}, RemoteSessionAccountLabel("owner@example.com", "", "u1", IdentitySourceIDToken, contradicted), "a recorded identity a later answer contradicted is shown out of date")
	require.Equal(t, AccountLabel{Name: "owner@example.com", Email: "", Chips: nil, Caveat: ""}, RemoteSessionAccountLabel("owner@example.com", "", "u1", IdentitySourceIDToken, []byte(`{"interfaces":{"userinfo":{"status":"failed","reason":"no sub"}}}`)), "other failures are not a contradiction")
	require.Equal(t, AccountLabel{Name: "owner@example.com", Email: "", Chips: nil, Caveat: ""}, RemoteSessionAccountLabel("owner@example.com", "", "u1", IdentitySourceUserinfo, []byte(`{"interfaces":{"jwt_access_token":{"status":"failed","reason":"subject mismatch"}}}`)), "a rejected access token for another subject does not contradict the verified account")

	slack := []byte(`{"token_response":{"team":{"id":"T1","name":"Acme"}}}`)
	require.Equal(t, "Acme", RemoteSessionAccountLabel("", "", "", "", slack).String(), "a chip alone still names the session")
	require.Empty(t, RemoteSessionAccountLabel("", "", "", "", slack).Identity())

	github := []byte(`{"token_response":{"login":"octocat"}}`)
	require.Equal(t, "Octo Cat · octocat", RemoteSessionAccountLabel("", "Octo Cat", "", "", github).String())
	require.Equal(t, "octocat", RemoteSessionAccountLabel("", "octocat", "", "", github).String(), "a login equal to the rendered name is not repeated")
	require.Equal(t, "octocat · octo@example.com", RemoteSessionAccountLabel("octo@example.com", "octocat", "", "", github).String(), "a login equal to the shown name is not repeated")
	require.Equal(t, "Octo Cat · octo@example.com · octocat", RemoteSessionAccountLabel("octo@example.com", "Octo Cat", "", "", github).String())
	require.Equal(t, "Octo Cat · Acme Docs", RemoteSessionAccountLabel("", "Octo\r\nCat", "", "", []byte(`{"token_response":{"workspace_name":"Acme\nDocs"}}`)).String(), "provider text is one line")
}

func TestRemoteSessionAccountLabelJWTAccessTokenDisclosure(t *testing.T) {
	t.Parallel()

	enrichment := []byte(`{"token_response":{"workspace_name":"Example workspace"}}`)
	contextOnly := AccountLabel{Name: "", Email: "", Chips: []string{"Example workspace"}, Caveat: ""}
	require.Equal(t, contextOnly, RemoteSessionAccountLabel("", "Signed Name", "opaque-subject", IdentitySourceJWTAccessToken, enrichment),
		"hide the identity, not independently supplied provider context")
	require.Equal(t, contextOnly, RemoteSessionAccountLabel("", "Signed Name", "Owner <owner@example.com>", IdentitySourceJWTAccessToken, enrichment),
		"a display address is not an email-shaped subject")
	require.Equal(t, AccountLabel{Name: "Signed Name", Email: "owner@example.com", Chips: []string{"Example workspace"}, Caveat: ""},
		RemoteSessionAccountLabel("owner@example.com", "Signed Name", "opaque-subject", IdentitySourceJWTAccessToken, enrichment))
	require.Equal(t, AccountLabel{Name: "Signed Name", Email: "owner@example.com", Chips: []string{"Example workspace"}, Caveat: ""},
		RemoteSessionAccountLabel("", "Signed Name", "owner@example.com", IdentitySourceJWTAccessToken, enrichment))
	require.Equal(t, "Signed Name · Example workspace",
		RemoteSessionAccountLabel("", "Signed Name", "opaque-subject", IdentitySourceUserinfo, enrichment).String(),
		"the label policy is limited to JWT access-token identities")
}

func TestBuildEnrichmentKeepsUserinfoUnderItsOwnKey(t *testing.T) {
	t.Parallel()

	tok := tokenResponse{AccessToken: "a", RefreshToken: "", TokenType: "Bearer", ExpiresIn: 0, RefreshTokenTimeout: nil, AuthorizationExpiresIn: nil, RefreshExpiresIn: 0, RefreshTokenExpiresIn: 0, Scope: "", IDToken: "", raw: []byte(`{"access_token":"a"}`)}
	identity := &UpstreamIdentity{
		Subject: "user-123", Email: "", DisplayName: "Grant Owner", Source: IdentitySourceUserinfo,
		Claims: map[string]json.RawMessage{"sub": json.RawMessage(`"user-123"`), "email_verified": json.RawMessage(`true`), "access_token": json.RawMessage(`"leak"`)},
	}
	raw, err := buildEnrichment(tok, identity, map[string]interfaceRecord{IdentitySourceUserinfo: {Status: "ok"}}, nil)
	require.NoError(t, err)

	var doc enrichmentDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Nil(t, doc.IDToken)
	require.JSONEq(t, `"user-123"`, string(doc.Userinfo["sub"]))
	require.NotContains(t, doc.Userinfo, "email_verified", "email_verified without an email is dropped")
	require.NotContains(t, doc.Userinfo, "access_token")
	require.Equal(t, "ok", doc.Interfaces[IdentitySourceUserinfo].Status)

	raw, err = buildEnrichment(tok, nil, map[string]interfaceRecord{IdentitySourceUserinfo: {Status: "failed", Reason: "no sub"}}, nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, "failed", doc.Interfaces[IdentitySourceUserinfo].Status, "a failed interface is recorded even with nothing else to keep")
}

func TestIntrospectionAuthMethod(t *testing.T) {
	t.Parallel()

	method, err := introspectionAuthMethod([]string{"client_secret_post", "client_secret_basic"}, "client_secret_basic", "s3cret")
	require.NoError(t, err)
	require.Equal(t, TokenEndpointAuthMethodPost, method, "the first advertised method the credentials satisfy wins")

	method, err = introspectionAuthMethod([]string{"none", "client_secret_basic"}, "client_secret_basic", "s3cret")
	require.NoError(t, err)
	require.Equal(t, TokenEndpointAuthMethodBasic, method, "none is skipped when a secret is held")

	method, err = introspectionAuthMethod([]string{"client_secret_basic", "none"}, "none", "")
	require.NoError(t, err)
	require.Equal(t, TokenEndpointAuthMethodNone, method, "secret-bearing methods are skipped without a secret")

	method, err = introspectionAuthMethod([]string{"private_key_jwt"}, "client_secret_post", "s3cret")
	require.NoError(t, err)
	require.Equal(t, TokenEndpointAuthMethodPost, method, "an unsatisfiable advertisement falls back to the token endpoint's method")

	method, err = introspectionAuthMethod(nil, "client_secret_basic", "s3cret")
	require.NoError(t, err)
	require.Equal(t, TokenEndpointAuthMethodBasic, method, "no metadata falls back to the registered method")

	_, err = introspectionAuthMethod(nil, "client_secret_basic", "")
	require.Error(t, err, "the fallback keeps the token endpoint's validation")

	_, err = introspectionAuthMethod(nil, "private_key_jwt", "")
	require.ErrorContains(t, err, "private_key_jwt introspection authentication is not supported")
}

func TestIntrospectedTokenFromEnrichment(t *testing.T) {
	t.Parallel()

	_, ok := IntrospectedTokenFromEnrichment(nil)
	require.False(t, ok)
	_, ok = IntrospectedTokenFromEnrichment([]byte(`{"userinfo":{"sub":"u"}}`))
	require.False(t, ok, "no introspection answer stored")
	_, ok = IntrospectedTokenFromEnrichment([]byte(`{"introspection":{"scope":"read"}}`))
	require.False(t, ok, "an answer without active is no answer")

	token, ok := IntrospectedTokenFromEnrichment([]byte(`{"introspection":{"active":true,"exp":1789668505,"scope":"read"}}`))
	require.True(t, ok)
	require.True(t, token.Active)
	require.Equal(t, time.Unix(1789668505, 0), token.ExpiresAt)

	token, ok = IntrospectedTokenFromEnrichment([]byte(`{"introspection":{"active":false}}`))
	require.True(t, ok)
	require.False(t, token.Active)
	require.True(t, token.ExpiresAt.IsZero())

	_, ok = IntrospectedTokenFromEnrichment([]byte(`{"introspection":{"active":null,"exp":1789668505}}`))
	require.False(t, ok, "a null active is no verdict, not an inactive one")
	_, ok = IntrospectedTokenFromEnrichment([]byte(`{"introspection":{"active":"true"}}`))
	require.False(t, ok, "only a JSON boolean is a verdict")
}

// A bound issuer's userinfo endpoint is served from inside the customer
// network, so the call must take the tunnel. With no live route published the
// forward fails closed; what matters is that the advertised endpoint — which
// direct egress can still reach here — was never dialed.
func TestEnrichmentUserinfoTakesTheIssuerTunnel(t *testing.T) {
	t.Parallel()

	var direct atomic.Int64
	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		direct.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"owner"}`))
	}))
	defer userinfo.Close()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	enricher := NewSessionEnricher(testenv.NewLogger(t), nil, policy, nil, nil,
		tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", policy, nil), nil)

	target := enrichmentTarget{
		issuerID:            uuid.New(),
		issuerURL:           "https://idp.private.test",
		organizationID:      "org-1",
		userinfoEndpoint:    userinfo.URL,
		tunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
	result := enricher.userinfo(t.Context(), target, "access-token")
	require.True(t, result.ran)
	require.Nil(t, result.identity)
	require.Zero(t, direct.Load(), "a bound issuer's userinfo must not be dialed over direct egress")

	target.tunneledMcpServerID = uuid.NullUUID{}
	unbound := enricher.userinfo(t.Context(), target, "access-token")
	require.True(t, unbound.ran)
	require.NotNil(t, unbound.identity)
	require.Equal(t, "owner", unbound.identity.Subject)
	require.Equal(t, int64(1), direct.Load())
}
