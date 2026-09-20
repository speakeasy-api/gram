package identityproviderconnections_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
)

func completion(items []identityproviderconnections.ChecklistItem) map[string]*bool {
	out := make(map[string]*bool, len(items))
	for _, item := range items {
		out[item.Key] = item.Completed
	}
	return out
}

func TestOktaChecklist_ShapeIsSixPlusOne(t *testing.T) {
	t.Parallel()
	items := identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{})

	var connect, xaa int
	for _, item := range items {
		switch item.Group {
		case identityproviderconnections.ChecklistGroupConnect:
			connect++
		case identityproviderconnections.ChecklistGroupCrossAppAccess:
			xaa++
		default:
			t.Fatalf("unexpected group %q", item.Group)
		}
	}
	require.Equal(t, 6, connect)
	require.Equal(t, 5, xaa)
	require.Equal(t, "first_resource_connection", items[len(items)-1].Key)
}

func TestOktaChecklist_NothingObservedBeforeVerification(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeOIN, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{}))

	for _, key := range []string{"add_oin_app", "public_key_auth", "dpop", "grant_scopes", "assign_admin_roles", "register_ai_agent", "link_agent_app", "activate_agent_app", "record_ai_agent", "first_resource_connection"} {
		require.Nil(t, done[key], key)
	}
	require.NotNil(t, done["submit_client_id"])
	require.False(t, *done["submit_client_id"])
}

func TestOktaChecklist_VerifiedTicksEverythingObservable(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{
		Checked:           true,
		ClientIDSubmitted: true,
		DPoPBound:         true,
		MissingScopes:     []string{},
	}))

	for _, key := range []string{"create_api_services_app", "public_key_auth", "dpop", "grant_scopes", "assign_admin_roles", "submit_client_id"} {
		require.NotNil(t, done[key], key)
		require.True(t, *done[key], key)
	}
	require.Nil(t, done["activate_agent_app"], "the agent steps in Okta stay manual")
	require.Nil(t, done["record_ai_agent"])
}

func TestOktaChecklist_DegradedReportsWhatVerificationSaw(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{
		Checked:           true,
		ClientIDSubmitted: true,
		DPoPBound:         false,
		MissingScopes:     []string{"okta.users.read"},
	}))

	require.True(t, *done["public_key_auth"], "a minted token proves the key-based credential")
	require.True(t, *done["submit_client_id"])
	require.False(t, *done["dpop"])
	require.False(t, *done["grant_scopes"])
}

func TestOktaChecklist_ScopeRefusalDoesNotProveTokenIssued(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{
		Checked:           true,
		ClientIDSubmitted: true,
		MissingScopes:     identityproviderconnections.RequiredOktaScopes,
		Reasons:           []string{identityproviderconnections.ReasonMissingScope, identityproviderconnections.ReasonDPoPNotBound},
	}))
	for _, key := range []string{"create_api_services_app", "public_key_auth", "dpop", "assign_admin_roles"} {
		require.Nil(t, done[key], key)
	}
	require.False(t, *done["grant_scopes"])
	require.True(t, *done["submit_client_id"])
}

