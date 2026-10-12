package identityproviderconnections

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// Listing modes select the client authentication a new connection provisions.
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

// Checklist keys are the stable step identifiers the dashboard switches on.
const (
	ChecklistKeyAddOINApp               = "add_oin_app"
	ChecklistKeyCreateAPIServicesApp    = "create_api_services_app"
	ChecklistKeyPublicKeyAuth           = "public_key_auth"
	ChecklistKeyDPoP                    = "dpop"
	ChecklistKeyGrantScopes             = "grant_scopes"
	ChecklistKeyAssignAdminRoles        = "assign_admin_roles"
	ChecklistKeySubmitClientID          = "submit_client_id"
	ChecklistKeyRegisterAIAgent         = "register_ai_agent"
	ChecklistKeyLinkAgentApp            = "link_agent_app"
	ChecklistKeyActivateAgentApp        = "activate_agent_app"
	ChecklistKeyRecordAIAgent           = "record_ai_agent"
	ChecklistKeyAddAgentPublicKey       = "add_agent_public_key"
	ChecklistKeyFirstResourceConnection = "first_resource_connection"
)

// RequiredOktaScopes are the Okta API scopes the connection verifies and the
// sync relies on.
var RequiredOktaScopes = []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}

// RequiredOktaAdminRoles are the admin roles the checklist asks operators to
// assign to the service app.
var RequiredOktaAdminRoles = []string{"Application Administrator (org-wide)", "Read-only Administrator"}

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
	// Agent is what Speakeasy's own records say about the agent steps.
	Agent AgentObservation
}

// AgentObservation is read from the database rather than from verification.
type AgentObservation struct {
	// App is the agent's linked app as the last applications sync saw it.
	// Nil when no app id is recorded or no sync has completed.
	App *AgentAppSignal
	// ConnectionRecorded is whether any resource connection is recorded.
	ConnectionRecorded bool
}

// AgentAppSignal is what the applications sync observed for the recorded
// linked app. Okta exposes no agent API to the service app, so the agent
// itself is never observed.
type AgentAppSignal struct {
	Found    bool
	Active   bool
	Assigned bool
}

const (
	accessInstruction   = "On the app's Admin roles tab, assign these roles. Speakeasy checks whether it can read apps, users, and groups through the Okta API; it does not check which named admin roles are assigned. Okta will ask you to confirm your identity with multi-factor authentication first."
	linkInstruction     = "On User access and authentication, keep Allow users to access this agent checked and Create a new OIDC app linked to this AI agent selected, then click Next. You can skip the Add owners step that follows. The linked app becomes the app your people use to sign in to Speakeasy, which is what lets Okta turn that sign-in into Cross App Access assertions."
	activateInstruction = "Okta creates the agent in Staged status and its linked app as Inactive. Open the linked app (linked from the agent's User access section), set it to Active, and on its Assignments tab assign the users or groups who use MCP servers through Speakeasy. Okta issues assertions only for the users assigned here. People who sign in through Okta must already exist in Speakeasy, provisioned through directory sync (SCIM); Speakeasy does not create accounts at sign-in."
)

// completions holds each observable step's tri-state; nil is not observed.
type completions struct {
	appCreated      *bool
	keyAuth         *bool
	dpop            *bool
	scopesGranted   *bool
	apiAccess       *bool
	clientID        *bool
	agentRecorded   *bool
	agentAppLinked  *bool
	agentAppReady   *bool
	firstConnection *bool
}

