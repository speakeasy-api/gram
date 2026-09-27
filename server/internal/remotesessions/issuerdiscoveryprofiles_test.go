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
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssuerStrictMetadataArrays(t *testing.T) {
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

func TestIssuerDiscoveryEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		profiles, grants []string
	}{
		{"none", nil, nil}, {"jwt_only", nil, []string{oauthwire.GrantTypeJWTBearer}},
		{"profile_only", []string{oauthwire.GrantProfileIDJAG}, nil}, {"both", []string{oauthwire.GrantProfileIDJAG}, []string{oauthwire.GrantTypeJWTBearer}},
		{"unrelated", []string{"other"}, []string{oauthwire.GrantTypeJWTBearer}}, {"removed", []string{}, []string{}},
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

// Discovery and token validation both require an exact issuer match.
func TestIssuerDiscoveryExactTrailingSlashIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, requestedSuffix, servedSuffix string }{
		{"added slash", "", "/"},
		{"removed slash", "/", ""},
		{"exact root slash", "/", "/"},
		{"exact path slash", "/tenant/", "/tenant/"},
		{"exact repeated slash", "/tenant//", "/tenant//"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
					"issuer":                                 server.URL + tc.servedSuffix,
					"authorization_endpoint":                 server.URL + "/authorize",
					"token_endpoint":                         server.URL + "/token",
					"authorization_grant_profiles_supported": []string{oauthwire.GrantProfileIDJAG},
				}))
			}))
			defer server.Close()
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			doc, err := DiscoverIssuerMetadata(t.Context(), policy, server.URL+tc.requestedSuffix)
			if tc.requestedSuffix == tc.servedSuffix {
				require.NoError(t, err)
				require.Equal(t, server.URL+tc.servedSuffix, doc.Issuer)
				return
			}
			var untrusted *untrustedDocumentError
			require.ErrorAs(t, err, &untrusted)
			require.Equal(t, DiscoveredIssuerMetadata{}, doc)
		})
	}
}

func TestIssuerDiscoveryRejectsUnsafeEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, contentType, suffix, raw string }{
		{"sibling_issuer", "application/json", "/sibling", ""},
		{"html", "text/html", "", "<html>error</html>"},
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

func TestIssuerProfilesNeedReprojection(t *testing.T) {
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
		{"null", `{"authorization_grant_profiles_supported":null}`, []string{}, false},
		{"null withdraws profiles", `{"authorization_grant_profiles_supported":null}`, []string{"id-jag"}, true},
		{"malformed", `{"authorization_grant_profiles_supported":true}`, []string{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, issuerProfilesNeedReprojection(repo.RemoteSessionIssuer{Metadata: []byte(tc.metadata), AuthorizationGrantProfilesSupported: tc.stored}))
		})
	}
}

func TestIssuerDiscoverySkipsUntrustedCandidates(t *testing.T) {
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

// Public discovery reports partial evidence, but refresh must not persist it.
// Diagnostics must retain the peer's status, never its response body.
func TestIssuerDiscoveryPartialSuccessPreservesStatusAndFreshEvidence(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		for _, failedFamily := range []string{"oauth-authorization-server", "openid-configuration"} {
			t.Run(fmt.Sprintf("%d/%s", status, failedFamily), func(t *testing.T) {
				t.Parallel()
				var server *httptest.Server
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, failedFamily) {
						http.Error(w, "private upstream response body", status)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
						"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
						"token_endpoint": server.URL + "/token", "scopes_supported": []string{"fresh"},
					}))
				}))
				defer server.Close()
				policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
				require.NoError(t, err)
				discovered, err := DiscoverIssuerMetadata(t.Context(), policy, server.URL)
				require.NoError(t, err)
				expectedURL := server.URL + "/.well-known/" + failedFamily
				require.Equal(t, expectedURL, discovered.UnreadableURL)
				require.Contains(t, discovered.UnreadableMessage, fmt.Sprintf("Unexpected HTTP %d from %s", status, expectedURL))
				require.NotContains(t, discovered.UnreadableMessage, "private upstream")
				stored := repo.RemoteSessionIssuer{Issuer: server.URL,
					AuthorizationGrantProfilesSupported: []string{"stale-profile"},
					Metadata:                            []byte(fmt.Sprintf(`{"issuer":%q,"authorization_grant_profiles_supported":["stale-profile"],"grant_types_supported":["stale-grant"],"registration_endpoint":"https://stale.example/register"}`, server.URL)),
				}
				params, warnings, err := refreshIssuerMetadata(t.Context(), policy, nil, nil, stored)
				require.Error(t, err)
				message, transient := discoveryFailureMessage(err)
				require.True(t, transient)
				require.Equal(t, fmt.Sprintf("Unexpected HTTP %d from %s", status, expectedURL), message)
				require.NotContains(t, message, "private upstream")
				require.Equal(t, expectedURL, discoveryRetryURL(err))
				require.Zero(t, params, "partial discovery must not overwrite stored evidence")
				require.Empty(t, warnings)
			})
		}
	}
}

func TestIssuerDiscoveryAcceptsJSONRegardlessOfContentType(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"", "text/plain", "binary/octet-stream", "text/html", "invalid;"} {
		t.Run(contentType, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = []string{contentType}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
					"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token",
				}))
			}))
			defer server.Close()
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			doc, err := DiscoverIssuerMetadata(t.Context(), policy, server.URL)
			require.NoError(t, err)
			require.Equal(t, server.URL, doc.Issuer)
		})
	}
}
