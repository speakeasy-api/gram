// Code generated from authz.Model by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// Typed requests, one ref per resource kind and one method per permission.
// A handler writes e.Require(ctx, authz.MCPServer(id, project).Write()).
// Kinds whose requests need hand-built dimensions are listed at the end.

// OrgID identifies one org resource.
type OrgID string

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
	return newRefCheck(ScopeOrgRead, string(r.id), r.dimensions(), MatchLoose)
}

// Admin requests ScopeOrgAdmin: Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.
func (r OrgRef) Admin() Check {
	return newRefCheck(ScopeOrgAdmin, string(r.id), r.dimensions(), MatchLoose)
}

// DeviceAgentSync requests ScopeOrgDeviceAgentSync: Let an agent's device agent sync the plugins assigned to it.
func (r OrgRef) DeviceAgentSync() Check {
	return newRefCheck(ScopeOrgDeviceAgentSync, string(r.id), r.dimensions(), MatchLoose)
}

// HooksIngest requests ScopeOrgHooksIngest: Let an agent send AI-tool hook events and telemetry.
func (r OrgRef) HooksIngest(project ProjectID) Check {
	dimensions := r.dimensions()
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	dimensions["project_id"] = string(project)
	return newRefCheck(ScopeOrgHooksIngest, string(r.id), dimensions, MatchLoose)
}

// ProjectID identifies one project resource.
type ProjectID string

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

// Read requests ScopeProjectRead: View projects and project-related resources.
func (r ProjectRef) Read() Check {
	return newRefCheck(ScopeProjectRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeProjectWrite: Create and modify projects and project-related resources.
func (r ProjectRef) Write() Check {
	return newRefCheck(ScopeProjectWrite, string(r.id), r.dimensions(), MatchLoose)
}

// PluginWrite requests ScopePluginWrite: Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.
func (r ProjectRef) PluginWrite() Check {
	return newRefCheck(ScopePluginWrite, string(r.id), r.dimensions(), MatchLoose)
}

// LogRead requests ScopeLogRead: View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.
func (r ProjectRef) LogRead() Check {
	return newRefCheck(ScopeLogRead, string(r.id), r.dimensions(), MatchLoose)
}

// MCPServerID identifies one mcp resource.
type MCPServerID string

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
		"project_id": string(r.project),
	}
}

// Read requests ScopeMCPRead: View MCP servers and their configuration.
func (r MCPServerRef) Read() Check {
	return newRefCheck(ScopeMCPRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeMCPWrite: Create, change and delete MCP servers and their configuration.
func (r MCPServerRef) Write() Check {
	return newRefCheck(ScopeMCPWrite, string(r.id), r.dimensions(), MatchLoose)
}

// Connect requests ScopeMCPConnect: Connect to MCP servers and call their tools.
func (r MCPServerRef) Connect() Check {
	return newRefCheck(ScopeMCPConnect, string(r.id), r.dimensions(), MatchLoose)
}

// EnvironmentID identifies one environment resource.
type EnvironmentID string

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
		"project_id": string(r.project),
	}
}

// Read requests ScopeEnvironmentRead: View environments and their entries within the project, including secret values.
func (r EnvironmentRef) Read() Check {
	return newRefCheck(ScopeEnvironmentRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeEnvironmentWrite: Add, edit, clone, and remove environments within the project.
func (r EnvironmentRef) Write() Check {
	return newRefCheck(ScopeEnvironmentWrite, string(r.id), r.dimensions(), MatchLoose)
}

// SkillID identifies one skill resource.
type SkillID string

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
	return newRefCheck(ScopeSkillRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeSkillWrite: Create and modify skills within the project.
func (r SkillRef) Write() Check {
	return newRefCheck(ScopeSkillWrite, string(r.id), r.dimensions(), MatchLoose)
}

// AssistantID identifies one assistant resource.
type AssistantID string

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
		"project_id": string(r.project),
	}
}

// Read requests ScopeAssistantRead: View and interact with assistants within the project.
func (r AssistantRef) Read() Check {
	return newRefCheck(ScopeAssistantRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeAssistantWrite: Create and modify assistants within the project.
func (r AssistantRef) Write() Check {
	return newRefCheck(ScopeAssistantWrite, string(r.id), r.dimensions(), MatchLoose)
}

// ChatID identifies one chat resource.
type ChatID string

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

// Read requests ScopeChatRead: Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.
func (r ChatRef) Read() Check {
	return newRefCheck(ScopeChatRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeChatWrite: Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.
func (r ChatRef) Write() Check {
	return newRefCheck(ScopeChatWrite, string(r.id), r.dimensions(), MatchLoose)
}

// AgentID identifies one agent resource.
type AgentID string

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
	return newRefCheck(ScopeAgentRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeAgentWrite: Create, configure, and manage agents.
func (r AgentRef) Write() Check {
	return newRefCheck(ScopeAgentWrite, string(r.id), r.dimensions(), MatchLoose)
}

// Authorize requests ScopeAgentAuthorize: Authorize agents and manage their credentials.
func (r AgentRef) Authorize() Check {
	return newRefCheck(ScopeAgentAuthorize, string(r.id), r.dimensions(), MatchLoose)
}

// Transfer requests ScopeAgentTransfer: Hand an agent to another owner. The previous owner loses control of it.
func (r AgentRef) Transfer() Check {
	return newRefCheck(ScopeAgentTransfer, string(r.id), r.dimensions(), MatchLoose)
}

// WorkloadID identifies one workload resource.
type WorkloadID string

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
	return newRefCheck(ScopeWorkloadRead, string(r.id), r.dimensions(), MatchLoose)
}

// Write requests ScopeWorkloadWrite: Register workload issuers, admit and withdraw subjects, and assign agents to workloads.
func (r WorkloadRef) Write() Check {
	return newRefCheck(ScopeWorkloadWrite, string(r.id), r.dimensions(), MatchLoose)
}

// Kinds without a generated ref, and the hand-written constructor each uses:
//
//   - inference: InferenceExecuteCheck: the project is both the resource and its parent
//   - mcp_auth_client: MCPAuthClientCheck: client type, connection owner and upstream client come from the client row
//   - risk_policy: RiskPolicyApplies: evaluation pairs the policy with its bypass exemption