func observe(signal ChecklistSignal) completions {
	done := completions{
		appCreated:      nil,
		keyAuth:         nil,
		dpop:            nil,
		scopesGranted:   nil,
		apiAccess:       nil,
		clientID:        new(signal.ClientIDSubmitted),
		agentRecorded:   nil,
		agentAppLinked:  nil,
		agentAppReady:   nil,
		firstConnection: nil,
	}

	if signal.Checked {
		done.scopesGranted = new(len(signal.MissingScopes) == 0)
	}
	// Scope verification can complete without a token. Only report token
	// protection and key authentication when token issuance is evidenced.
	if signal.Checked && !slices.Contains(signal.Reasons, ReasonKeyNotFetched) && !slices.Contains(signal.Reasons, ReasonSecretRejected) {
		// A granted scope or a bound token proves credential acceptance.
		if signal.DPoPBound || len(signal.MissingScopes) < len(RequiredOktaScopes) {
			done.appCreated = new(true)
			done.keyAuth = new(true)
			done.dpop = new(signal.DPoPBound)
		}
		for _, reason := range signal.Reasons {
			switch reason {
			case ReasonMissingRole, ReasonReadFailedApps, ReasonReadFailedUsers, ReasonReadFailedGroup:
				done.apiAccess = new(false)
			}
		}
		// Missing scopes skip their reads, so absence of a failure alone is
		// not proof of access. An observed failure still takes precedence.
		if done.apiAccess == nil && len(signal.MissingScopes) == 0 && !slices.Contains(signal.Reasons, ReasonMissingScope) {
			done.apiAccess = new(true)
		}
	}

	// Okta exposes no agent API, so a recorded id stands in for registration.
	if signal.AgentRecorded {
		done.agentRecorded = new(true)
	}
	if app := signal.Agent.App; app != nil {
		done.agentAppLinked = new(app.Found)
		if app.Found {
			done.agentAppReady = new(app.Active && app.Assigned)
		}
	}
	// An unrecorded connection is simply not done yet, not a fault.
	if signal.Agent.ConnectionRecorded {
		done.firstConnection = new(true)
	}
	return done
}

func accessDescription(apiAccess *bool) string {
	if apiAccess != nil && *apiAccess {
		return "Speakeasy can read apps, users, and groups"
	}
	return accessInstruction
}

func accessDetails(apiAccess *bool) []string {
	if apiAccess != nil && *apiAccess {
		return []string{}
	}
	return slices.Clone(RequiredOktaAdminRoles)
}

func linkDescription(app *AgentAppSignal) string {
	if app != nil && !app.Found {
		return "The last applications sync found no app with the bound application ID recorded below. Check the ID, then sync applications again. " + linkInstruction
	}
	return linkInstruction
}

func activateDescription(app *AgentAppSignal) string {
	switch {
	case app == nil || !app.Found:
		return activateInstruction
	case app.Active && app.Assigned:
		return "The linked app is Active and has users or groups assigned, as of the last applications sync."
	case !app.Active:
		return "The last applications sync shows the linked app is not Active. Fix it in Okta, then sync applications again. " + activateInstruction
	default:
		return "The last applications sync shows the linked app has no users or groups assigned. Fix it in Okta, then sync applications again. " + activateInstruction
	}
}

// OktaChecklist renders the console steps for the connection's client
// authentication; signal ticks the steps a verification can observe.
func OktaChecklist(authMethod remotesessions.TokenEndpointAuthMethod, signal ChecklistSignal) []ChecklistItem {
	done := observe(signal)
	return slices.Concat(connectItems(authMethod, done), agentItems(signal.Agent.App, done))
}

func appItem(completed *bool) ChecklistItem {
	return ChecklistItem{
		Key:         ChecklistKeyCreateAPIServicesApp,
		Group:       ChecklistGroupConnect,
		Title:       "Create an app for Speakeasy",
		Description: "In the Okta Admin Console, open Applications and Resources > Applications and choose Create App Integration. Select API Services, name the app Speakeasy, choose Use Okta-generated client ID, and save it.",
		Details: []string{
			"Do not choose Client ID Metadata Document (CIMD). Configure public-key authentication on the saved app, in the next step.",
		},
		Completed: completed,
	}
}

