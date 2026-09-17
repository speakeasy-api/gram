package identityproviderconnections

import "strings"

// Listing modes select the console checklist template.
const (
	ListingModeCustomApp = "custom_app"
	ListingModeOIN       = "oin"
)

// RequiredOktaScopes are the Okta API scopes the connection verifies and the
// sync relies on.
var RequiredOktaScopes = []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}

// ChecklistItem is one console step for the administrator.
type ChecklistItem struct {
	Key         string
	Title       string
	Description string
}

// OktaChecklist renders the console steps for a listing mode. jwksURL is
// substituted into the steps that need it.
func OktaChecklist(listingMode, jwksURL string) []ChecklistItem {
	scopes := strings.Join(RequiredOktaScopes, ", ")
	items := make([]ChecklistItem, 0, 14)

	if listingMode == ListingModeOIN {
		items = append(items, ChecklistItem{
			Key:         "add_oin_app",
			Title:       "Add the Speakeasy app from the Okta Integration Network",
			Description: "In the Admin Console go to Applications > Browse App Catalog, search for Speakeasy, and add it. This creates the API Services app for you; skip creating one by hand.",
		})
	} else {
		items = append(items, ChecklistItem{
			Key:         "create_api_services_app",
			Title:       "Create an API Services app",
			Description: "In the Admin Console go to Applications > Create App Integration, choose API Services, and name it Speakeasy.",
		})
	}

	items = append(items,
		ChecklistItem{
			Key:         "public_key_auth",
			Title:       "Use public key / private key client authentication",
			Description: "On the app's General tab set Client authentication to Public key / Private key, choose Use a URL to fetch keys dynamically, and enter " + jwksURL + ". Gram signs every token request with a key it publishes there; no secret is shared.",
		},
		ChecklistItem{
			Key:         "dpop",
			Title:       "Keep DPoP required",
			Description: "Leave \"Require Demonstrating Proof of Possession (DPoP) header in token requests\" enabled. Gram binds every token to a DPoP proof; verification reports a bearer-only token as degraded.",
		},
		ChecklistItem{
			Key:         "grant_scopes",
			Title:       "Grant the Okta API scopes",
			Description: "On the app's Okta API Scopes tab grant " + scopes + ".",
		},
		ChecklistItem{
			Key:         "assign_admin_roles",
			Title:       "Assign admin roles to the app",
			Description: "On the app's Admin roles tab assign Application Administrator (org-wide) and Read-only Administrator. Okta requires an MFA step-up to assign admin roles, so be ready to re-authenticate.",
		},
		ChecklistItem{
			Key:         "submit_client_id",
			Title:       "Submit the app's client ID",
			Description: "Copy the Client ID (it starts with 0oa) from the app's General tab and submit it to this connection. Gram then verifies the credential and the granted scopes.",
		},
	)

	if listingMode != ListingModeOIN {
		items = append(items, ChecklistItem{
			Key:         "enable_xaa_on_resource_apps",
			Title:       "Enable Cross App Access on each MCP app",
			Description: "For every MCP app the agent will reach, open the app, go to its resource server tab, and enable Cross App Access (XAA). OIN-listed resource apps have this enabled already.",
		})
	}

	items = append(items,
		ChecklistItem{
			Key:         "create_ai_agent",
			Title:       "Create the AI agent",
			Description: "Go to Directory > AI Agents and create an agent bound to the SSO app your users sign in to Speakeasy with.",
		},
		ChecklistItem{
			Key:         "agent_delegated_caller",
			Title:       "Add the SSO app as a delegated caller",
			Description: "On the agent's Delegations tab add the SSO app as a delegated caller so it can obtain tokens on behalf of signed-in users.",
		},
		ChecklistItem{
			Key:         "agent_public_key",
			Title:       "Point the agent's credential at the JWKS URL",
			Description: "On the agent's Credentials tab choose public key / private key authentication, select external key management, and enter " + jwksURL + " as the JWKS URI so Okta fetches Gram's key dynamically. Adding a public key by hand would not use the managed key.",
		},
		ChecklistItem{
			Key:         "activate_agent",
			Title:       "Activate the agent",
			Description: "From the agent's Actions menu choose Activate.",
		},
		ChecklistItem{
			Key:         "resource_connections",
			Title:       "Add a resource connection per MCP app",
			Description: "On the agent's Resource connections tab add one connection per MCP app with the scope policy set to Allow all, the only value app-instance connections support. The console has no multi-select, so repeat this for every app.",
		},
		ChecklistItem{
			Key:         "record_agent",
			Title:       "Record the agent on this connection",
			Description: "Copy the agent ID and the ID of the app it is bound to into this connection. Okta does not expose them through its API, so they are kept for display only.",
		},
	)

	return items
}
