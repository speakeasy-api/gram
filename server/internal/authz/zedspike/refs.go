package zedspike

// Check is one permission on one resource, built only by the typed methods
// below. Its fields are unexported, so code outside this package cannot build
// a check that names a permission the schema does not have, or pass context a
// caveat does not read.
type Check struct {
	resourceType string
	resourceID   string
	permission   string
	// projectID fills the project edge, from the reference.
	projectID string
	// context fills the caveats' check parameters. Typed methods always pass
	// every parameter the permission's caveats read.
	context map[string]any
	// facts are relationships taken from the checked row, such as a chat's
	// author, rather than from grants.
	facts []Relationship
}

// String names the check for errors and logs: mcp_server:<id>#connect.
func (c Check) String() string { return c.resourceType + ":" + c.resourceID + "#" + c.permission }

func check(resourceType, resourceID, permission string) Check {
	return Check{resourceType: resourceType, resourceID: resourceID, permission: permission}
}

func (c Check) inProject(projectID string) Check {
	c.projectID = projectID
	return c
}

func (c Check) with(context map[string]any) Check {
	c.context = context
	return c
}

// OrganizationRef names the caller's organization.
type OrganizationRef struct{ id string }

// Organization names an organization. It must be the caller's: every
// organization edge in the schema is the caller's organization.
func Organization(organizationID string) OrganizationRef { return OrganizationRef{id: organizationID} }

func (o OrganizationRef) Read() Check       { return check("organization", o.id, "read") }
func (o OrganizationRef) Administer() Check { return check("organization", o.id, "administer") }
func (o OrganizationRef) SyncDeviceAgent() Check {
	return check("organization", o.id, "sync_device_agent")
}

// IngestHooks always names the project, so a grant bounded to another project
// never matches.
func (o OrganizationRef) IngestHooks(projectID string) Check {
	return check("organization", o.id, "ingest_hooks").with(map[string]any{"project_id": projectID})
}

// ProjectRef names one project of the caller's organization.
type ProjectRef struct{ organizationID, id string }

func Project(organizationID, projectID string) ProjectRef {
	return ProjectRef{organizationID: organizationID, id: projectID}
}

func (p ProjectRef) Read() Check          { return check("project", p.id, "read") }
func (p ProjectRef) Write() Check         { return check("project", p.id, "write") }
func (p ProjectRef) ReadToolLogs() Check  { return check("project", p.id, "read_logs") }
func (p ProjectRef) PublishPlugin() Check { return check("project", p.id, "plugin_publication") }
func (p ProjectRef) ReadOrOrganizationRead() Check {
	return check("project", p.id, "read_or_organization_read")
}
func (p ProjectRef) WriteOrAdminister() Check { return check("project", p.id, "write_or_administer") }

// Collections: every resource of a kind in the project, for listing and
// creating. These replace the *InProject references that passed the project
// ID as a server, environment or assistant ID.
func (p ProjectRef) ReadMCPServers() Check    { return check("project", p.id, "read_mcp_servers") }
func (p ProjectRef) CreateMCPServer() Check   { return check("project", p.id, "create_mcp_server") }
func (p ProjectRef) ReadEnvironments() Check  { return check("project", p.id, "read_environments") }
func (p ProjectRef) CreateEnvironment() Check { return check("project", p.id, "create_environment") }
func (p ProjectRef) ReadSkills() Check        { return check("project", p.id, "read_skills") }
func (p ProjectRef) WriteSkills() Check       { return check("project", p.id, "write_skills") }
func (p ProjectRef) ReadAssistants() Check    { return check("project", p.id, "read_assistants") }
func (p ProjectRef) CreateAssistant() Check   { return check("project", p.id, "create_assistant") }
func (p ProjectRef) ReadChats() Check         { return check("project", p.id, "read_chats") }

// MCPServerRef names one MCP server. The project must be the server's own,
// taken from the loaded row: a project-level grant on the request's project
// would otherwise match a server in another project.
type MCPServerRef struct{ projectID, id string }

