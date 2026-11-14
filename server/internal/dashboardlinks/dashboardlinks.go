// Package dashboardlinks builds links to dashboard pages.
//
// Every dashboard page the server links to, in an email, a Slack message, an
// agent reply or an analytics event, has one builder here. The contract test
// checks each builder against client/dashboard/src/server-links.json, and the
// dashboard checks the same file against its own routes. A route that moves,
// or a wizard step that is renamed, then fails a test instead of leaving a
// dead link behind.
package dashboardlinks

import (
	"net/url"
)

// SelfOrganization is the portable organization segment. The dashboard
// expands "/@self/..." into the viewer's active organization, so a link that
// cannot know the viewer's organization can still reach a page inside it.
const SelfOrganization = "@self"

// DefaultProject is the slug of the project every organization starts with.
const DefaultProject = "default"

// Link is one dashboard page: its path, and the query and fragment the page
// reads.
type Link struct {
	segments []string
	query    url.Values
	fragment string
}

func page(segments ...string) Link {
	return Link{segments: segments, query: nil, fragment: ""}
}

func orgPage(organizationSlug string, segments ...string) Link {
	return page(append([]string{organizationSlug}, segments...)...)
}

func projectPage(organizationSlug, projectSlug string, segments ...string) Link {
	return page(append([]string{organizationSlug, "projects", projectSlug}, segments...)...)
}

// with returns a copy of the link with one query value set. An empty value
// leaves the key out.
func (l Link) with(key, value string) Link {
	if value == "" {
		return l
	}
	query := url.Values{}
	for k, v := range l.query {
		query[k] = append([]string(nil), v...)
	}
	query.Set(key, value)
	l.query = query
	return l
}

func (l Link) withFragment(fragment string) Link {
	l.fragment = fragment
	return l
}

// On returns the link as a URL on the given dashboard origin, or nil when
// there is no origin.
func (l Link) On(site *url.URL) *url.URL {
	if site == nil {
		return nil
	}
	u := site.JoinPath(l.segments...)
	if len(l.query) > 0 {
		u.RawQuery = l.query.Encode()
	}
	u.Fragment = l.fragment
	return u
}

// URL returns the link on the given dashboard origin, or "" when there is no
// origin.
func (l Link) URL(site *url.URL) string {
	u := l.On(site)
	if u == nil {
		return ""
	}
	return u.String()
}

// Path returns the link without an origin, for callers that hand the
// dashboard a path to open on its own host.
func (l Link) Path() string {
	return l.On(&url.URL{Scheme: "", Opaque: "", User: nil, Host: "", Path: "/", RawPath: "", OmitHost: false, ForceQuery: false, RawQuery: "", Fragment: "", RawFragment: ""}).String()
}

// --- Pages outside any organization ---

// Self is the dashboard home of the viewer's active organization.
func Self() Link {
	return page(SelfOrganization)
}

// Block is the page that explains a blocked tool call.
func Block(blockID string) Link {
	return page("blocks", blockID)
}

// SharedSkill is the public page of a shared skill.
func SharedSkill(token string) Link {
	return page("shared", "skills", token)
}

// RiskPolicyChallengeAcknowledge is where a user acknowledges a risk policy
// challenge. The token is a bearer credential, so it travels in the fragment,
// which never leaves the browser.
func RiskPolicyChallengeAcknowledge(ackToken string) Link {
	return page("risk-policy-challenge", "acknowledge").withFragment(url.Values{"ack_token": {ackToken}}.Encode())
}

// RiskPolicyChallengeAcknowledgeShort is RiskPolicyChallengeAcknowledge in
// its shortest form, for surfaces that cut a message off after a line or two.
// The bare ack id is the whole fragment.
func RiskPolicyChallengeAcknowledgeShort(ackID string) Link {
	return page("ack").withFragment(ackID)
}

// RiskPolicyBypassRequest is where a user asks for a risk policy bypass. The
// token travels in the fragment for the same reason as the acknowledge link.
func RiskPolicyBypassRequest(requestToken string) Link {
	return page("risk-policy-bypass", "request").withFragment(url.Values{"request_token": {requestToken}}.Encode())
}

// --- Organization pages ---

// OrganizationHome is the organization's home page.
func OrganizationHome(organizationSlug string) Link {
	return orgPage(organizationSlug)
}

// Billing is the organization's billing page.
func Billing(organizationSlug string) Link {
	return orgPage(organizationSlug, "billing")
}

// Domains is the organization's custom domains page.
func Domains(organizationSlug string) Link {
	return orgPage(organizationSlug, "domains")
}

// Identity is the organization's identity and directory page.
func Identity(organizationSlug string) Link {
	return orgPage(organizationSlug, "identity")
}

// DeviceAgent is the organization's device agent page.
func DeviceAgent(organizationSlug string) Link {
	return orgPage(organizationSlug, "device-agent")
}

// DataExports is the organization's data exports page.
func DataExports(organizationSlug string) Link {
	return orgPage(organizationSlug, "data", "exports")
}

// EventFeed is the organization's event feed page.
func EventFeed(organizationSlug string) Link {
	return orgPage(organizationSlug, "data", "event-feed")
}

