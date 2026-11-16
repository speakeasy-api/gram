// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// Typed requests, one ref per resource kind and one method per permission.
// A handler writes e.Require(ctx, authz.MCPServer(id, project).Write()), with
// id and project built by the id types in ids_gen.go.
// Kinds whose requests need hand-built dimensions are listed at the end.

// OrgRef names one org resource for a request.
type OrgRef struct {
	id OrgID
}

// Org names the org resource id.
func Org(id OrgID) OrgRef {
	return OrgRef{id: id}
}

func (r OrgRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeOrgRead: View organization settings and members.
func (r OrgRef) Read() Check {
	return newRefCheck(ScopeOrgRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Admin requests ScopeOrgAdmin: Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.
func (r OrgRef) Admin() Check {
	return newRefCheck(ScopeOrgAdmin, r.id.String(), r.dimensions(), MatchStandard)
}

// DeviceAgentSync requests ScopeOrgDeviceAgentSync: Let an agent's device agent sync the plugins assigned to it.
func (r OrgRef) DeviceAgentSync() Check {
	return newRefCheck(ScopeOrgDeviceAgentSync, r.id.String(), r.dimensions(), MatchStandard)
}

// HooksIngest requests ScopeOrgHooksIngest: Let an agent send AI-tool hook events and telemetry.
func (r OrgRef) HooksIngest(project ProjectID) Check {
	dimensions := r.dimensions()
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	dimensions["project_id"] = project.String()
	return newRefCheck(ScopeOrgHooksIngest, r.id.String(), dimensions, MatchStandard)
}

// ChatRef names one chat resource for a request.
type ChatRef struct {
	id ChatID
}

// Chat names the chat resource id.
func Chat(id ChatID) ChatRef {
	return ChatRef{id: id}
}

func (r ChatRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeChatRead: Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions and those of agents they own or edit.
func (r ChatRef) Read() Check {
	return newRefCheck(ScopeChatRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeChatWrite: Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own sessions and those of agents they own or edit.
func (r ChatRef) Write() Check {
	return newRefCheck(ScopeChatWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// ProjectRef names one project resource for a request.
type ProjectRef struct {
	id ProjectID
}

// Project names the project resource id.
func Project(id ProjectID) ProjectRef {
	return ProjectRef{id: id}
}

func (r ProjectRef) dimensions() map[string]string {
	return nil
}

// ChannelsConnect requests ScopeChannelsConnect: Connect to Channels and exchange messages as an authorized instance.
func (r ProjectRef) ChannelsConnect() Check {
	return newRefCheck(ScopeChannelsConnect, r.id.String(), r.dimensions(), MatchStandard)
}

// ChannelsAdmin requests ScopeChannelsAdmin: Manage Channels settings and member access without reading other members' messages.
func (r ProjectRef) ChannelsAdmin() Check {
	return newRefCheck(ScopeChannelsAdmin, r.id.String(), r.dimensions(), MatchStandard)
}

// ChannelsReadAll requests ScopeChannelsReadAll: Read other members' private Channels conversations in the project.
func (r ProjectRef) ChannelsReadAll() Check {
	return newRefCheck(ScopeChannelsReadAll, r.id.String(), r.dimensions(), MatchStandard)
}

// Read requests ScopeProjectRead: View projects and project-related resources.
func (r ProjectRef) Read() Check {
	return newRefCheck(ScopeProjectRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeProjectWrite: Create and modify projects and project-related resources.
func (r ProjectRef) Write() Check {
	return newRefCheck(ScopeProjectWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// PluginWrite requests ScopePluginWrite: Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.
func (r ProjectRef) PluginWrite() Check {
	return newRefCheck(ScopePluginWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// LogRead requests ScopeLogRead: View every member's tool-call logs and traces in the project, including tool arguments, results, and prompt text. Everyone keeps the logs of their own sessions and of agents they own. Not implied by project or organization admin access.
func (r ProjectRef) LogRead() Check {
	return newRefCheck(ScopeLogRead, r.id.String(), r.dimensions(), MatchStandard)
}

// MemoryRead requests ScopeMemoryRead: Search and recall the project's team memory: masked session summaries, memos and digests from every member. The built-in Admin role holds it by default; other roles need an explicit grant.
func (r ProjectRef) MemoryRead() Check {
	return newRefCheck(ScopeMemoryRead, r.id.String(), r.dimensions(), MatchStandard)
}

// MemoryWrite requests ScopeMemoryWrite: Save memos to the project's team memory, attributed to you.
func (r ProjectRef) MemoryWrite() Check {
	return newRefCheck(ScopeMemoryWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// MCPServerRef names one mcp resource for a request.
type MCPServerRef struct {
	id      MCPServerID
	project ProjectID
}

// MCPServer names the mcp resource id, in project project.
func MCPServer(id MCPServerID, project ProjectID) MCPServerRef {
	return MCPServerRef{id: id, project: project}
}

func (r MCPServerRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project.String(),
	}
}

// Read requests ScopeMCPRead: View MCP servers and their configuration.
func (r MCPServerRef) Read() Check {
	return newRefCheck(ScopeMCPRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeMCPWrite: Create, change and delete MCP servers and their configuration.
func (r MCPServerRef) Write() Check {
	return newRefCheck(ScopeMCPWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// Connect requests ScopeMCPConnect: Connect to MCP servers and call their tools.
func (r MCPServerRef) Connect() Check {
	return newRefCheck(ScopeMCPConnect, r.id.String(), r.dimensions(), MatchStandard).asConnection()
}

// ToolCall requests ScopeMCPConnect with tool and disposition. An empty value is left out of the
// check, except an empty disposition, which is carried as unannotated.
func (r MCPServerRef) ToolCall(tool, disposition string) Check {
	dimensions := r.dimensions()
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	if tool != "" {
		dimensions["tool"] = tool
	}
	if disposition != "" {
		dimensions["disposition"] = disposition
	}
	dimensions["disposition"] = toolCallDisposition(dimensions["disposition"])
	return newRefCheck(ScopeMCPConnect, r.id.String(), dimensions, MatchStandard).asToolCall()
}

// EnvironmentRef names one environment resource for a request.
type EnvironmentRef struct {
	id      EnvironmentID
	project ProjectID
}

// Environment names the environment resource id, in project project.
func Environment(id EnvironmentID, project ProjectID) EnvironmentRef {
	return EnvironmentRef{id: id, project: project}
}

func (r EnvironmentRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project.String(),
	}
}

// Read requests ScopeEnvironmentRead: View environments and their entries within the project, including secret values.
func (r EnvironmentRef) Read() Check {
	return newRefCheck(ScopeEnvironmentRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeEnvironmentWrite: Add, edit, clone, and remove environments within the project.
func (r EnvironmentRef) Write() Check {
	return newRefCheck(ScopeEnvironmentWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// SkillRef names one skill resource for a request.
type SkillRef struct {
	id SkillID
}

// Skill names the skill resource id.
func Skill(id SkillID) SkillRef {
	return SkillRef{id: id}
}

func (r SkillRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeSkillRead: View skills within the project.
func (r SkillRef) Read() Check {
	return newRefCheck(ScopeSkillRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeSkillWrite: Create and modify skills within the project.
func (r SkillRef) Write() Check {
	return newRefCheck(ScopeSkillWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// AssistantRef names one assistant resource for a request.
type AssistantRef struct {
	id      AssistantID
	project ProjectID
}

// Assistant names the assistant resource id, in project project.
func Assistant(id AssistantID, project ProjectID) AssistantRef {
	return AssistantRef{id: id, project: project}
}

func (r AssistantRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project.String(),
	}
}

// Read requests ScopeAssistantRead: View and interact with assistants within the project.
func (r AssistantRef) Read() Check {
	return newRefCheck(ScopeAssistantRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeAssistantWrite: Create and modify assistants within the project.
func (r AssistantRef) Write() Check {
	return newRefCheck(ScopeAssistantWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// AgentRef names one agent resource for a request.
type AgentRef struct {
	id AgentID
}

// Agent names the agent resource id.
func Agent(id AgentID) AgentRef {
	return AgentRef{id: id}
}

func (r AgentRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeAgentRead: View agents and their settings.
func (r AgentRef) Read() Check {
	return newRefCheck(ScopeAgentRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeAgentWrite: Create, configure, and manage agents.
func (r AgentRef) Write() Check {
	return newRefCheck(ScopeAgentWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// Authorize requests ScopeAgentAuthorize: Authorize agents and manage their credentials.
func (r AgentRef) Authorize() Check {
	return newRefCheck(ScopeAgentAuthorize, r.id.String(), r.dimensions(), MatchStandard)
}

// Transfer requests ScopeAgentTransfer: Hand an agent to another owner. The previous owner loses control of it.
func (r AgentRef) Transfer() Check {
	return newRefCheck(ScopeAgentTransfer, r.id.String(), r.dimensions(), MatchStandard)
}

// WorkloadRef names one workload resource for a request.
type WorkloadRef struct {
	id WorkloadID
}

// Workload names the workload resource id.
func Workload(id WorkloadID) WorkloadRef {
	return WorkloadRef{id: id}
}

func (r WorkloadRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeWorkloadRead: View workload identity trust policy: issuers, admitted subjects, and assigned agents.
func (r WorkloadRef) Read() Check {
	return newRefCheck(ScopeWorkloadRead, r.id.String(), r.dimensions(), MatchStandard)
}

// Write requests ScopeWorkloadWrite: Register workload issuers, admit and withdraw subjects, and assign agents to workloads.
func (r WorkloadRef) Write() Check {
	return newRefCheck(ScopeWorkloadWrite, r.id.String(), r.dimensions(), MatchStandard)
}

// Collections name every resource of one kind inside a project, for requests
// with no single resource: listing, creating, or a lookup that found nothing.
// Each is the kind's permission with the project id as the resource.

// MCPServerCollection names every mcp resource of one project.
type MCPServerCollection struct{ project ProjectID }

// MCPServers names every mcp resource of project.
func MCPServers(project ProjectID) MCPServerCollection {
	return MCPServerCollection{project: project}
}

// Read requests ScopeMCPRead on every mcp resource of the project.
func (c MCPServerCollection) Read() Check {
	return MCPServer(MCPServerID{id: c.project.id}, c.project).Read()
}

// Write requests ScopeMCPWrite on every mcp resource of the project.
func (c MCPServerCollection) Write() Check {
	return MCPServer(MCPServerID{id: c.project.id}, c.project).Write()
}

// Connect requests ScopeMCPConnect on every mcp resource of the project.
func (c MCPServerCollection) Connect() Check {
	return MCPServer(MCPServerID{id: c.project.id}, c.project).Connect()
}

// EnvironmentCollection names every environment resource of one project.
type EnvironmentCollection struct{ project ProjectID }

// Environments names every environment resource of project.
func Environments(project ProjectID) EnvironmentCollection {
	return EnvironmentCollection{project: project}
}

// Read requests ScopeEnvironmentRead on every environment resource of the project.
func (c EnvironmentCollection) Read() Check {
	return Environment(EnvironmentID{id: c.project.id}, c.project).Read()
}

// Write requests ScopeEnvironmentWrite on every environment resource of the project.
func (c EnvironmentCollection) Write() Check {
	return Environment(EnvironmentID{id: c.project.id}, c.project).Write()
}

// AssistantCollection names every assistant resource of one project.
type AssistantCollection struct{ project ProjectID }

// Assistants names every assistant resource of project.
func Assistants(project ProjectID) AssistantCollection {
	return AssistantCollection{project: project}
}

// Read requests ScopeAssistantRead on every assistant resource of the project.
func (c AssistantCollection) Read() Check {
	return Assistant(AssistantID{id: c.project.id}, c.project).Read()
}

// Write requests ScopeAssistantWrite on every assistant resource of the project.
func (c AssistantCollection) Write() Check {
	return Assistant(AssistantID{id: c.project.id}, c.project).Write()
}

// Kinds without a generated ref, and the hand-written constructor each uses:
//
//   - inference: InferenceExecuteCheck: the project is both the resource and its parent
//   - mcp_auth_client: MCPAuthClientCheck: client type, connection owner and upstream client come from the client row
//   - risk_policy: RiskPolicyApplies: evaluation pairs the policy with its bypass exemption
