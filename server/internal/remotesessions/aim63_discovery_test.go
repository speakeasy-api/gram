package remotesessions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAIM63StrictMetadataArrays(t *testing.T) {
	t.Parallel()
	requested, err := url.Parse("https://issuer.example")
	require.NoError(t, err)
	for _, member := range []string{"authorization_grant_profiles_supported", "grant_types_supported", "scopes_supported", "response_types_supported", "token_endpoint_auth_methods_supported", "code_challenge_methods_supported", "introspection_endpoint_auth_methods_supported", "id_token_signing_alg_values_supported", "claims_supported"} {
		for _, raw := range []string{`null`, `"jwt-bearer"`, `{}`, `[null]`, `[1]`, `[true]`, `["ok",null]`} {
			t.Run(member+raw, func(t *testing.T) {
				t.Parallel()
				_, err := decodeIssuerDocument([]byte(`{"`+member+`":`+raw+`}`), requested)
				require.Error(t, err)
			})
		}
	}
	for _, raw := range []string{`{}`, `{"authorization_grant_profiles_supported":[]}`, `{"authorization_grant_profiles_supported":["urn:ietf:params:oauth:grant-profile:id-jag"]}`} {
		_, err := decodeIssuerDocument([]byte(raw), requested)
		require.NoError(t, err)
	}
}

func TestAIM63DiscoveryEvidence(t *testing.T) {
	t.Parallel()
	const profile = "urn:ietf:params:oauth:grant-profile:id-jag"
	const grant = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	for _, tc := range []struct {
		name             string
		profiles, grants []string
	}{
		{"none", nil, nil}, {"jwt_only", nil, []string{grant}},
		{"profile_only", []string{profile}, nil}, {"both", []string{profile}, []string{grant}},
		{"unrelated", []string{"other"}, []string{grant}}, {"removed", []string{}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/tenant/.well-known/openid-configuration" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				doc := map[string]any{"issuer": server.URL + "/tenant", "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token"}
				if tc.profiles != nil {
					doc["authorization_grant_profiles_supported"] = tc.profiles
				}
				if tc.grants != nil {
					doc["grant_types_supported"] = tc.grants
				}
				assert.NoError(t, json.NewEncoder(w).Encode(doc))
			}))
			defer server.Close()
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			doc, err := DiscoverIssuerMetadata(t.Context(), policy, server.URL+"/tenant")
			require.NoError(t, err)
			require.Equal(t, orEmptySlice(tc.profiles), doc.AuthorizationGrantProfilesSupported)
			require.Equal(t, slices.Clone(tc.grants), doc.GrantTypesSupported)
		})
	}
}

func TestAIM63DiscoveryRejectsUnsafeEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, contentType, suffix, raw string }{
		{"trailing_slash", "application/json", "/", ""},
		{"sibling_issuer", "application/json", "/sibling", ""},
		{"html", "text/html", "", "<html>error</html>"},
		{"json_as_html", "text/html", "", ""},
		{"oversize", "application/json", "", `{"padding":"` + strings.Repeat("x", 1<<20) + `"}`},
		{"null_array", "application/json", "", `{"authorization_grant_profiles_supported":[null]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				if tc.raw != "" {
					_, _ = w.Write([]byte(tc.raw))
					return
				}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL + tc.suffix, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token"}))
			}))
			defer server.Close()
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			_, err = DiscoverIssuerMetadata(t.Context(), policy, server.URL)
			require.Error(t, err)
		})
	}
}

func TestAIM63ClientMetadataGrants(t *testing.T) {
	t.Parallel()
	const clientID = "https://gram.example/.well-known/oauth-client/client"
	const jwt = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	for _, tc := range []struct {
		name                        string
		grants, expected, responses []string
	}{
		{"legacy", nil, []string{"authorization_code", "refresh_token"}, []string{"code"}},
		{"empty", []string{}, []string{}, []string{}},
		{"jwt", []string{jwt}, []string{jwt}, []string{}},
		{"combined", []string{"authorization_code", "refresh_token", jwt}, []string{"authorization_code", "refresh_token", jwt}, []string{"code"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := BuildClientMetadataDocumentWithGrants(clientID, "https://gram.example/callback", TokenEndpointAuthMethodNone, "", nil, tc.grants)
			require.Equal(t, clientID, doc.ClientID)
			require.Equal(t, tc.expected, doc.GrantTypes)
			require.Equal(t, tc.responses, doc.ResponseTypes)
			encoded, err := json.Marshal(doc)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), `"grant_types":null`)
		})
	}
}

func TestAIM63ProfilesNeedReprojection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, metadata string
		stored         []string
		want           bool
	}{
		{"absent", `{}`, nil, false},
		{"empty", `{"authorization_grant_profiles_supported":[]}`, []string{}, false},
		{"capture", `{"authorization_grant_profiles_supported":["id-jag"]}`, []string{}, true},
		{"captured", `{"authorization_grant_profiles_supported":["id-jag"]}`, []string{"id-jag"}, false},
		{"withdrawn", `{}`, []string{"id-jag"}, true},
		{"null", `{"authorization_grant_profiles_supported":null}`, []string{}, true},
		{"malformed", `{"authorization_grant_profiles_supported":true}`, []string{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, issuerProfilesNeedReprojection(repo.RemoteSessionIssuer{Metadata: []byte(tc.metadata), AuthorizationGrantProfilesSupported: tc.stored}))
		})
	}
}

func TestAIM63DiscoverySkipsUntrustedCandidates(t *testing.T) {
	t.Parallel()
	for _, identity := range []string{"missing", "mismatched", "endpointless_mismatch"} {
		for _, usable := range []bool{false, true} {
			t.Run(identity+"/usable="+fmt.Sprint(usable), func(t *testing.T) {
				t.Parallel()
				var server *httptest.Server
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					doc := map[string]any{"issuer": "https://untrusted.example", "grant_types_supported": []string{"untrusted-grant"}, "authorization_endpoint": "https://untrusted.example/authorize", "token_endpoint": "https://untrusted.example/token"}
					if identity == "missing" {
						delete(doc, "issuer")
					}
					if identity == "endpointless_mismatch" {
						delete(doc, "authorization_endpoint")
						delete(doc, "token_endpoint")
					}
					if usable && r.URL.Path == "/.well-known/openid-configuration" {
						doc = map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token"}
					}
					assert.NoError(t, json.NewEncoder(w).Encode(doc))
				}))
				defer server.Close()
				policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
				require.NoError(t, err)
				result, err := DiscoverIssuerMetadata(t.Context(), policy, server.URL)
				if !usable {
					var untrusted *untrustedDocumentError
					require.ErrorAs(t, err, &untrusted)
					return
				}
				require.NoError(t, err)
				require.Equal(t, server.URL, result.Issuer)
				require.Equal(t, server.URL+"/token", result.TokenEndpoint)
				require.NotContains(t, result.GrantTypesSupported, "untrusted-grant", "mismatched candidates cannot contribute metadata")
			})
		}
	}
}
