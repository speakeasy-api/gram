// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// Typed requests, one ref per resource kind and one method per permission.
// A handler writes e.Require(ctx, authz.MCPServer(id, project).Write()).
// Kinds whose requests need hand-built dimensions are listed at the end.

// OrgRef names one org resource for a request.
type OrgRef struct {
	id string
}

// Org names the org resource id.
func Org[I ID](id I) OrgRef {
	return OrgRef{id: idString(id)}
}

func (r OrgRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeOrgRead: View organization settings and members.
func (r OrgRef) Read() Check {
	return newRefCheck(ScopeOrgRead, r.id, r.dimensions(), MatchLoose)
}

// Admin requests ScopeOrgAdmin: Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.
func (r OrgRef) Admin() Check {
	return newRefCheck(ScopeOrgAdmin, r.id, r.dimensions(), MatchLoose)
}

// DeviceAgentSync requests ScopeOrgDeviceAgentSync: Let an agent's device agent sync the plugins assigned to it.
func (r OrgRef) DeviceAgentSync() Check {
	return newRefCheck(ScopeOrgDeviceAgentSync, r.id, r.dimensions(), MatchLoose)
}

// HooksIngest requests ScopeOrgHooksIngest: Let an agent send AI-tool hook events and telemetry.
func (r OrgRef) HooksIngest(project string) Check {
	dimensions := r.dimensions()
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	dimensions["project_id"] = project
	return newRefCheck(ScopeOrgHooksIngest, r.id, dimensions, MatchLoose)
}

// ProjectRef names one project resource for a request.
type ProjectRef struct {
	id string
}

// Project names the project resource id.
func Project[I ID](id I) ProjectRef {
	return ProjectRef{id: idString(id)}
}

func (r ProjectRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeProjectRead: View projects and project-related resources.
func (r ProjectRef) Read() Check {
	return newRefCheck(ScopeProjectRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeProjectWrite: Create and modify projects and project-related resources.
func (r ProjectRef) Write() Check {
	return newRefCheck(ScopeProjectWrite, r.id, r.dimensions(), MatchLoose)
}

// PluginWrite requests ScopePluginWrite: Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.
func (r ProjectRef) PluginWrite() Check {
	return newRefCheck(ScopePluginWrite, r.id, r.dimensions(), MatchLoose)
}

// LogRead requests ScopeLogRead: View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.
func (r ProjectRef) LogRead() Check {
	return newRefCheck(ScopeLogRead, r.id, r.dimensions(), MatchLoose)
}

// MCPServerRef names one mcp resource for a request.
type MCPServerRef struct {
	id      string
	project string
}

// MCPServer names the mcp resource id, in project project.
func MCPServer[I, P ID](id I, project P) MCPServerRef {
	return MCPServerRef{id: idString(id), project: idString(project)}
}

func (r MCPServerRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project,
	}
}

// Read requests ScopeMCPRead: View MCP servers and their configuration.
func (r MCPServerRef) Read() Check {
	return newRefCheck(ScopeMCPRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeMCPWrite: Create, change and delete MCP servers and their configuration.
func (r MCPServerRef) Write() Check {
	return newRefCheck(ScopeMCPWrite, r.id, r.dimensions(), MatchLoose)
}

// Connect requests ScopeMCPConnect: Connect to MCP servers and call their tools.
func (r MCPServerRef) Connect() Check {
	return newRefCheck(ScopeMCPConnect, r.id, r.dimensions(), MatchLoose)
}

// ToolCall requests ScopeMCPConnect with tool and disposition. An empty value is sent as unknown, so a
// grant limited on that key never allows it and a block on it applies.
func (r MCPServerRef) ToolCall(tool, disposition string) Check {
	dimensions := r.dimensions()
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	dimensions["tool"] = valueOrUnknown(tool)
	dimensions["disposition"] = valueOrUnknown(disposition)
	return newRefCheck(ScopeMCPConnect, r.id, dimensions, MatchLoose)
}

// EnvironmentRef names one environment resource for a request.
type EnvironmentRef struct {
	id      string
	project string
}

// Environment names the environment resource id, in project project.
func Environment[I, P ID](id I, project P) EnvironmentRef {
	return EnvironmentRef{id: idString(id), project: idString(project)}
}

func (r EnvironmentRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project,
	}
}

// Read requests ScopeEnvironmentRead: View environments and their entries within the project, including secret values.
func (r EnvironmentRef) Read() Check {
	return newRefCheck(ScopeEnvironmentRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeEnvironmentWrite: Add, edit, clone, and remove environments within the project.
func (r EnvironmentRef) Write() Check {
	return newRefCheck(ScopeEnvironmentWrite, r.id, r.dimensions(), MatchLoose)
}

// SkillRef names one skill resource for a request.
type SkillRef struct {
	id string
}

// Skill names the skill resource id.
func Skill[I ID](id I) SkillRef {
	return SkillRef{id: idString(id)}
}

func (r SkillRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeSkillRead: View skills within the project.
func (r SkillRef) Read() Check {
	return newRefCheck(ScopeSkillRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeSkillWrite: Create and modify skills within the project.
func (r SkillRef) Write() Check {
	return newRefCheck(ScopeSkillWrite, r.id, r.dimensions(), MatchLoose)
}

// AssistantRef names one assistant resource for a request.
type AssistantRef struct {
	id      string
	project string
}

// Assistant names the assistant resource id, in project project.
func Assistant[I, P ID](id I, project P) AssistantRef {
	return AssistantRef{id: idString(id), project: idString(project)}
}

func (r AssistantRef) dimensions() map[string]string {
	return map[string]string{
		"project_id": r.project,
	}
}

// Read requests ScopeAssistantRead: View and interact with assistants within the project.
func (r AssistantRef) Read() Check {
	return newRefCheck(ScopeAssistantRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeAssistantWrite: Create and modify assistants within the project.
func (r AssistantRef) Write() Check {
	return newRefCheck(ScopeAssistantWrite, r.id, r.dimensions(), MatchLoose)
}

// ChatRef names one chat resource for a request.
type ChatRef struct {
	id string
}

// Chat names the chat resource id.
func Chat[I ID](id I) ChatRef {
	return ChatRef{id: idString(id)}
}

func (r ChatRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeChatRead: Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.
func (r ChatRef) Read() Check {
	return newRefCheck(ScopeChatRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeChatWrite: Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.
func (r ChatRef) Write() Check {
	return newRefCheck(ScopeChatWrite, r.id, r.dimensions(), MatchLoose)
}

// AgentRef names one agent resource for a request.
type AgentRef struct {
	id string
}

// Agent names the agent resource id.
func Agent[I ID](id I) AgentRef {
	return AgentRef{id: idString(id)}
}

func (r AgentRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeAgentRead: View agents and their settings.
func (r AgentRef) Read() Check {
	return newRefCheck(ScopeAgentRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeAgentWrite: Create, configure, and manage agents.
func (r AgentRef) Write() Check {
	return newRefCheck(ScopeAgentWrite, r.id, r.dimensions(), MatchLoose)
}

// Authorize requests ScopeAgentAuthorize: Authorize agents and manage their credentials.
func (r AgentRef) Authorize() Check {
	return newRefCheck(ScopeAgentAuthorize, r.id, r.dimensions(), MatchLoose)
}

// Transfer requests ScopeAgentTransfer: Hand an agent to another owner. The previous owner loses control of it.
func (r AgentRef) Transfer() Check {
	return newRefCheck(ScopeAgentTransfer, r.id, r.dimensions(), MatchLoose)
}

// WorkloadRef names one workload resource for a request.
type WorkloadRef struct {
	id string
}

// Workload names the workload resource id.
func Workload[I ID](id I) WorkloadRef {
	return WorkloadRef{id: idString(id)}
}

func (r WorkloadRef) dimensions() map[string]string {
	return nil
}

// Read requests ScopeWorkloadRead: View workload identity trust policy: issuers, admitted subjects, and assigned agents.
func (r WorkloadRef) Read() Check {
	return newRefCheck(ScopeWorkloadRead, r.id, r.dimensions(), MatchLoose)
}

// Write requests ScopeWorkloadWrite: Register workload issuers, admit and withdraw subjects, and assign agents to workloads.
func (r WorkloadRef) Write() Check {
	return newRefCheck(ScopeWorkloadWrite, r.id, r.dimensions(), MatchLoose)
}

// Kinds without a generated ref, and the hand-written constructor each uses:
//
//   - inference: InferenceExecuteCheck: the project is both the resource and its parent
//   - mcp_auth_client: MCPAuthClientCheck: client type, connection owner and upstream client come from the client row
//   - risk_policy: RiskPolicyApplies: evaluation pairs the policy with its bypass exemption