func TestOktaChecklist_AdminInstructions(t *testing.T) {
	t.Parallel()
	const keyURL = "https://example.test/jwks.json"
	items := identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, keyURL, identityproviderconnections.ChecklistSignal{})
	byKey := make(map[string]identityproviderconnections.ChecklistItem, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}
	require.Equal(t, "Create an app for Speakeasy", byKey["create_api_services_app"].Title)
	require.Contains(t, byKey["create_api_services_app"].Description, "Classic experience if offered, select API Services")
	require.Contains(t, byKey["create_api_services_app"].Description, "save it")
	require.Contains(t, byKey["create_api_services_app"].Details[0], "Coming soon")
	require.Contains(t, byKey["create_api_services_app"].Details[0], "Create App Integration > Classic experience")
	require.Contains(t, byKey["create_api_services_app"].Details[0], "on the saved app")
	auth := byKey["public_key_auth"]
	require.Equal(t, "Let Okta recognize Speakeasy", auth.Title)
	require.Contains(t, auth.Description, "connection requests come from Speakeasy")
	require.Equal(t, []string{
		"On the newly created Speakeasy app's General tab, find Client Credentials, click Edit, and set Client authentication to Public key / Private key.",
		"Choose Use a URL to fetch keys dynamically.",
		"Enter this URL: " + keyURL,
	}, auth.Details)
	dpop := byKey["dpop"]
	require.Equal(t, "Keep token protection (DPoP) enabled", dpop.Title)
	require.Contains(t, dpop.Description, "Require Demonstrating Proof of Possession (DPoP) header in token requests")
	require.Contains(t, dpop.Description, "someone who copies the token cannot use it on its own")
	scopes := byKey["grant_scopes"]
	require.Contains(t, scopes.Description, "Okta API Scopes")
	for _, scope := range identityproviderconnections.RequiredOktaScopes {
		require.Contains(t, scopes.Description, scope)
	}
	require.Contains(t, scopes.Description, "without changing them")
	require.Contains(t, byKey["assign_admin_roles"].Description, "Admin roles")
	require.Contains(t, byKey["submit_client_id"].Description, "Client ID")
	register := byKey["register_ai_agent"]
	require.Equal(t, "Register the Speakeasy AI agent", register.Title)
	require.Contains(t, register.Description, "Directory > AI Agents")
	require.Contains(t, register.Description, "Name it Speakeasy Agent")
	link := byKey["link_agent_app"]
	require.Contains(t, link.Description, "keep Create a new OIDC app linked to this AI agent selected")
	require.Contains(t, link.Description, "does not change how your people sign in to Speakeasy")
	require.Contains(t, link.Details[0], "permanent")
	activate := byKey["activate_agent_app"]
	require.Contains(t, activate.Description, "Staged")
	require.Contains(t, activate.Description, "Speakeasy Agent app marked Linked AI Agent")
	require.Contains(t, activate.Description, "set it to Active")
	require.Contains(t, activate.Description, "Assignments")
	require.Contains(t, activate.Description, "only for the users assigned here")
	require.Contains(t, activate.Details[0], "moves the agent from Staged to Active")
	require.Contains(t, activate.Details[1], "upcoming release")
	record := byKey["record_ai_agent"]
	require.Contains(t, record.Description, "wlp")
	first := byKey["first_resource_connection"]
	require.Contains(t, first.Description, "one resource connection per MCP server")
	require.Contains(t, first.Details[0], "does not create the connection in Okta")
	for _, item := range []identityproviderconnections.ChecklistItem{register, link, activate, record, first} {
		for _, text := range append([]string{item.Description}, item.Details...) {
			require.NotContains(t, text, keyURL, "the management JWKS is never offered as the agent credential")
			require.NotContains(t, text, "optional")
			require.NotContains(t, text, "Register manually", "the console has no manual-registration choice")
			require.NotContains(t, text, "External ID", "the wizard has no platform or external id fields")
		}
	}
	require.Nil(t, activate.Completed)
}

func TestOktaChecklist_RecordedAgentTicksTheRecordStep(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{AgentRecorded: true}))

	require.True(t, *done["record_ai_agent"])
	require.True(t, *done["register_ai_agent"], "a recorded id implies a registered agent")
	require.Nil(t, done["link_agent_app"], "no linked app observed")
	require.Nil(t, done["activate_agent_app"])
}

func TestOktaChecklist_LinkedAppStateDrivesTheAppSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		app      identityproviderconnections.AgentAppSignal
		linked   bool
		ready    *bool
		contains string
	}{
		{name: "not in the sync", app: identityproviderconnections.AgentAppSignal{Found: false, Active: false, Assigned: false}, linked: false, ready: nil, contains: ""},
		{name: "inactive", app: identityproviderconnections.AgentAppSignal{Found: true, Active: false, Assigned: true}, linked: true, ready: new(false), contains: "is not Active"},
		{name: "unassigned", app: identityproviderconnections.AgentAppSignal{Found: true, Active: true, Assigned: false}, linked: true, ready: new(false), contains: "has no users or groups assigned"},
		{name: "ready", app: identityproviderconnections.AgentAppSignal{Found: true, Active: true, Assigned: true}, linked: true, ready: new(true), contains: "as of the last applications sync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			items := identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{AgentRecorded: true, AgentApp: &tt.app})
			done := completion(items)
			require.Equal(t, tt.linked, *done["link_agent_app"])
			require.Equal(t, tt.ready, done["activate_agent_app"])
			for _, item := range items {
				if item.Key == "activate_agent_app" {
					require.Contains(t, item.Description, tt.contains)
				}
				if item.Key == "link_agent_app" && !tt.linked {
					require.Contains(t, item.Description, "found no app with the bound application ID")
				}
			}
		})
	}
}

func TestOktaChecklist_RecordedConnectionTicksTheFirstConnectionStep(t *testing.T) {
	t.Parallel()
	none := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{ConnectionRecorded: new(false)}))
	require.Nil(t, none["first_resource_connection"], "not done yet is not a fault")

	one := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeCustomApp, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{ConnectionRecorded: new(true)}))
	require.True(t, *one["first_resource_connection"])
}
