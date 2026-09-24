package remotesessions_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationRefresh_EndpointChangesRequireUnlinkButEvidenceDoesNot(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"project", "organization", "global"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			ctx = withAdmin(t, ctx)
			auth, _ := contextvalues.GetAuthContext(ctx)
			var changedEndpoint, removedCapability atomic.Bool
			upstream := fakeIssuerServer(t, func(doc map[string]any) {
				if changedEndpoint.Load() {
					issuerURL, ok := doc["issuer"].(string)
					if !ok {
						t.Error("fixture issuer must be a string")
						return
					}
					doc["token_endpoint"] = issuerURL + "/new-token"
				}
				if !removedCapability.Load() {
					doc["authorization_grant_profiles_supported"] = []string{"urn:ietf:params:oauth:grant-profile:id-jag"}
				}
			})
			params := repo.CreateRemoteSessionIssuerParams{Slug: "refresh-ema-" + tier, Issuer: upstream.URL, ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}}
			if tier != "global" {
				params.OrganizationID = conv.ToPGText(auth.ActiveOrganizationID)
			}
			if tier == "project" {
				params.ProjectID = conv.ToNullUUID(*auth.ProjectID)
			}
			q := repo.New(ti.conn)
			issuer, err := q.CreateRemoteSessionIssuer(ctx, params)
			require.NoError(t, err)
			refresh := func() error {
				var err error
				switch tier {
				case "project":
					_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &issuersgen.RefreshRemoteSessionIssuerMetadataPayload{ID: issuer.ID.String()})
				case "organization":
					_, err = ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{ID: issuer.ID.String()})
				case "global":
					_, err = ti.service.RefreshGlobalIssuerMetadata(ctx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{ID: issuer.ID.String()})
				}
				if err != nil {
					return fmt.Errorf("refresh issuer fixture: %w", err)
				}
				return nil
			}
			require.NoError(t, refresh())
			require.Contains(t, loadIssuerByID(t, ctx, ti, issuer.ID).AuthorizationGrantProfilesSupported, "urn:ietf:params:oauth:grant-profile:id-jag", "initial refresh must persist capability before removal")
			key := repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: issuer.ID, Resource: in.Resource}
			require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key)))
			removedCapability.Store(true)
			require.NoError(t, refresh(), "capability removal must not be blocked by an active selection")
			stored := loadIssuerByID(t, ctx, ti, issuer.ID)
			require.Empty(t, stored.AuthorizationGrantProfilesSupported)
			changedEndpoint.Store(true)
			requireOopsCode(t, refresh(), oops.CodeConflict)
			refresher, _ := newIssuerMetadataRefresher(t, ti)
			outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, issuer.ID))
			require.NoError(t, err)
			require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure, outcome, "background refresh shares the reconfiguration policy")
			require.Equal(t, stored.TokenEndpoint, loadIssuerByID(t, ctx, ti, issuer.ID).TokenEndpoint)
			binding, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: key.ProjectID, OrganizationID: key.OrganizationID, ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1, State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{}, RemoteSessionClientID: uuid.NullUUID{}})
			require.NoError(t, err)
			require.NoError(t, refresh(), "explicit unlink releases the endpoint configuration")
			require.Equal(t, upstream.URL+"/new-token", loadIssuerByID(t, ctx, ti, issuer.ID).TokenEndpoint.String)
		})
	}
}

func TestIssuerUpdate_ActiveBindingAllowsPresentationAndNoopEdits(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"project", "organization", "global"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			ctx = withAdmin(t, ctx)
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := repo.New(ti.conn)
			params := repo.CreateRemoteSessionIssuerParams{Slug: "update-bound-" + tier, Issuer: "https://issuer.example.com", TokenEndpoint: conv.ToPGText("https://issuer.example.com/token"), ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{}}
			if tier != "global" {
				params.OrganizationID = conv.ToPGText(auth.ActiveOrganizationID)
			}
			if tier == "project" {
				params.ProjectID = conv.ToNullUUID(*auth.ProjectID)
			}
			issuer, err := q.CreateRemoteSessionIssuer(ctx, params)
			require.NoError(t, err)
			require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: issuer.ID, Resource: in.Resource}))
			update := func(name, token, identity, jwks *string) error {
				var err error
				switch tier {
				case "project":
					_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{ID: issuer.ID.String(), Name: name, TokenEndpoint: token, Issuer: identity, JwksURI: jwks, LogoAssetID: new(""), ClientSetupDocumentationURL: new("https://docs.example.com/setup"), ServiceDocumentation: new("https://docs.example.com/service")})
				case "organization":
					_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: issuer.ID.String(), Name: name, TokenEndpoint: token, Issuer: identity, JwksURI: jwks, LogoAssetID: new(""), ClientSetupDocumentationURL: new("https://docs.example.com/setup"), ServiceDocumentation: new("https://docs.example.com/service")})
				case "global":
					_, err = ti.service.UpdateGlobalIssuer(ctx, &adminrsgen.UpdateGlobalIssuerPayload{ID: issuer.ID.String(), Name: name, TokenEndpoint: token, Issuer: identity, JwksURI: jwks, LogoAssetID: new(""), ClientSetupDocumentationURL: new("https://docs.example.com/setup"), ServiceDocumentation: new("https://docs.example.com/service")})

				}
				if err != nil {
					return fmt.Errorf("update issuer fixture: %w", err)
				}
				return nil
			}
			require.NoError(t, update(new("Renamed provider"), nil, nil, nil))
			require.NoError(t, update(nil, new(issuer.TokenEndpoint.String), new(issuer.Issuer), new("")), "effective no-op configuration edits must succeed")
			stored := loadIssuerByID(t, ctx, ti, issuer.ID)
			require.Equal(t, "Renamed provider", stored.Name.String)
			require.False(t, stored.LogoAssetID.Valid)
			require.Equal(t, "https://docs.example.com/setup", stored.ClientSetupDocumentationUrl.String)
			require.Equal(t, "https://docs.example.com/service", stored.ServiceDocumentation.String)
			requireOopsCode(t, update(new("Must roll back"), new("https://issuer.example.com/new-token"), nil, nil), oops.CodeConflict)
			requireOopsCode(t, update(nil, new(""), nil, nil), oops.CodeConflict)
			requireOopsCode(t, update(nil, nil, new("https://other.example.com"), nil), oops.CodeConflict)
			requireOopsCode(t, update(nil, nil, nil, new("https://issuer.example.com/jwks")), oops.CodeConflict)
			after := loadIssuerByID(t, ctx, ti, issuer.ID)
			require.Equal(t, stored.Name, after.Name)
			require.Equal(t, stored.TokenEndpoint, after.TokenEndpoint)
			require.Equal(t, stored.Issuer, after.Issuer)
			require.Equal(t, stored.JwksUri, after.JwksUri)
		})
	}
}
