package identityproviderconnections

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/stretchr/testify/require"
)

func TestBuildConnectionView_ChecklistVerification(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{ListingModeCustomApp, ListingModeOIN} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			for _, tt := range []struct {
				name, status, lastError     string
				granted                     []string
				dpop                        bool
				app, access, scopes, submit *bool
				unsubmitted                 bool
			}{
				{"verified", StatusVerified, "", RequiredOktaScopes, true, new(true), new(true), new(true), new(true), false},
				{"pending unsubmitted", StatusPending, "", nil, false, nil, nil, nil, new(false), true},
				{"pending submitted", StatusPending, "", nil, false, nil, nil, nil, new(true), false},
				{"dpop only", StatusDegraded, ReasonDPoPNotBound, RequiredOktaScopes, false, new(true), new(true), new(true), new(true), false},
				{"missing role", StatusDegraded, ReasonMissingRole, RequiredOktaScopes, true, new(true), new(false), new(true), new(true), false},
				{"apps read", StatusDegraded, ReasonReadFailedApps, RequiredOktaScopes, true, new(true), new(false), new(true), new(true), false},
				{"users read", StatusDegraded, ReasonReadFailedUsers, RequiredOktaScopes, true, new(true), new(false), new(true), new(true), false},
				{"groups read", StatusDegraded, ReasonReadFailedGroup, RequiredOktaScopes, true, new(true), new(false), new(true), new(true), false},
				{"missing scope", StatusDegraded, ReasonMissingScope, []string{"okta.apps.read"}, true, new(true), nil, new(false), new(true), false},
				{"missing scope and failed read", StatusDegraded, ReasonMissingScope + "," + ReasonReadFailedApps, []string{"okta.apps.read"}, true, new(true), new(false), new(false), new(true), false},
				{"no token", StatusDegraded, ReasonMissingScope + "," + ReasonDPoPNotBound, nil, false, nil, nil, new(false), new(true), false},
				{"pending rejected", StatusPending, LastErrorCredentialRejected + "," + ReasonKeyNotFetched, nil, false, nil, nil, nil, new(true), false},
				{"reverification rejected", StatusDegraded, LastErrorCredentialRejected + "," + ReasonKeyNotFetched, RequiredOktaScopes, true, nil, nil, nil, new(true), false},
				{"reverification unreachable", StatusVerified, LastErrorOktaUnreachable, RequiredOktaScopes, true, nil, nil, nil, new(true), false},
				{"degraded reverification unreachable", StatusDegraded, LastErrorOktaUnreachable, RequiredOktaScopes, true, nil, nil, nil, new(true), false},
				{"key not fetched", StatusDegraded, ReasonKeyNotFetched, RequiredOktaScopes, true, nil, nil, new(true), new(true), false},
				{"revoked", StatusRevoked, "", RequiredOktaScopes, true, nil, nil, nil, new(true), false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					connection := repo.IdentityProviderConnection{Provider: "okta", Status: tt.status, LastError: conv.ToPGTextEmpty(tt.lastError)}
					clientID := "0oatestclient"
					if tt.unsubmitted {
						clientID = PlaceholderClientID(connection.Provider, connection.ID)
					}
					view := buildConnectionView(connectionRows{
						Connection: connection,
						Okta:       repo.OktaIdentityProviderConnection{ListingMode: mode, GrantedScopes: tt.granted, DpopRequired: tt.dpop},
						Managed:    &ManagedClient{ClientID: clientID},
					}, agentSignal{App: nil, ConnectionRecorded: nil})
					appKey := "create_api_services_app"
					if mode == ListingModeOIN {
						appKey = "add_oin_app"
					}
					var connect int
					for _, item := range view.Checklist {
						if item.Key == "grant_scopes" {
							require.Equal(t, tt.scopes, item.Completed, item.Key)
						}
						if item.Key == "submit_client_id" {
							require.Equal(t, tt.submit, item.Completed, item.Key)
						}
						if item.Key == "public_key_auth" {
							require.Equal(t, tt.app, item.Completed)
						}
						if item.Key == "dpop" {
							if tt.app == nil {
								require.Nil(t, item.Completed)
							} else {
								require.Equal(t, new(tt.dpop), item.Completed)
							}
						}
						if item.Key == appKey {
							require.Equal(t, tt.app, item.Completed)
						}
						if item.Key == "assign_admin_roles" {
							require.Equal(t, tt.access, item.Completed)
							require.Equal(t, "Allow access to apps, users, and groups", item.Title)
							require.Contains(t, item.Details[0], "read apps, users, and groups through the Okta API")
							require.Contains(t, item.Details[0], "does not check which named admin roles are assigned")
							if tt.access != nil && *tt.access {
								require.Equal(t, "Speakeasy can read apps, users, and groups", item.Description)
							} else {
								require.Contains(t, item.Description, "Application Administrator")
								require.Contains(t, item.Description, "Read-only Administrator")
							}
						}
						if item.Group == ChecklistGroupConnect {
							connect++
							if tt.name == "verified" {
								require.Equal(t, new(true), item.Completed, item.Key)
							}
						} else {
							require.Nil(t, item.Completed)
						}
					}
					require.Equal(t, 6, connect)
				})
			}
		})
	}
}