func MCPServer(projectID, serverID string) MCPServerRef {
	return MCPServerRef{projectID: projectID, id: serverID}
}

func (m MCPServerRef) Read() Check { return m.check("read", "", "") }
func (m MCPServerRef) Edit() Check { return m.check("edit", "", "") }

// Connect opens the server. Grants and blocks limited to tools do not decide
// it; each tool call is checked with CallTool.
func (m MCPServerRef) Connect() Check { return m.check("connect", "", "") }

// CallTool calls one tool. disposition may be empty when the tool's
// annotations yield none.
func (m MCPServerRef) CallTool(tool, disposition string) Check {
	return m.check("connect", tool, disposition)
}

// ReadProjectOrEdit passes with project read or server edit.
func (m MCPServerRef) ReadProjectOrEdit() Check { return m.check("project_read_or_edit", "", "") }

func (m MCPServerRef) check(permission, tool, disposition string) Check {
	return check("mcp_server", m.id, permission).inProject(m.projectID).
		with(map[string]any{"tool": tool, "disposition": disposition})
}

// MCPAuthClientRef names one upstream auth client and the connection asked for.
type MCPAuthClientRef struct {
	id    string
	attrs AuthClientAttrs
}

// AuthClientAttrs describe the connection: every field a grant may name.
type AuthClientAttrs struct {
	ClientType      string
	ConnectionOwner string
	ProjectID       string
	UpstreamClient  string
}

func MCPAuthClient(clientID string, attrs AuthClientAttrs) MCPAuthClientRef {
	return MCPAuthClientRef{id: clientID, attrs: attrs}
}

func (a MCPAuthClientRef) Use() Check {
	return check("mcp_auth_client", a.id, "use").with(map[string]any{
		"client_type":      a.attrs.ClientType,
		"connection_owner": a.attrs.ConnectionOwner,
		"project_id":       a.attrs.ProjectID,
		"upstream_client":  a.attrs.UpstreamClient,
		"represented_user": "", // filled by the client from the caller
	})
}

// EnvironmentRef, SkillRef and AssistantRef name one resource in its project.
type (
	EnvironmentRef struct{ projectID, id string }
	SkillRef       struct{ projectID, id string }
	AssistantRef   struct{ projectID, id string }
)

func Environment(projectID, id string) EnvironmentRef { return EnvironmentRef{projectID, id} }
func Skill(projectID, id string) SkillRef             { return SkillRef{projectID, id} }
func Assistant(projectID, id string) AssistantRef     { return AssistantRef{projectID, id} }

func (e EnvironmentRef) Read() Check {
	return check("environment", e.id, "read").inProject(e.projectID)
}
func (e EnvironmentRef) Write() Check {
	return check("environment", e.id, "write").inProject(e.projectID)
}
func (s SkillRef) Read() Check      { return check("skill", s.id, "read").inProject(s.projectID) }
func (s SkillRef) Write() Check     { return check("skill", s.id, "write").inProject(s.projectID) }
func (a AssistantRef) Read() Check  { return check("assistant", a.id, "read").inProject(a.projectID) }
func (a AssistantRef) Write() Check { return check("assistant", a.id, "write").inProject(a.projectID) }

// InferenceRef names inference in one project.
type InferenceRef struct{ projectID string }

func Inference(projectID string) InferenceRef { return InferenceRef{projectID: projectID} }

func (i InferenceRef) Execute() Check {
	return check("inference", i.projectID, "execute").inProject(i.projectID)
}

// ChatRef names one chat session. Reading or changing it needs its owner, so
// the only way to a check is WithOwner: a handler cannot forget the facts.
type ChatRef struct{ projectID, id string }

func Chat(projectID, chatID string) ChatRef { return ChatRef{projectID: projectID, id: chatID} }

// ChatOwner is who started the chat, from the loaded row.
type ChatOwner struct {
	UserID         string // chats.user_id
	ExternalUserID string // chats.external_user_id
	Agent          *AgentOwner
}

// AgentOwner is the agent that ran the chat and its current owner.
type AgentOwner struct {
	AgentID                   string // chats.agent_id
	OwnerUserID               string // agents.owner_user_id
	Deleted                   bool
	OwnerReassignmentRequired bool
}

