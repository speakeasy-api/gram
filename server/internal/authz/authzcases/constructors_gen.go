// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authzcases

import "github.com/speakeasy-api/gram/server/internal/authz"

// constructors holds the Go twin of every dashboard check constructor
// method. build takes the constructor's arguments, then the method's.
var constructors = []constructor{
	{name: "org", method: "read", scope: "org:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Org(authz.OrgIDFrom(args[0])).Read() }},
	{name: "org", method: "admin", scope: "org:admin", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Org(authz.OrgIDFrom(args[0])).Admin() }},
	{name: "org", method: "deviceAgentSync", scope: "org:device_agent_sync", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Org(authz.OrgIDFrom(args[0])).DeviceAgentSync() }},
	{name: "org", method: "hooksIngest", scope: "org:hooks_ingest", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.Org(authz.OrgIDFrom(args[0])).HooksIngest(authz.ProjectIDFrom(args[1]))
	}},
	{name: "chat", method: "read", scope: "chat:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Chat(authz.ChatIDFrom(args[0])).Read() }},
	{name: "chat", method: "write", scope: "chat:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Chat(authz.ChatIDFrom(args[0])).Write() }},
	{name: "chats", method: "read", scope: "chat:read", parent: "project", ref: "chat", keys: []string{""}, build: func(args []string) authz.Check { return authz.Chats(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "chats", method: "write", scope: "chat:write", parent: "project", ref: "chat", keys: []string{""}, build: func(args []string) authz.Check { return authz.Chats(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "project", method: "channelsConnect", scope: "channels:connect", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).ChannelsConnect() }},
	{name: "project", method: "channelsAdmin", scope: "channels:admin", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).ChannelsAdmin() }},
	{name: "project", method: "channelsReadAll", scope: "channels:read_all", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).ChannelsReadAll() }},
	{name: "project", method: "read", scope: "project:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "project", method: "write", scope: "project:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "project", method: "pluginWrite", scope: "plugin:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).PluginWrite() }},
	{name: "project", method: "logRead", scope: "log:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).LogRead() }},
	{name: "project", method: "memoryRead", scope: "memory:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).MemoryRead() }},
	{name: "project", method: "memoryWrite", scope: "memory:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Project(authz.ProjectIDFrom(args[0])).MemoryWrite() }},
	{name: "mcpServer", method: "read", scope: "mcp:read", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.MCPServer(authz.MCPServerIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Read()
	}},
	{name: "mcpServer", method: "write", scope: "mcp:write", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.MCPServer(authz.MCPServerIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Write()
	}},
	{name: "mcpServer", method: "connect", scope: "mcp:connect", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.MCPServer(authz.MCPServerIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Connect()
	}},
	{name: "mcpServers", method: "read", scope: "mcp:read", parent: "project", ref: "mcpServer", keys: []string{""}, build: func(args []string) authz.Check { return authz.MCPServers(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "mcpServers", method: "write", scope: "mcp:write", parent: "project", ref: "mcpServer", keys: []string{""}, build: func(args []string) authz.Check { return authz.MCPServers(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "mcpServers", method: "connect", scope: "mcp:connect", parent: "project", ref: "mcpServer", keys: []string{""}, build: func(args []string) authz.Check { return authz.MCPServers(authz.ProjectIDFrom(args[0])).Connect() }},
	{name: "environment", method: "read", scope: "environment:read", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.Environment(authz.EnvironmentIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Read()
	}},
	{name: "environment", method: "write", scope: "environment:write", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.Environment(authz.EnvironmentIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Write()
	}},
	{name: "environments", method: "read", scope: "environment:read", parent: "project", ref: "environment", keys: []string{""}, build: func(args []string) authz.Check { return authz.Environments(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "environments", method: "write", scope: "environment:write", parent: "project", ref: "environment", keys: []string{""}, build: func(args []string) authz.Check { return authz.Environments(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "skill", method: "read", scope: "skill:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Skill(authz.SkillIDFrom(args[0])).Read() }},
	{name: "skill", method: "write", scope: "skill:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Skill(authz.SkillIDFrom(args[0])).Write() }},
	{name: "skills", method: "read", scope: "skill:read", parent: "project", ref: "skill", keys: []string{""}, build: func(args []string) authz.Check { return authz.Skills(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "skills", method: "write", scope: "skill:write", parent: "project", ref: "skill", keys: []string{""}, build: func(args []string) authz.Check { return authz.Skills(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "assistant", method: "read", scope: "assistant:read", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.Assistant(authz.AssistantIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Read()
	}},
	{name: "assistant", method: "write", scope: "assistant:write", parent: "", ref: "", keys: []string{"", "project_id"}, build: func(args []string) authz.Check {
		return authz.Assistant(authz.AssistantIDFrom(args[0]), authz.ProjectIDFrom(args[1])).Write()
	}},
	{name: "assistants", method: "read", scope: "assistant:read", parent: "project", ref: "assistant", keys: []string{""}, build: func(args []string) authz.Check { return authz.Assistants(authz.ProjectIDFrom(args[0])).Read() }},
	{name: "assistants", method: "write", scope: "assistant:write", parent: "project", ref: "assistant", keys: []string{""}, build: func(args []string) authz.Check { return authz.Assistants(authz.ProjectIDFrom(args[0])).Write() }},
	{name: "agent", method: "read", scope: "agent:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agent(authz.AgentIDFrom(args[0])).Read() }},
	{name: "agent", method: "write", scope: "agent:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agent(authz.AgentIDFrom(args[0])).Write() }},
	{name: "agent", method: "authorize", scope: "agent:authorize", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agent(authz.AgentIDFrom(args[0])).Authorize() }},
	{name: "agent", method: "transfer", scope: "agent:transfer", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agent(authz.AgentIDFrom(args[0])).Transfer() }},
	{name: "agents", method: "read", scope: "agent:read", parent: "org", ref: "agent", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agents(authz.OrgIDFrom(args[0])).Read() }},
	{name: "agents", method: "write", scope: "agent:write", parent: "org", ref: "agent", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agents(authz.OrgIDFrom(args[0])).Write() }},
	{name: "agents", method: "authorize", scope: "agent:authorize", parent: "org", ref: "agent", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agents(authz.OrgIDFrom(args[0])).Authorize() }},
	{name: "agents", method: "transfer", scope: "agent:transfer", parent: "org", ref: "agent", keys: []string{""}, build: func(args []string) authz.Check { return authz.Agents(authz.OrgIDFrom(args[0])).Transfer() }},
	{name: "workload", method: "read", scope: "workload:read", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Workload(authz.WorkloadIDFrom(args[0])).Read() }},
	{name: "workload", method: "write", scope: "workload:write", parent: "", ref: "", keys: []string{""}, build: func(args []string) authz.Check { return authz.Workload(authz.WorkloadIDFrom(args[0])).Write() }},
	{name: "workloads", method: "read", scope: "workload:read", parent: "org", ref: "workload", keys: []string{""}, build: func(args []string) authz.Check { return authz.Workloads(authz.OrgIDFrom(args[0])).Read() }},
	{name: "workloads", method: "write", scope: "workload:write", parent: "org", ref: "workload", keys: []string{""}, build: func(args []string) authz.Check { return authz.Workloads(authz.OrgIDFrom(args[0])).Write() }},
}
