package remotesessions

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparationEligibility(t *testing.T) {
	t.Parallel()
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, PreparationEligibility(tt.profiles, tt.grants))
		})
	}
}
func TestPreparationCanonicalResourceAndScopes(t *testing.T) {
	t.Parallel()
	in := PreparationInput{UserSessionIssuerID: uuid.New(), RemoteSessionIssuerID: uuid.New(), Resource: "https://resource.example.com/mcp/", Scopes: []string{"write", "read", "read"}}
	normalized, err := normalizePreparationInput(in)
	require.NoError(t, err)
	require.Equal(t, in.Resource, normalized.Resource)
	require.Equal(t, []string{"read", "write"}, normalized.Scopes)
	for _, scope := range []string{"", "read write", "read\twrite", "read\rwrite", "read\nwrite", "read\"write", "read\\write"} {
		invalid := in
		invalid.Scopes = []string{scope}
		_, err := normalizePreparationInput(invalid)
		require.Error(t, err, "scope %q", scope)
	}
	for _, resource := range []string{"http://resource.example.com/mcp", "https://name:password@resource.example.com/mcp", "https://resource.example.com/mcp#fragment", "https://resource.example.com/mcp?query=1", "relative", "https://resource.example.com/mcp?"} {
		in.Resource = resource
		_, err := normalizePreparationInput(in)
		require.Error(t, err)
	}
}
func TestPreparationNormalizesConfirmedGrantSets(t *testing.T) {
	t.Parallel()
	in := PreparationInput{UserSessionIssuerID: uuid.New(), RemoteSessionIssuerID: uuid.New(), Resource: "https://resource.example.com/", ConfirmGrants: []string{"refresh_token", "authorization_code", "refresh_token"}}
	normalized, err := normalizePreparationInput(in)
	require.NoError(t, err)
	require.Equal(t, []string{"authorization_code", "refresh_token"}, normalized.ConfirmGrants)
	require.Equal(t, []string{"refresh_token", "authorization_code", "refresh_token"}, in.ConfirmGrants)
	in.ConfirmGrants = nil
	normalized, err = normalizePreparationInput(in)
	require.NoError(t, err)
	require.Nil(t, normalized.ConfirmGrants)
	in.ConfirmGrants = []string{}
	normalized, err = normalizePreparationInput(in)
	require.NoError(t, err)
	require.NotNil(t, normalized.ConfirmGrants)
	require.Empty(t, normalized.ConfirmGrants)
}

func TestPreparationDCRValidation(t *testing.T) {
	t.Parallel()
	base := preparationDCRResponse{ClientID: "resource-client", ClientSecret: "test-secret", TokenEndpointAuthMethod: "client_secret_basic", GrantTypes: []string{PreparationJWTBearerGrant}}
	for _, tt := range []struct {
		name, want string
		mutate     func(*preparationDCRResponse)
	}{
		{"confirmed", "ready", func(*preparationDCRResponse) {}},
		{"expired secret", "manual_setup_required", func(r *preparationDCRResponse) { r.ClientSecretExpiresAt = time.Now().Add(-time.Minute).Unix() }},
		{"future expiry", "ready", func(r *preparationDCRResponse) { r.ClientSecretExpiresAt = time.Now().Add(time.Hour).Unix() }},
		{"omitted grants", "unknown_grants", func(r *preparationDCRResponse) { r.GrantTypes = nil }},
		{"empty grants", "manual_setup_required", func(r *preparationDCRResponse) { r.GrantTypes = []string{} }},
		{"auth downgrade", "indeterminate", func(r *preparationDCRResponse) { r.TokenEndpointAuthMethod = "none" }},
		{"auth omission", "indeterminate", func(r *preparationDCRResponse) { r.TokenEndpointAuthMethod = "" }},
		{"missing secret", "indeterminate", func(r *preparationDCRResponse) { r.ClientSecret = "" }},
		{"narrowed scope", "ready", func(r *preparationDCRResponse) { v := "read"; r.Scope = &v }},
		{"broadened scope", "indeterminate", func(r *preparationDCRResponse) { v := "admin"; r.Scope = &v }},
		{"missing client", "indeterminate", func(r *preparationDCRResponse) { r.ClientID = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := base
			tt.mutate(&r)
			require.Equal(t, tt.want, validatePreparationDCR(r, []string{"read", "write"}, "client_secret_basic"))
		})
	}
}
func TestPreparationDCRWireAndUncertainty(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.JSONEq(t, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, string(request["grant_types"]))
				assert.NotContains(t, request, "client_secret")
				assert.NotContains(t, request, "redirect_uris")
				w.Header().Set("Content-Type", tt.content)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)
			_, state := s.submitPreparationDCR(t.Context(), PreparationInput{Scopes: []string{"read"}}, server.URL, "client_secret_basic")
			require.Equal(t, tt.want, state)
		})
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)
	_, state := s.submitPreparationDCR(t.Context(), PreparationInput{}, source.URL, "client_secret_basic")
	require.Equal(t, "indeterminate", state)
	require.Zero(t, redirected.Load())
}

