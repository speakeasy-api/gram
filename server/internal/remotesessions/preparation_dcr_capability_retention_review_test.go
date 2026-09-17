package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationDCRReview_CapabilityRefreshDuringPOSTRetainsConfirmedCredentials(t *testing.T) {
	t.Parallel()
	var withdraw atomic.Bool
	var posts atomic.Int32
	var refresh func() error
	refreshResult := make(chan error, 1)
	registration := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		withdraw.Store(true)
		refreshResult <- refresh()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"retained-review-client","client_secret":"retained-review-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`))
	}))
	t.Cleanup(registration.Close)
	ctx, ti, in, interactive := preparationDCRFixture(t, registration.URL)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, _ := contextvalues.GetAuthContext(ctx)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		doc["registration_endpoint"] = registration.URL
		doc["grant_types_supported"] = []string{"authorization_code", preparationJWTGrant}
		doc["token_endpoint_auth_methods_supported"] = []string{"client_secret_basic"}
		if !withdraw.Load() {
			doc["authorization_grant_profiles_supported"] = []string{"urn:ietf:params:oauth:grant-profile:id-jag"}
		}
	})
	issuer, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{Slug: "dcr-capability-review", Issuer: upstream.URL, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}})
	require.NoError(t, err)
	in.RemoteSessionIssuerID = issuer.ID
	refresh = func() error {
		_, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &issuersgen.RefreshRemoteSessionIssuerMetadataPayload{ID: issuer.ID.String()})
		if err != nil {
			return fmt.Errorf("refresh issuer fixture: %w", err)
		}
		return nil
	}
	require.NoError(t, refresh())
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.NoError(t, <-refreshResult, "capability-only refresh must finish while the registration POST is active")
	require.Equal(t, "unsupported_profile", result.State)
	require.NotEqual(t, uuid.Nil, result.ClientID)
	require.Equal(t, "provider_returned", result.GrantSource)
	record, err := testrepo.New(ti.conn).GetPreparationFixtureRegistration(ctx, testrepo.GetPreparationFixtureRegistrationParams{ID: result.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	require.NotEmpty(t, record.ClientSecretEncrypted.String)
	require.NotEqual(t, "retained-review-secret", record.ClientSecretEncrypted.String)
	again, err := restartPreparationService(t, ti).PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, int32(1), posts.Load())
	assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
}
