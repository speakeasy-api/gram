// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// Every scope the model declares, in declaration order. Blocks follow the
// permission they subtract from.
const (
	// ScopeRoot is the engine's own bypass. It is internal and never granted to people.
	ScopeRoot Scope = "root"
	// ScopeOrgRead is "View organization": View organization settings and members.
	ScopeOrgRead Scope = "org:read"
	// ScopeOrgBlockedRead stores exceptions for organization read access. Internal: granted, never checked directly.
	ScopeOrgBlockedRead Scope = "org:blocked_read"
	// ScopeOrgAdmin is "Manage organization": Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.
	ScopeOrgAdmin Scope = "org:admin"
	// ScopeOrgBlockedAdmin stores exceptions for organization admin access. Internal: granted, never checked directly.
	ScopeOrgBlockedAdmin Scope = "org:blocked_admin"
	// ScopeOrgDeviceAgentSync is "Sync device agent plugins": Let an agent's device agent sync the plugins assigned to it.
	ScopeOrgDeviceAgentSync Scope = "org:device_agent_sync"
	// ScopeOrgHooksIngest is "Send hook events": Let an agent send AI-tool hook events and telemetry.
	ScopeOrgHooksIngest Scope = "org:hooks_ingest"
	// ScopeProjectRead is "View projects": View projects and project-related resources.
	ScopeProjectRead Scope = "project:read"
	// ScopeProjectBlockedRead stores exceptions for project read access. Internal: granted, never checked directly.
	ScopeProjectBlockedRead Scope = "project:blocked_read"
	// ScopeProjectWrite is "Manage projects": Create and modify projects and project-related resources.
	ScopeProjectWrite Scope = "project:write"
	// ScopeProjectBlockedWrite stores exceptions for project write access. Internal: granted, never checked directly.
	ScopeProjectBlockedWrite Scope = "project:blocked_write"
	// ScopePluginWrite is "Publish plugins": Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.
	ScopePluginWrite Scope = "plugin:write"
	// ScopePluginBlockedWrite stores exceptions for plugin write access. Internal: granted, never checked directly.
	ScopePluginBlockedWrite Scope = "plugin:blocked_write"
	// ScopeLogRead is "View tool logs": View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.
	ScopeLogRead Scope = "log:read"
	// ScopeMCPRead is "View MCP servers": View MCP servers and their configuration.
	ScopeMCPRead Scope = "mcp:read"
	// ScopeMCPBlockedRead stores exceptions for MCP read access. Internal: granted, never checked directly.
	ScopeMCPBlockedRead Scope = "mcp:blocked_read"
	// ScopeMCPWrite is "Manage MCP servers": Create, change and delete MCP servers and their configuration.
	ScopeMCPWrite Scope = "mcp:write"
	// ScopeMCPBlockedWrite stores exceptions for MCP write access. Internal: granted, never checked directly.
	ScopeMCPBlockedWrite Scope = "mcp:blocked_write"
	// ScopeMCPConnect is "Connect to MCP servers": Connect to MCP servers and call their tools.
	ScopeMCPConnect Scope = "mcp:connect"
	// ScopeMCPBlockedConnect stores exceptions for MCP connect access. Internal: granted, never checked directly.
	ScopeMCPBlockedConnect Scope = "mcp:blocked_connect"
	// ScopeMCPAuth is "Use shared auth clients": Use an upstream auth client to reach MCP servers. Applies on every server that shares the client.
	ScopeMCPAuth Scope = "mcp:auth"
	// ScopeMCPBlockedAuth stores exceptions for auth client use. Internal: granted, never checked directly.
	ScopeMCPBlockedAuth Scope = "mcp:blocked_auth"
	// ScopeEnvironmentRead is "View environments": View environments and their entries within the project, including secret values.
	ScopeEnvironmentRead Scope = "environment:read"
	// ScopeEnvironmentBlockedRead stores exceptions for environment read access. Internal: granted, never checked directly.
	ScopeEnvironmentBlockedRead Scope = "environment:blocked_read"
	// ScopeEnvironmentWrite is "Manage environments": Add, edit, clone, and remove environments within the project.
	ScopeEnvironmentWrite Scope = "environment:write"
	// ScopeEnvironmentBlockedWrite stores exceptions for environment write access. Internal: granted, never checked directly.
	ScopeEnvironmentBlockedWrite Scope = "environment:blocked_write"
	// ScopeSkillRead is "View skills": View skills within the project.
	ScopeSkillRead Scope = "skill:read"
	// ScopeSkillBlockedRead stores exceptions for skill read access. Internal: granted, never checked directly.
	ScopeSkillBlockedRead Scope = "skill:blocked_read"
	// ScopeSkillWrite is "Manage skills": Create and modify skills within the project.
	ScopeSkillWrite Scope = "skill:write"
	// ScopeSkillBlockedWrite stores exceptions for skill write access. Internal: granted, never checked directly.
	ScopeSkillBlockedWrite Scope = "skill:blocked_write"
	// ScopeAssistantRead is "View assistants": View and interact with assistants within the project.
	ScopeAssistantRead Scope = "assistant:read"
	// ScopeAssistantBlockedRead stores exceptions for assistant read access. Internal: granted, never checked directly.
	ScopeAssistantBlockedRead Scope = "assistant:blocked_read"
	// ScopeAssistantWrite is "Manage assistants": Create and modify assistants within the project.
	ScopeAssistantWrite Scope = "assistant:write"
	// ScopeAssistantBlockedWrite stores exceptions for assistant write access. Internal: granted, never checked directly.
	ScopeAssistantBlockedWrite Scope = "assistant:blocked_write"
	// ScopeRiskPolicyEvaluate is "Evaluate risk policies": Put the holder in the named risk policy's audience, so the policy is evaluated against their requests unless a bypass grant for the same policy exempts the request.
	ScopeRiskPolicyEvaluate Scope = "risk_policy:evaluate"
	// ScopeRiskPolicyBypass is "Bypass risk policies": Exempt the holder from the named risk policy for the exact server the grant names, by URL or identity. It only subtracts from a matching evaluate grant and never puts anyone in a policy's audience.
	ScopeRiskPolicyBypass Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	// ScopeRiskPolicyBlock is "Block shadow MCP servers": Block specific shadow MCP servers under allow-by-default risk policies.
	ScopeRiskPolicyBlock Scope = "risk_policy:block"
	// ScopeChatRead is "View agent sessions": Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.
	ScopeChatRead Scope = "chat:read"
	// ScopeChatWrite is "Manage agent sessions": Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.
	ScopeChatWrite Scope = "chat:write"
	// ScopeInferenceExecute is "Run inference": Run model inference without accessing chat transcripts.
	ScopeInferenceExecute Scope = "inference:execute"
	// ScopeAgentRead is "View agents": View agents and their settings.
	ScopeAgentRead Scope = "agent:read"
	// ScopeAgentWrite is "Manage agents": Create, configure, and manage agents.
	ScopeAgentWrite Scope = "agent:write"
	// ScopeAgentAuthorize is "Authorize agent credentials": Authorize agents and manage their credentials.
	ScopeAgentAuthorize Scope = "agent:authorize"
	// ScopeAgentTransfer is "Transfer agent ownership": Hand an agent to another owner. The previous owner loses control of it.
	ScopeAgentTransfer Scope = "agent:transfer"
	// ScopeWorkloadRead is "View workload identities": View workload identity trust policy: issuers, admitted subjects, and assigned agents.
	ScopeWorkloadRead Scope = "workload:read"
	// ScopeWorkloadBlockedRead stores exceptions for workload read access. Internal: granted, never checked directly.
	ScopeWorkloadBlockedRead Scope = "workload:blocked_read"
	// ScopeWorkloadWrite is "Manage workload identities": Register workload issuers, admit and withdraw subjects, and assign agents to workloads.
	ScopeWorkloadWrite Scope = "workload:write"
	// ScopeWorkloadBlockedWrite stores exceptions for workload write access. Internal: granted, never checked directly.
	ScopeWorkloadBlockedWrite Scope = "workload:blocked_write"
)
