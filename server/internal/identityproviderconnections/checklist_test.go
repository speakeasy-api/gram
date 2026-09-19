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
	require.Equal(t, 1, xaa)
	require.Equal(t, "create_ai_agent", items[len(items)-1].Key)
	require.Len(t, items[len(items)-1].Details, 5)
}

func TestOktaChecklist_NothingObservedBeforeVerification(t *testing.T) {
	t.Parallel()
	done := completion(identityproviderconnections.OktaChecklist(identityproviderconnections.ListingModeOIN, "https://example.test/jwks.json", identityproviderconnections.ChecklistSignal{}))

	for _, key := range []string{"add_oin_app", "public_key_auth", "dpop", "grant_scopes", "assign_admin_roles", "create_ai_agent"} {
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
	require.Nil(t, done["create_ai_agent"], "the agent stays manual")
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
	agent := byKey["create_ai_agent"]
	require.Equal(t, "Register the Speakeasy AI agent", agent.Title)
	require.Contains(t, agent.Description, "only for the users assigned to it")
	require.Contains(t, agent.Description, "resource connection per MCP server on the Cross App Access tab")
	require.Len(t, agent.Details, 5)
	require.Contains(t, agent.Details[0], "Directory > AI Agents")
	require.Contains(t, agent.Details[1], "keep Create a new OIDC app linked to this AI agent selected")
	require.Contains(t, agent.Details[1], "does not change how your people sign in to Speakeasy")
	require.Contains(t, agent.Details[1], "permanent")
	require.Contains(t, agent.Details[2], "Staged")
	require.Contains(t, agent.Details[2], "set it to Active")
	require.Contains(t, agent.Details[2], "Assignments")
	require.Contains(t, agent.Details[3], "Leave the agent in Staged status")
	require.Contains(t, agent.Details[3], "upcoming release")
	require.NotContains(t, agent.Details[3], keyURL, "the management JWKS is never offered as the agent credential")
	require.Contains(t, agent.Details[4], "wlp")
	for _, d := range agent.Details {
		require.NotContains(t, d, "optional")
		require.NotContains(t, d, "Register manually", "the console has no manual-registration choice")
		require.NotContains(t, d, "External ID", "the wizard has no platform or external id fields")
	}
	require.Nil(t, agent.Completed)
}
