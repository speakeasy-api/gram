package identityproviderconnections

import (
	"slices"
	"strings"
)

// Listing modes select the console checklist template.
const (
	ListingModeCustomApp = "custom_app"
	ListingModeOIN       = "oin"
)

// Checklist groups: the service app the connection authenticates with, then
// the AI agent for Cross App Access.
const (
	ChecklistGroupConnect        = "connect"
	ChecklistGroupCrossAppAccess = "cross_app_access"
)

// RequiredOktaScopes are the Okta API scopes the connection verifies and the
// sync relies on.
var RequiredOktaScopes = []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}

// ChecklistItem is one console step for the administrator. Completed is nil
// for steps the server has no signal for.
type ChecklistItem struct {
	Key         string
	Group       string
	Title       string
	Description string
	Details     []string
	Completed   *bool
}

// ChecklistSignal is what the last verification observed, used to tick the
// steps it can vouch for. Zero value means nothing has been observed.
type ChecklistSignal struct {
	// Checked means the current scope verification completed, not necessarily
	// that Okta issued a token (scope refusal can complete without one).
	// Failed reverifications must not reuse an earlier successful result.
	Checked bool
	// ClientIDSubmitted is true once a real client id replaced the placeholder.
	ClientIDSubmitted bool
	// DPoPBound is whether the minted token was DPoP-bound.
	DPoPBound bool
	// MissingScopes are the required scopes Okta did not grant.
	MissingScopes []string
	// Reasons are the persisted reasons from the current verification.
	Reasons []string
}