// Verified against the live console 2026-10-05: Okta greys out Public key /
// Private key under Client Credentials until a key URL is saved on Public keys.
func connectItems(authMethod remotesessions.TokenEndpointAuthMethod, done completions) []ChecklistItem {
	if authMethod == remotesessions.TokenEndpointAuthMethodBasic {
		return oinConnectItems(done)
	}
	return []ChecklistItem{
		appItem(done.appCreated),
		{
			Key:         ChecklistKeyPublicKeyAuth,
			Group:       ChecklistGroupConnect,
			Title:       "Let Okta recognize Speakeasy",
			Description: "Add Speakeasy's key URL so Okta can check that connection requests come from Speakeasy. You do not need to create or share a password.",
			Details: []string{
				"On the newly created Speakeasy app's General tab, find Public keys and click Edit.",
				"Choose Use a URL to fetch keys dynamically, enter the key URL below, and save.",
				"Then, in Client Credentials, click Edit, set Client authentication to Public key / Private key, and save. Okta enables that option only once a key URL is saved; refresh the page if it is still grayed out.",
			},
			Completed: done.keyAuth,
		},
		{
			Key:         ChecklistKeyDPoP,
			Group:       ChecklistGroupConnect,
			Title:       "Keep token protection (DPoP) enabled",
			Description: "Under General Settings, leave \"Require Demonstrating Proof of Possession (DPoP) header in token requests\" enabled. DPoP protects the access token Okta gives Speakeasy so that someone who copies the token cannot use it on its own.",
			Details:     []string{},
			Completed:   done.dpop,
		},
		{
			Key:         ChecklistKeyGrantScopes,
			Group:       ChecklistGroupConnect,
			Title:       "Allow read-only permissions",
			Description: "On the app's Okta API Scopes tab, grant these permissions. They let Speakeasy read apps, users, and groups without changing them.",
			Details:     slices.Clone(RequiredOktaScopes),
			Completed:   done.scopesGranted,
		},
		{
			Key:         ChecklistKeyAssignAdminRoles,
			Group:       ChecklistGroupConnect,
			Title:       "Allow access to apps, users, and groups",
			Description: accessDescription(done.apiAccess),
			Details:     accessDetails(done.apiAccess),
			Completed:   done.apiAccess,
		},
		{
			Key:         ChecklistKeySubmitClientID,
			Group:       ChecklistGroupConnect,
			Title:       "Submit the app's client ID",
			Description: "Copy the Client ID (it starts with 0oa) from the app's General tab and enter it here. Speakeasy will check the connection and whether it can read apps, users, and groups.",
			Details:     []string{},
			Completed:   done.clientID,
		},
	}
}

// oinConnectItems covers an install from the Okta Integration Network, which
// authenticates with a client secret, so there is no key URL or DPoP step.
func oinConnectItems(done completions) []ChecklistItem {
	return []ChecklistItem{
		{
			Key:         ChecklistKeyAddOINApp,
			Group:       ChecklistGroupConnect,
			Title:       "Install Speakeasy from the Okta Integration Network",
			Description: "In the Okta Admin Console go to Applications > API Service Integrations, choose Add Integration, select Speakeasy, review the read-only permissions, and choose Install & Authorize.",
			Details: []string{
				"Okta shows the client secret only once. Copy the client ID and client secret before leaving the page.",
			},
			Completed: done.appCreated,
		},
		{
			Key:         ChecklistKeyGrantScopes,
			Group:       ChecklistGroupConnect,
			Title:       "Allow read-only permissions",
			Description: "Okta asks you to approve these read-only permissions during installation. They let Speakeasy read apps, users, and groups without changing them.",
			Details:     slices.Clone(RequiredOktaScopes),
			Completed:   done.scopesGranted,
		},
		{
			Key:         ChecklistKeyAssignAdminRoles,
			Group:       ChecklistGroupConnect,
			Title:       "Allow access to apps, users, and groups",
			Description: accessDescription(done.apiAccess),
			Details:     accessDetails(done.apiAccess),
			Completed:   done.apiAccess,
		},
		{
			Key:         ChecklistKeySubmitClientID,
			Group:       ChecklistGroupConnect,
			Title:       "Enter the integration's credentials",
			Description: "Enter the client ID (it starts with 0oa) and client secret from the Speakeasy integration here. Speakeasy will check the connection and whether it can read apps, users, and groups.",
			Details:     []string{},
			Completed:   done.clientID,
		},
	}
}

