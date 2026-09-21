package identityproviderconnections

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/stretchr/testify/require"
)

func TestBuildConnectionView_ChecklistVerification(t *testing.T) {
	t.Parallel()

	type row struct {
		name        string
		status      string
		lastError   string
		granted     []string
		dpop        bool
		unsubmitted bool
		// token is the app, key and DPoP steps: they are evidenced together.
		token  *bool
		access *bool
		scopes *bool
	}
	tests := []row{
		{name: "verified", status: StatusVerified, granted: RequiredOktaScopes, dpop: true, token: new(true), access: new(true), scopes: new(true)},
		{name: "pending unsubmitted", status: StatusPending, unsubmitted: true},
		{name: "pending submitted", status: StatusPending},
		{name: "dpop only", status: StatusDegraded, lastError: ReasonDPoPNotBound, granted: RequiredOktaScopes, token: new(true), access: new(true), scopes: new(true)},
		{name: "missing role", status: StatusDegraded, lastError: ReasonMissingRole, granted: RequiredOktaScopes, dpop: true, token: new(true), access: new(false), scopes: new(true)},
		{name: "apps read", status: StatusDegraded, lastError: ReasonReadFailedApps, granted: RequiredOktaScopes, dpop: true, token: new(true), access: new(false), scopes: new(true)},
		{name: "users read", status: StatusDegraded, lastError: ReasonReadFailedUsers, granted: RequiredOktaScopes, dpop: true, token: new(true), access: new(false), scopes: new(true)},
		{name: "groups read", status: StatusDegraded, lastError: ReasonReadFailedGroup, granted: RequiredOktaScopes, dpop: true, token: new(true), access: new(false), scopes: new(true)},
		{name: "missing scope", status: StatusDegraded, lastError: ReasonMissingScope, granted: []string{"okta.apps.read"}, dpop: true, token: new(true), scopes: new(false)},
		{name: "missing scope and failed read", status: StatusDegraded, lastError: ReasonMissingScope + "," + ReasonReadFailedApps, granted: []string{"okta.apps.read"}, dpop: true, token: new(true), access: new(false), scopes: new(false)},
		{name: "no token", status: StatusDegraded, lastError: ReasonMissingScope + "," + ReasonDPoPNotBound, scopes: new(false)},
		{name: "pending rejected", status: StatusPending, lastError: LastErrorCredentialRejected + "," + ReasonKeyNotFetched},
		{name: "reverification rejected", status: StatusDegraded, lastError: LastErrorCredentialRejected + "," + ReasonKeyNotFetched, granted: RequiredOktaScopes, dpop: true},
		{name: "reverification unreachable", status: StatusVerified, lastError: LastErrorOktaUnreachable, granted: RequiredOktaScopes, dpop: true},
		{name: "degraded reverification unreachable", status: StatusDegraded, lastError: LastErrorOktaUnreachable, granted: RequiredOktaScopes, dpop: true},
		{name: "key not fetched", status: StatusDegraded, lastError: ReasonKeyNotFetched, granted: RequiredOktaScopes, dpop: true, scopes: new(true)},
		{name: "revoked", status: StatusRevoked, granted: RequiredOktaScopes, dpop: true},
	}

	for _, mode := range []string{ListingModeCustomApp, ListingModeOIN} {
		appKey := ChecklistKeyCreateAPIServicesApp
		if mode == ListingModeOIN {
			appKey = ChecklistKeyAddOINApp
		}
		for _, tt := range tests {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
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
				}, AgentObservation{})

				var dpop *bool
				if tt.token != nil {
					dpop = new(tt.dpop)
				}
				want := map[string]*bool{
					appKey:                              tt.token,
					ChecklistKeyPublicKeyAuth:           tt.token,
					ChecklistKeyDPoP:                    dpop,
					ChecklistKeyGrantScopes:             tt.scopes,
					ChecklistKeyAssignAdminRoles:        tt.access,
					ChecklistKeySubmitClientID:          new(!tt.unsubmitted),
					ChecklistKeyRegisterAIAgent:         nil,
					ChecklistKeyLinkAgentApp:            nil,
					ChecklistKeyActivateAgentApp:        nil,
					ChecklistKeyRecordAIAgent:           nil,
					ChecklistKeyFirstResourceConnection: nil,
				}
				got := make(map[string]*bool, len(view.Checklist))
				for _, item := range view.Checklist {
					got[item.Key] = item.Completed
					if item.Key == ChecklistKeyAssignAdminRoles {
						require.Equal(t, accessDescription(tt.access), item.Description)
					}
				}
				require.Equal(t, want, got)
			})
		}
	}
}

func TestAccessDescription_ReplacesTheInstructionOnceObserved(t *testing.T) {
	t.Parallel()
	require.Equal(t, accessInstruction, accessDescription(nil))
	require.Equal(t, accessInstruction, accessDescription(new(false)))
	require.Equal(t, "Speakeasy can read apps, users, and groups", accessDescription(new(true)))
	require.Contains(t, accessInstruction, "Application Administrator")
	require.Contains(t, accessInstruction, "Read-only Administrator")
	require.Contains(t, accessInstruction, "multi-factor authentication")
}
