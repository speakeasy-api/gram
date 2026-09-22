package remotesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssuerPartialRefreshUsesOnlyFreshEvidence(t *testing.T) {
	t.Parallel()
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"issuer": upstream.URL, "authorization_endpoint": upstream.URL + "/authorize", "token_endpoint": upstream.URL + "/new-token"}))
	}))
	t.Cleanup(upstream.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	for _, storedIssuer := range []string{upstream.URL, "https://other.example.com"} {
		t.Run(storedIssuer, func(t *testing.T) {
			t.Parallel()
			issuer := repo.RemoteSessionIssuer{Issuer: upstream.URL, Metadata: []byte(`{"issuer":"` + storedIssuer + `","token_endpoint":"https://old.example.com/token","claims_supported":["email"],"authorization_grant_profiles_supported":["urn:ietf:params:oauth:grant-profile:id-jag"]}`)}
			params, warnings, err := refreshIssuerMetadata(t.Context(), policy, nil, nil, issuer)
			require.NoError(t, err)
			require.Equal(t, upstream.URL+"/new-token", params.TokenEndpoint)
			require.NotEmpty(t, params.MetadataLastErrorUrl)
			require.NotEmpty(t, warnings)
			// Stored merged metadata cannot attribute omitted claims to the
			// unavailable candidate, even when its issuer matches.
			require.Empty(t, params.ClaimsSupported)
			require.Empty(t, params.AuthorizationGrantProfilesSupported)
		})
	}
}

func TestIssuerLegacyNullArraysRemainStrictOnNetwork(t *testing.T) {
	t.Parallel()
	requested, err := url.Parse("https://idp.example.com")
	require.NoError(t, err)
	raw := []byte(`{"issuer":"https://idp.example.com","authorization_endpoint":"https://idp.example.com/authorize","token_endpoint":"https://idp.example.com/token","authorization_grant_profiles_supported":null,"claims_supported":null}`)
	_, err = decodeIssuerDocument(raw, requested)
	require.Error(t, err)
	row := repo.RemoteSessionIssuer{Issuer: requested.String(), Metadata: raw}
	_, err = decodeStoredIssuerDocument(row)
	require.NoError(t, err)
	require.False(t, issuerProfilesNeedReprojection(row))
	row.AuthorizationGrantProfilesSupported = []string{"stale"}
	require.True(t, issuerProfilesNeedReprojection(row))
}
