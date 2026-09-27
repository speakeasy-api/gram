package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationDCRIntegration_DiscoveryRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		grants  []string
		expired bool
		removed bool
		want    string
	}{
		{"ready", []string{oauthwire.GrantTypeJWTBearer}, false, false, "ready"},
		{"unknown grants", nil, false, false, "unknown_grants"},
		{"missing grant", []string{"authorization_code"}, false, false, "manual_setup_required"},
		{"expired credential", []string{oauthwire.GrantTypeJWTBearer}, true, false, "manual_setup_required"},
		{"removed capabilities", []string{oauthwire.GrantTypeJWTBearer}, false, true, "unsupported_profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posts atomic.Int32
			requested, respond := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if posts.Add(1) == 1 {
					close(requested)
				}
				<-respond
				response := map[string]any{
					"client_id": "recovered-client", "client_secret": "recovered-secret",
					"token_endpoint_auth_method": "client_secret_basic", "scope": "read",
				}
				if tc.grants != nil {
					response["grant_types"] = tc.grants
				}
				if tc.expired {
					response["client_secret_expires_at"] = time.Now().Add(-time.Minute).Unix()
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(response)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() {
				select {
				case <-respond:
				default:
					close(respond)
				}
			})
			ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := repo.New(ti.conn)
			issuer, err := q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
				ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID),
			})
			require.NoError(t, err)
			type outcome struct {
				result *remotesessions.PreparationResult
				err    error
			}
			completed := make(chan outcome, 1)
			go func() { result, err := ti.service.PrepareIdentityChaining(ctx, in); completed <- outcome{result, err} }()
			select {
			case <-requested:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// The provider cannot respond until discovery failure has been committed.
			require.NoError(t, testrepo.New(ti.conn).FailPreparationFixtureIssuerMetadata(ctx, testrepo.FailPreparationFixtureIssuerMetadataParams{
				ID: issuer.ID, ProjectID: issuer.ProjectID,
			}))
			close(respond)
			var out outcome
			select {
			case out = <-completed:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, out.err)
			require.Equal(t, "transient_failure", out.result.State)
			require.Equal(t, "provider_returned", out.result.GrantSource)
			restarted := restartPreparationService(t, ti)
			check := func(want string) {
				t.Helper()
				read, err := restarted.ReadIdentityChaining(ctx, in)
				require.NoError(t, err)
				repeated, err := restarted.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				for _, result := range []*remotesessions.PreparationResult{read, repeated} {
					require.Equal(t, want, result.State)
					require.Equal(t, out.result.ClientID, result.ClientID)
					require.Equal(t, out.result.Generation, result.Generation)
					require.Equal(t, "provider_returned", result.GrantSource)
					require.Equal(t, []string{"read"}, result.Scopes)
				}
				require.EqualValues(t, 1, posts.Load(), "recovery must never replay registration")
			}
			check("transient_failure")
			// A successful refresh replaces both the metadata and advertised arrays.
			profiles, grants := []string{oauthwire.GrantProfileIDJAG}, []string{oauthwire.GrantTypeJWTBearer}
			document := map[string]any{
				"registration_endpoint":                 server.URL,
				"token_endpoint_auth_methods_supported": issuer.TokenEndpointAuthMethodsSupported,
			}
			if tc.removed {
				profiles, grants = []string{}, []string{}
			} else {
				document["authorization_grant_profiles_supported"] = profiles
				document["grant_types_supported"] = grants
			}
			metadata, err := json.Marshal(document)
			require.NoError(t, err)
			_, err = q.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams{
				ID: issuer.ID, Issuer: issuer.Issuer, ProjectID: issuer.ProjectID, OrganizationID: issuer.OrganizationID,
				Metadata: string(metadata), RegistrationEndpoint: server.URL,
				AuthorizationGrantProfilesSupported: profiles, GrantTypesSupported: grants,
				TokenEndpointAuthMethodsSupported: issuer.TokenEndpointAuthMethodsSupported,
				ScopesSupported:                   []string{}, ResponseTypesSupported: []string{},
				CodeChallengeMethodsSupported: []string{}, IntrospectionEndpointAuthMethodsSupported: []string{},
				IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
			})
			require.NoError(t, err)
			check(tc.want)
			check(tc.want)
			stored, err := testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, testrepo.GetPreparationFixtureRegistrationParams{
				ID: out.result.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
			})
			require.NoError(t, err)
			require.Equal(t, []string{"read"}, stored.RequestedScopes)
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
		})
	}
}