func agentItems(app *AgentAppSignal, done completions) []ChecklistItem {
	return []ChecklistItem{
		// Verified against the live console 2026-10-05. The wizard creates the
		// agent STAGED on its second step; it turns ACTIVE when its linked app is
		// activated. The linked app's Client ID shows the agent ID (wlp...); its
		// own ID (0oa...) is only in its page URL. Okta only exchanges ID tokens
		// minted for the requesting client, so the linked app is also Speakeasy's
		// trusted sign-in client. The agent key is separate from the management
		// service app key.
		{
			Key:         ChecklistKeyRegisterAIAgent,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Register the Speakeasy AI agent",
			Description: "Okta issues Cross App Access assertions only through a registered AI agent. Go to Directory > AI Agents and select Register AI agent > Register manually. Name it Speakeasy Agent, so the app Okta creates for it is easy to tell apart from the Speakeasy app above. This first step only asks for a name and description.",
			Details:     []string{},
			Completed:   done.agentRecorded,
		},
		{
			Key:         ChecklistKeyLinkAgentApp,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Link a new app to the agent",
			Description: linkDescription(app),
			Details: []string{
				"Do not pick Select an existing app: it lists your other apps, such as the MCP servers' own, and the link is permanent.",
			},
			Completed: done.agentAppLinked,
		},
		{
			Key:         ChecklistKeyRecordAIAgent,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Record the agent ID",
			Description: "Enter the agent ID and the linked app's ID below. The agent ID is the wlp... value in the agent page URL; the bound application ID is the 0oa... value in the linked app's page URL (not its Client ID, which Okta sets to the agent ID), which lets Speakeasy check the linked app after the next applications sync.",
			Details: []string{
				"This step is done once the agent ID is recorded. Speakeasy does not check the Okta settings in the next steps; the Okta sign-in section shows what Speakeasy has set up on its side.",
			},
			Completed: done.agentRecorded,
		},
		{
			Key:         ChecklistKeyAddAgentPublicKey,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Add the agent's public key",
			Description: "Use Set up Okta sign-in on this page. It registers the linked app, whose Client ID is the agent ID, as the organization's sign-in client and shows its public key. In Okta, open the agent's Credentials (Client registration), paste this key in the agent's Credentials and click Activate.",
			Details: []string{
				"Okta rejects edits to the linked app, including activating it, until the agent has an active public key, so do this before the next step.",
				"Okta stores the pasted key, not a key URL. Rotating the signing key set or publishing a new key in it means pasting the new public key in Okta again.",
				"Set up Okta sign-in needs customer-managed encryption keys enabled for your organization and a Google Cloud KMS key to sign with.",
			},
			Completed: nil,
		},
		{
			Key:         ChecklistKeyActivateAgentApp,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Activate the linked app and assign users",
			Description: activateDescription(app),
			Details: []string{
				"Activating the linked app also moves the agent from Staged to Active.",
				"Owners and Machine access can stay unset.",
				"On the linked app's General tab, enable the Authorization Code and Refresh Token grant types and add the sign-in redirect URI shown in the Okta sign-in section.",
			},
			Completed: done.agentAppReady,
		},
		{
			Key:         ChecklistKeyFirstResourceConnection,
			Group:       ChecklistGroupCrossAppAccess,
			Title:       "Set up your first Cross App Access connection",
			Description: "The agent needs one resource connection per MCP server. Open the Cross App Access tab and pick a server: Speakeasy shows the values to copy into Okta, on the agent's Resource connections section, and records the connection once you confirm it. Repeat for each MCP server your agents use.",
			Details: []string{
				"First enable Cross App Access on the app for that service: under Applications, open the app, go to Machine Assignments > Callers > Cross-app access (XAA) > Edit, and set it to Enabled. For Issuer URL, enter the server's authorization server issuer shown on the Cross App Access tab, and add the scopes your agents use; Audience/tenant ID can stay empty.",
				"Then on the agent choose Add resource connection > Application > App configured for AI Agent access and pick that app, listed with an XAA suffix (for example, Linear - XAA). Okta lists only apps with Cross App Access enabled.",
				"Speakeasy records what you confirm. It does not create the connection in Okta or prove that access works.",
				"Each vendor must also enable enterprise-managed authorization in its own admin console and trust your Okta issuer. For example, Linear needs SAML via Okta and \"MCP enterprise managed authentication\" turned on with your Okta issuer. Many vendors require an Enterprise or SSO plan, and some only allow specific clients. Speakeasy cannot do this step for you.",
			},
			Completed: done.firstConnection,
		},
	}
}