// AgentManagement is the organization's agent page. The dashboard sends it
// on to the agents of the organization's project.
func AgentManagement(organizationSlug string) Link {
	return orgPage(organizationSlug, "agent-management")
}

// AccessRoles is the organization's roles page.
func AccessRoles(organizationSlug string) Link {
	return orgPage(organizationSlug, "access", "roles")
}

// AccessGrant is the roles page with the grant dialog open, filled in for a
// user who asked for a scope. The resource and its project are optional.
func AccessGrant(organizationSlug, userID, scope, resourceID, projectID string) Link {
	return AccessRoles(organizationSlug).
		with("grant_user", userID).
		with("scope", scope).
		with("resource_id", resourceID).
		with("project_id", projectID)
}

// AccessChallenges is where an admin resolves denied access challenges.
func AccessChallenges(organizationSlug string) Link {
	return orgPage(organizationSlug, "access", "challenges")
}

// RequestAccess is where a user asks the organization's admins for a scope.
// Every query value is optional.
func RequestAccess(organizationSlug, scope, resourceID, resourceName string) Link {
	return orgPage(organizationSlug, "request-access").
		with("scope", scope).
		with("resource_id", resourceID).
		with("resource_name", resourceName)
}

// --- Project pages ---

// Assistants is the project's assistants list.
func Assistants(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "assistants")
}

// NewAssistant is the project's new assistant page. The disposition is
// optional.
func NewAssistant(organizationSlug, projectSlug, disposition string) Link {
	return projectPage(organizationSlug, projectSlug, "assistants", "new").with("disposition", disposition)
}

// Assistant is one assistant's page.
func Assistant(organizationSlug, projectSlug, assistantID string) Link {
	return projectPage(organizationSlug, projectSlug, "assistants", assistantID)
}

// AssistantThread is one assistant's page open on a conversation.
func AssistantThread(organizationSlug, projectSlug, assistantID, threadID string) Link {
	return Assistant(organizationSlug, projectSlug, assistantID).with("threadId", threadID)
}

// AssistantSessions is one assistant's page open on its sessions tab.
func AssistantSessions(organizationSlug, projectSlug, assistantID string) Link {
	return Assistant(organizationSlug, projectSlug, assistantID).with("tab", "sessions")
}

// Chat is a conversation with the project's assistant.
func Chat(organizationSlug, projectSlug, chatID string) Link {
	return projectPage(organizationSlug, projectSlug, "chat", chatID)
}

// Deployment is one deployment's page.
func Deployment(organizationSlug, projectSlug, deploymentID string) Link {
	return projectPage(organizationSlug, projectSlug, "deployments", deploymentID)
}

// Dashboard is one of the project's custom dashboards.
func Dashboard(organizationSlug, projectSlug, dashboardID string) Link {
	return projectPage(organizationSlug, projectSlug, "dashboards", dashboardID)
}

// SignalsIntelligence is the project's signals page.
func SignalsIntelligence(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "signals-intelligence")
}

// Logs is the project's tool call logs.
func Logs(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "logs")
}

// Plugins is the project's plugins page.
func Plugins(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "plugins")
}

// MCPServer is one MCP server's page. route is the server's dashboard route
// segment (see mcpservers.DashboardRouteParam).
func MCPServer(organizationSlug, projectSlug, route string) Link {
	return projectPage(organizationSlug, projectSlug, "mcp", route)
}

// MCPServerInspect is one MCP server's Inspect page.
func MCPServerInspect(organizationSlug, projectSlug, route string) Link {
	return projectPage(organizationSlug, projectSlug, "mcp", route, "inspect")
}

// MCPServerAuthentication is the Authentication section of one MCP server's
// settings. setupStep names the open setup step the page explains, and is
// optional.
func MCPServerAuthentication(organizationSlug, projectSlug, route, setupStep string) Link {
	return projectPage(organizationSlug, projectSlug, "mcp", route, "settings").
		with("setup", setupStep).
		withFragment("authentication")
}

// MCPGateway is one MCP gateway's page.
func MCPGateway(organizationSlug, projectSlug, gatewayID string) Link {
	return projectPage(organizationSlug, projectSlug, "mcp", "gateway", gatewayID)
}

// ShadowAIMCP is one shadow MCP server's page.
func ShadowAIMCP(organizationSlug, projectSlug, serverSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "shadow-ai", "mcps", serverSlug)
}

// RiskPolicies is the project's risk policies page.
func RiskPolicies(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "risk-policies")
}

// MCPApprovals is the project's risk policies page open on MCP approvals.
func MCPApprovals(organizationSlug, projectSlug string) Link {
	return RiskPolicies(organizationSlug, projectSlug).with("tab", "mcp-approvals")
}

// ToolApprovalRequest is the project's risk policies page open on one tool
// approval request.
func ToolApprovalRequest(organizationSlug, projectSlug, requestID string) Link {
	return RiskPolicies(organizationSlug, projectSlug).
		with("tab", "tool-approvals").
		with("request", requestID)
}

// RiskEvents is the project's risk events page.
func RiskEvents(organizationSlug, projectSlug string) Link {
	return projectPage(organizationSlug, projectSlug, "risk-events")
}