// OktaChecklist renders the console steps for a listing mode. jwksURL is
// substituted into the steps that need it; signal ticks the steps a
// verification can observe.
func OktaChecklist(listingMode, jwksURL string, signal ChecklistSignal) []ChecklistItem {
	scopes := strings.Join(RequiredOktaScopes, ", ")
	items := make([]ChecklistItem, 0, 7)

	// Scope verification can complete without a token. Only report token
	// protection and key authentication when token issuance is evidenced.
	var keyAuth, dpop, scopesGranted *bool
	if signal.Checked {
		scopesGranted = new(len(signal.MissingScopes) == 0)
	}

	var appCreated, apiAccess *bool
	if signal.Checked && !slices.Contains(signal.Reasons, ReasonKeyNotFetched) {
		// A granted scope or a bound token proves credential acceptance. Scope
		// refusal can complete verification without minting a token at all.
		if signal.DPoPBound || len(signal.MissingScopes) < len(RequiredOktaScopes) {
			appCreated = new(true)
			keyAuth = new(true)
			dpop = new(signal.DPoPBound)
		}
		for _, reason := range signal.Reasons {
			switch reason {
			case ReasonMissingRole, ReasonReadFailedApps, ReasonReadFailedUsers, ReasonReadFailedGroup:
				apiAccess = new(false)
			}
		}
		// Missing scopes skip their reads, so absence of a failure alone is
		// not proof of access. An observed failure still takes precedence.
		if apiAccess == nil && len(signal.MissingScopes) == 0 && !slices.Contains(signal.Reasons, ReasonMissingScope) {
			apiAccess = new(true)
		}
	}
	accessDescription := "On the app's Admin roles tab, assign Application Administrator (org-wide) and Read-only Administrator. Okta will ask you to confirm your identity with multi-factor authentication before assigning roles."
	if apiAccess != nil && *apiAccess {
		accessDescription = "Speakeasy can read apps, users, and groups"
	}

	if listingMode == ListingModeOIN {
		items = append(items, ChecklistItem{
			Key:         "add_oin_app",
			Group:       ChecklistGroupConnect,
			Title:       "Add the Speakeasy app from the Okta Integration Network",
			Description: "In the Admin Console go to Applications > Browse App Catalog, search for Speakeasy, and add it. This creates the API Services app for you; skip creating one by hand.",
			Details:     []string{},
			Completed:   appCreated,
		})
	} else {
		items = append(items, ChecklistItem{
			Key:         "create_api_services_app",
			Group:       ChecklistGroupConnect,
			Title:       "Create an app for Speakeasy",
			Description: "In the Okta Admin Console, open Applications and choose Create App Integration. Use Classic experience if offered, select API Services, name the app Speakeasy, and save it.",
			Details: []string{
				"If Public key / Private key is marked Coming soon in the creation wizard, return to Applications and choose Create App Integration > Classic experience. Configure public-key authentication on the saved app, not in that wizard.",
			},
			Completed: appCreated,
		})
	}

	items = append(items,
		ChecklistItem{
			Key:         "public_key_auth",
			Group:       ChecklistGroupConnect,
			Title:       "Let Okta recognize Speakeasy",
			Description: "Add Speakeasy's key URL so Okta can check that connection requests come from Speakeasy. You do not need to create or share a password.",
			Details: []string{
				"On the newly created Speakeasy app's General tab, find Client Credentials, click Edit, and set Client authentication to Public key / Private key.",
				"Choose Use a URL to fetch keys dynamically.",
				"Enter this URL: " + jwksURL,
			},
			Completed: keyAuth,
		},
		ChecklistItem{
			Key:         "dpop",
			Group:       ChecklistGroupConnect,
			Title:       "Keep token protection (DPoP) enabled",
			Description: "Leave \"Require Demonstrating Proof of Possession (DPoP) header in token requests\" enabled. DPoP protects the access token Okta gives Speakeasy so that someone who copies the token cannot use it on its own.",
			Details:     []string{},
			Completed:   dpop,
		},
		ChecklistItem{
			Key:         "grant_scopes",
			Group:       ChecklistGroupConnect,
			Title:       "Allow read-only permissions",
			Description: "On the app's Okta API Scopes tab, grant " + scopes + ". These permissions let Speakeasy read apps, users, and groups without changing them.",
			Details:     []string{},
			Completed:   scopesGranted,
		},
		ChecklistItem{
			Key:         "assign_admin_roles",
			Group:       ChecklistGroupConnect,
			Title:       "Allow access to apps, users, and groups",
			Description: accessDescription,
			Details:     []string{"Speakeasy checks whether it can read apps, users, and groups through the Okta API. It does not check which named admin roles are assigned."},
			Completed:   apiAccess,
		},
		ChecklistItem{
			Key:         "submit_client_id",
			Group:       ChecklistGroupConnect,
			Title:       "Submit the app's client ID",
			Description: "Copy the Client ID (it starts with 0oa) from the app's General tab and enter it here. Speakeasy will check the connection and whether it can read apps, users, and groups.",
			Details:     []string{},
			Completed:   new(signal.ClientIDSubmitted),
		},
		// Verified against the live console 2026-09-19. The wizard is two steps
		// (Profile, then User access and authentication) and creates the agent
		// STAGED; client registration, user assignment and resource connections
		// are sections on the agent page afterwards. Okta's generated client ID
		// for the agent is the agent ID itself, and the agent activates from its
		// credential, not from the Actions menu. The agent key is separate from
		// the management service app key; its JWKS URI ships with AIM-62.
		ChecklistItem{
			Key:         "create_ai_agent",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Register the Speakeasy AI agent",
			Description: "Okta issues Cross App Access assertions only through a registered AI agent, and only for the users assigned to it. Register the agent once, then add a resource connection per MCP server on the Cross App Access tab.",
			Details: []string{
				"Go to Directory > AI Agents and select Register AI agent. Name it Speakeasy; the first step only asks for a name and description.",
				"On User access and authentication, keep Create a new OIDC app linked to this AI agent selected. Okta uses this app only to decide which users the agent may act for; it does not change how your people sign in to Speakeasy. Do not pick Select an existing app: it lists your other apps, such as the MCP servers' own, and the link is permanent.",
				"Okta creates the agent in Staged status and its linked app as Inactive. Open the linked app (under Applications, the Speakeasy app marked Linked AI Agent), set it to Active, and on its Assignments tab assign the users or groups who use MCP servers through Speakeasy.",
				"Leave the agent in Staged status for now. Activating it needs a Public/private key credential under Client registration, and Speakeasy will provide the key URL (JWKS URI) for that credential in an upcoming release.",
				"Record the agent ID below. It is the wlp... value in the agent page URL.",
			},
			Completed: nil,
		},
	)

	return items
}
