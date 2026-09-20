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
	// AgentRecorded is true once the administrator saved the AI agent id.
	AgentRecorded bool
	// AgentApp is the agent's linked app as the last applications sync saw it.
	// Nil when no app id is recorded or no sync has completed.
	AgentApp *AgentAppSignal
	// ConnectionRecorded is whether any resource connection is recorded in
	// Speakeasy. Nil when not observed.
	ConnectionRecorded *bool
}

// AgentAppSignal is what the applications sync observed for the recorded
// linked app. Okta exposes no agent API to the service app, so the agent
// itself is never observed.
type AgentAppSignal struct {
	Found    bool
	Active   bool
	Assigned bool
}

// OktaChecklist renders the console steps for a listing mode. jwksURL is
// substituted into the steps that need it; signal ticks the steps a
// verification can observe.
func OktaChecklist(listingMode, jwksURL string, signal ChecklistSignal) []ChecklistItem {
	scopes := strings.Join(RequiredOktaScopes, ", ")
	items := make([]ChecklistItem, 0, 11)

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

	// The agent is only known through the ids the administrator recorded.
	var agentRegistered, agentAppLinked, agentAppReady *bool
	if signal.AgentRecorded {
		agentRegistered = new(true)
	}
	activateDescription := "Okta creates the agent in Staged status and its linked app as Inactive. Open the linked app (under Applications, the Speakeasy Agent app marked Linked AI Agent), set it to Active, and on its Assignments tab assign the users or groups who use MCP servers through Speakeasy. Okta issues assertions only for the users assigned here."
	linkDescription := "On User access and authentication, keep Create a new OIDC app linked to this AI agent selected. Okta uses this app only to decide which users the agent may act for; it does not change how your people sign in to Speakeasy."
	if app := signal.AgentApp; app != nil {
		agentAppLinked = new(app.Found)
		switch {
		case !app.Found:
			linkDescription = "The last applications sync found no app with the bound application ID recorded below. Check the ID, then sync applications again. " + linkDescription
		case app.Active && app.Assigned:
			agentAppReady = new(true)
			activateDescription = "The linked app is Active and has users or groups assigned, as of the last applications sync."
		default:
			agentAppReady = new(false)
			problem := "has no users or groups assigned"
			if !app.Active {
				problem = "is not Active"
			}
			activateDescription = "The last applications sync shows the linked app " + problem + ". Fix it in Okta, then sync applications again. " + activateDescription
		}
	}

	// An unrecorded connection is simply not done yet, not a fault.
	var connectionRecorded *bool
	if signal.ConnectionRecorded != nil && *signal.ConnectionRecorded {
		connectionRecorded = new(true)
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
		// for the agent is the agent ID itself, and the agent turns ACTIVE when its
		// linked app is activated, with no credential. The agent key is separate from
		// the management service app key; its JWKS URI ships with AIM-62.
		ChecklistItem{
			Key:         "register_ai_agent",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Register the Speakeasy AI agent",
			Description: "Okta issues Cross App Access assertions only through a registered AI agent. Go to Directory > AI Agents and select Register AI agent. Name it Speakeasy Agent, so the app Okta creates for it is easy to tell apart from the Speakeasy app above. This first step only asks for a name and description.",
			Details:     []string{},
			Completed:   agentRegistered,
		},
		ChecklistItem{
			Key:         "link_agent_app",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Link a new app to the agent",
			Description: linkDescription,
			Details: []string{
				"Do not pick Select an existing app: it lists your other apps, such as the MCP servers' own, and the link is permanent.",
			},
			Completed: agentAppLinked,
		},
		ChecklistItem{
			Key:         "activate_agent_app",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Activate the linked app and assign users",
			Description: activateDescription,
			Details: []string{
				"Activating the linked app also moves the agent from Staged to Active.",
				"Skip Client registration for now: the agent only needs that credential when it requests access on a user's behalf, and Speakeasy will provide the key URL (JWKS URI) for it in an upcoming release.",
			},
			Completed: agentAppReady,
		},
		ChecklistItem{
			Key:         "record_ai_agent",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Record the agent ID",
			Description: "Enter the agent ID and the linked app's ID below. The agent ID is the wlp... value in the agent page URL; the bound application ID is the linked app's Client ID, which lets Speakeasy check the steps above after the next applications sync.",
			Details:     []string{},
			Completed:   agentRegistered,
		},
		ChecklistItem{
			Key:         "first_resource_connection",
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Set up your first Cross App Access connection",
			Description: "The agent needs one resource connection per MCP server. Open the Cross App Access tab and pick a server: Speakeasy shows the values to copy into Okta, on the agent's Resource connections section, and records the connection once you confirm it. Repeat for each MCP server your agents use.",
			Details: []string{
				"Speakeasy records what you confirm. It does not create the connection in Okta or prove that access works.",
			},
			Completed: connectionRecorded,
		},
	)

	return items
}
