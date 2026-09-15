package remotesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestPreparationEligibility(t *testing.T) {
	for _, tt := range []struct {
		name             string
		profiles, grants []string
		want             string
	}{
		{"none", nil, nil, "unsupported_profile"},
		{"jwt only", nil, []string{PreparationJWTBearerGrant}, "unsupported_profile"},
		{"profile only", []string{PreparationIDJAGProfile}, nil, "incomplete_metadata"},
		{"both", []string{PreparationIDJAGProfile}, []string{PreparationJWTBearerGrant}, "eligible"},
		{"unrelated", []string{"urn:example:unrelated"}, []string{PreparationJWTBearerGrant}, "unsupported_profile"},
		{"removed", []string{}, []string{}, "unsupported_profile"},
		{"not substring", []string{PreparationIDJAGProfile + "x"}, []string{PreparationJWTBearerGrant}, "unsupported_profile"},
	} {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, PreparationEligibility(tt.profiles, tt.grants)) })
	}
}
func TestPreparationCanonicalResourceAndScopes(t *testing.T) {
	in := PreparationInput{UserSessionIssuerID: uuid.New(), RemoteSessionIssuerID: uuid.New(), Resource: "https://resource.example.com/mcp/", Scopes: []string{"write", "read", "read"}}
	normalized, err := normalizePreparationInput(in)
	require.NoError(t, err)
	require.Equal(t, in.Resource, normalized.Resource)
	require.Equal(t, []string{"read", "write"}, normalized.Scopes)
	for _, resource := range []string{"http://resource.example.com/mcp", "https://name:password@resource.example.com/mcp", "https://resource.example.com/mcp#fragment", "https://resource.example.com/mcp?query=1", "relative"} {
		in.Resource = resource
		_, err := normalizePreparationInput(in)
		require.Error(t, err)
	}
}
func TestPreparationDCRValidation(t *testing.T) {
	base := preparationDCRResponse{ClientID: "resource-client", ClientSecret: "test-secret", TokenEndpointAuthMethod: "client_secret_basic", GrantTypes: []string{PreparationJWTBearerGrant}}
	for _, tt := range []struct {
		name, want string
		mutate     func(*preparationDCRResponse)
	}{
		{"confirmed", "ready", func(*preparationDCRResponse) {}},
		{"expired secret", "manual_setup_required", func(r *preparationDCRResponse) { r.ClientSecretExpiresAt = time.Now().Add(-time.Minute).Unix() }},
		{"future expiry", "ready", func(r *preparationDCRResponse) { r.ClientSecretExpiresAt = time.Now().Add(time.Hour).Unix() }},
		{"omitted grants", "unknown_grants", func(r *preparationDCRResponse) { r.GrantTypes = nil }},
		{"empty grants", "unknown_grants", func(r *preparationDCRResponse) { r.GrantTypes = []string{} }},
		{"auth downgrade", "indeterminate", func(r *preparationDCRResponse) { r.TokenEndpointAuthMethod = "none" }},
		{"auth omission", "indeterminate", func(r *preparationDCRResponse) { r.TokenEndpointAuthMethod = "" }},
		{"missing secret", "indeterminate", func(r *preparationDCRResponse) { r.ClientSecret = "" }},
		{"narrowed scope", "ready", func(r *preparationDCRResponse) { v := "read"; r.Scope = &v }},
		{"broadened scope", "indeterminate", func(r *preparationDCRResponse) { v := "admin"; r.Scope = &v }},
		{"missing client", "indeterminate", func(r *preparationDCRResponse) { r.ClientID = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			tt.mutate(&r)
			require.Equal(t, tt.want, validatePreparationDCR(r, []string{"read", "write"}, "client_secret_basic"))
		})
	}
}
func TestPreparationDCRWireAndUncertainty(t *testing.T) {
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	s := &Service{policy: policy}
	for _, tt := range []struct {
		name                string
		status              int
		body, content, want string
	}{
		{"confirmed", 201, `{"client_id":"resource-client","client_secret":"test-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`, "application/json", "ready"},
		{"malformed grants", 201, `{"grant_types":"bad"}`, "application/json", "indeterminate"},
		{"HTML", 201, `<html>ok</html>`, "text/html", "indeterminate"},
		{"rejected", 400, `{"error":"invalid_client_metadata"}`, "application/json", "provider_rejection"},
		{"timeout", 408, ``, "application/json", "indeterminate"},
		{"upstream failure", 503, ``, "application/json", "indeterminate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.JSONEq(t, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, string(request["grant_types"]))
				require.NotContains(t, request, "client_secret")
				require.NotContains(t, request, "redirect_uris")
				w.Header().Set("Content-Type", tt.content)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, state := s.submitPreparationDCR(t.Context(), PreparationInput{Scopes: []string{"read"}}, server.URL, "client_secret_basic")
			require.Equal(t, tt.want, state)
		})
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	_, state := s.submitPreparationDCR(t.Context(), PreparationInput{}, source.URL, "client_secret_basic")
	require.Equal(t, "indeterminate", state)
	require.Zero(t, redirected.Load())
}

func TestPreparationGrantSetIdempotency(t *testing.T) {
	require.True(t, samePreparationGrants([]string{"b", "a", "a"}, []string{"a", "b"}))
	require.True(t, samePreparationGrants(nil, nil))
	require.False(t, samePreparationGrants(nil, []string{}))
	require.False(t, samePreparationGrants([]string{"a"}, []string{"b"}))
}
