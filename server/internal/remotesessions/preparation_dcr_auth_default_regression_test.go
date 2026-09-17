package remotesessions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationDCRIntegration_OmittedAuthMethodRetainsRegistration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, extra, state string
		postOnly           bool
		retained           bool
	}{
		{"confirmed JWT grant", `,"grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, "ready", false, true},
		{"omitted grants", ``, "unknown_grants", false, true},
		{"empty grants", `,"grant_types":[]`, "manual_setup_required", false, true},
		{"expired secret", `,"grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"],"client_secret_expires_at":1`, "manual_setup_required", false, true},
		{"post requested but basic default unsupported", `,"grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, "manual_setup_required", true, true},
		{"explicit empty is not omission", `,"token_endpoint_auth_method":""`, "indeterminate", false, false},
		{"explicit null is not omission", `,"token_endpoint_auth_method":null`, "indeterminate", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"client_id":"default-auth-client","client_secret":"default-auth-secret"` + tc.extra + `}`))
			}))
			t.Cleanup(server.Close)
			ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
			auth, _ := contextvalues.GetAuthContext(ctx)
			if tc.postOnly {
				in.TokenEndpointAuthMethod = "client_secret_post"
				require.NoError(t, testrepo.New(ti.conn).SetPreparationFixtureIssuerPostAuth(ctx, testrepo.SetPreparationFixtureIssuerPostAuthParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
			}
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, tc.state, result.State)
			if tc.retained {
				require.NotEqual(t, uuid.Nil, result.ClientID)
				require.Equal(t, "default-auth-client", result.ExternalClientID)
				require.Equal(t, "provider_returned", result.GrantSource)
				record, err := testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, testrepo.GetPreparationFixtureRegistrationParams{ID: result.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
				require.NoError(t, err)
				require.NotEmpty(t, record.ClientSecretEncrypted.String)
				require.NotEqual(t, "default-auth-secret", record.ClientSecretEncrypted.String)
				require.Equal(t, result.GrantTypes, record.GrantTypes)
				if tc.name == "omitted grants" {
					require.Nil(t, record.GrantTypes)
				} else if tc.name != "empty grants" {
					require.Equal(t, []string{preparationJWTGrant}, record.GrantTypes)
				}
			} else {
				require.Equal(t, uuid.Nil, result.ClientID)
			}
			wire, err := json.Marshal(result) //nolint:musttag // Check secret-free diagnostic output.
			require.NoError(t, err)
			require.NotContains(t, string(wire), "default-auth-secret")
			restarted := restartPreparationService(t, ti)
			for range 2 {
				again, err := restarted.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, result, again)
			}
			require.Equal(t, int32(1), posts.Load())
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
		})
	}
}