func TestPreparationGrantSetIdempotency(t *testing.T) {
	t.Parallel()
	require.True(t, samePreparationGrants([]string{"b", "a", "a"}, []string{"a", "b"}))
	require.True(t, samePreparationGrants(nil, nil))
	require.False(t, samePreparationGrants(nil, []string{}))
	require.False(t, samePreparationGrants([]string{"a"}, []string{"b"}))
}

func TestPreparationMetadataTransient(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, tc := range []struct {
		name            string
		fetched, failed time.Time
		retryURL        string
		want            bool
	}{
		{"current transient", now.Add(-time.Hour), now, "https://issuer.example.com/metadata", true},
		{"stale transient", now, now.Add(-time.Hour), "https://issuer.example.com/metadata", false},
		{"definitive", now.Add(-time.Hour), now, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, preparationMetadataTransient(repo.RemoteSessionIssuer{
				MetadataFetchedAt:    pgtype.Timestamptz{Time: tc.fetched, Valid: true},
				MetadataLastErrorAt:  pgtype.Timestamptz{Time: tc.failed, Valid: true},
				MetadataLastErrorUrl: pgtype.Text{String: tc.retryURL, Valid: true},
			}))
		})
	}
}

func TestPreparationLookupError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		code oops.Code
	}{
		{"missing", pgx.ErrNoRows, oops.CodeNotFound},
		{"database unavailable", errors.New("database unavailable"), oops.CodeUnexpected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := preparationLookupError(tc.err, "resource not found")
			var classified *oops.ShareableError
			require.ErrorAs(t, got, &classified)
			require.Equal(t, tc.code, classified.Code)
			require.ErrorIs(t, got, tc.err)
		})
	}
}

func TestPreparationDCRRejectsPlaintextBeforeSubmission(t *testing.T) {
	t.Parallel()
	// A nil policy would return indeterminate if transport validation were skipped.
	_, state := (&Service{}).submitPreparationDCR(t.Context(), PreparationInput{}, "http://registration.example/register", "client_secret_basic")
	require.Equal(t, "manual_setup_required", state)
}

func TestPreparationPublicClientOmittedAuthMethods(t *testing.T) {
	t.Parallel()
	client := repo.RemoteSessionClient{TokenEndpointAuthMethod: pgtype.Text{String: "none", Valid: true}}
	for _, tc := range []struct {
		name    string
		methods []string
		want    bool
	}{
		{"omitted", nil, false},
		{"empty", []string{}, false},
		{"explicit public support", []string{"none"}, true},
		{"confidential only", []string{"client_secret_basic"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, preparationClientConfigurationValid(t.Context(), nil, client, repo.RemoteSessionIssuer{TokenEndpointAuthMethodsSupported: tc.methods}, ""))
		})
	}
}

func TestPreparationDCRRejectsUnsupportedConfidentialMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"", "none", "private_key_jwt", "unknown"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			_, state := (&Service{}).submitPreparationDCR(t.Context(), PreparationInput{}, "https://issuer.example/register", method)
			require.Equal(t, "manual_setup_required", state, "invalid authentication must fail before any HTTP submission")
			response := preparationDCRResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: method, GrantTypes: []string{PreparationJWTBearerGrant}}
			require.Equal(t, "indeterminate", validatePreparationDCR(response, nil, method))
		})
	}
}