// OwnedChatRef is a chat with its owner facts.
type OwnedChatRef struct {
	chat  ChatRef
	owner ChatOwner
}

func (c ChatRef) WithOwner(owner ChatOwner) OwnedChatRef { return OwnedChatRef{chat: c, owner: owner} }

func (c OwnedChatRef) Read() Check  { return c.check("read") }
func (c OwnedChatRef) Write() Check { return c.check("write") }

func (c OwnedChatRef) check(permission string) Check {
	out := check("chat", c.chat.id, permission).inProject(c.chat.projectID)
	if c.owner.UserID != "" {
		out.facts = append(out.facts, Relationship{
			ResourceType: "chat", ResourceID: c.chat.id, Relation: "author", SubjectType: "user", SubjectID: c.owner.UserID,
		})
	}
	if agent := c.owner.Agent; agent != nil {
		out.facts = append(out.facts,
			Relationship{
				ResourceType: "chat", ResourceID: c.chat.id, Relation: "agent", SubjectType: "agent", SubjectID: agent.AgentID,
				Caveat:        "agent_only_chat",
				CaveatContext: map[string]any{"user_id": c.owner.UserID, "external_user_id": c.owner.ExternalUserID},
			},
			Relationship{
				ResourceType: "agent", ResourceID: agent.AgentID, Relation: "owner", SubjectType: "user", SubjectID: agent.OwnerUserID,
				Caveat:        "agent_live",
				CaveatContext: map[string]any{"deleted": agent.Deleted, "owner_reassignment_required": agent.OwnerReassignmentRequired},
			},
		)
	}
	return out
}

// RiskPolicyRef names one risk policy and the server a request reached.
type RiskPolicyRef struct {
	id                        string
	serverURL, serverIdentity string
}

func RiskPolicy(policyID, serverURL, serverIdentity string) RiskPolicyRef {
	return RiskPolicyRef{id: policyID, serverURL: serverURL, serverIdentity: serverIdentity}
}

func (r RiskPolicyRef) Evaluate() Check { return r.check("evaluate") }
func (r RiskPolicyRef) Bypass() Check   { return r.check("bypass") }
func (r RiskPolicyRef) Block() Check    { return r.check("block") }

func (r RiskPolicyRef) check(permission string) Check {
	return check("risk_policy", r.id, permission).
		with(map[string]any{"server_url": r.serverURL, "server_identity": r.serverIdentity})
}

// typedChecks lists one check per typed method. The registry test fails when
// any names a permission the schema lacks or a building block (has_*,
// blocked_*), and when a public permission has no typed method.
func typedChecks() []Check {
	org := Organization("o")
	project := Project("o", "p")
	server := MCPServer("p", "s")
	chat := Chat("p", "c").WithOwner(ChatOwner{})
	risk := RiskPolicy("r", "", "")
	return []Check{
		org.Read(), org.Administer(), org.SyncDeviceAgent(), org.IngestHooks("p"),
		project.Read(), project.Write(), project.ReadToolLogs(), project.PublishPlugin(),
		project.ReadOrOrganizationRead(), project.WriteOrAdminister(),
		project.ReadMCPServers(), project.CreateMCPServer(), project.ReadEnvironments(), project.CreateEnvironment(),
		project.ReadSkills(), project.WriteSkills(), project.ReadAssistants(), project.CreateAssistant(), project.ReadChats(),
		server.Read(), server.Edit(), server.Connect(), server.CallTool("t", "d"), server.ReadProjectOrEdit(),
		MCPAuthClient("a", AuthClientAttrs{}).Use(),
		Environment("p", "e").Read(), Environment("p", "e").Write(),
		Skill("p", "k").Read(), Skill("p", "k").Write(),
		Assistant("p", "a").Read(), Assistant("p", "a").Write(),
		Inference("p").Execute(),
		chat.Read(), chat.Write(),
		risk.Evaluate(), risk.Bypass(), risk.Block(),
	}
}
