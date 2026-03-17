package hooks

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// Unified Claude Code hook payload
var ClaudeHookPayload = Type("ClaudeHookPayload", func() {
	Description("Unified payload for all Claude Code hook events")
	Required("hook_event_name")
	Attribute("hook_event_name", String, "The type of hook event", func() {
		Enum("SessionStart", "PreToolUse", "PostToolUse", "PostToolUseFailure")
	})
	Attribute("tool_name", String, "The name of the tool (for tool-related events)")
	Attribute("tool_use_id", String, "The unique ID for this tool use")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_response", Any, "The response from the tool (PostToolUse only)")
	Attribute("error", Any, "The error from the tool (PostToolUseFailure only)")
	Attribute("is_interrupt", Boolean, "Whether the failure was caused by user interruption (PostToolUseFailure only)")
	Attribute("session_id", String, "The Claude Code session ID")
	Attribute("additional_data", MapOf(String, Any), "Additional hook-specific data")
})

// Unified Claude Code hook result with proper hook response structure
var ClaudeHookResult = Type("ClaudeHookResult", func() {
	Description("Unified result for all Claude Code hook events with proper response structure")
	Attribute("continue", Boolean, "Whether to continue (SessionStart only)")
	Attribute("stopReason", String, "Reason if blocked (SessionStart only)")
	Attribute("hookSpecificOutput", Any, "Hook-specific output as JSON object")
})

// Unified Cursor hook payload
var CursorHookPayload = Type("CursorHookPayload", func() {
	Description("Unified payload for all Cursor hook events")
	Required("hook_event_name")
	Attribute("hook_event_name", String, "The type of hook event", func() {
		Enum(
			"sessionStart", "sessionEnd",
			"preToolUse", "postToolUse", "postToolUseFailure",
			"subagentStart", "subagentStop",
			"beforeShellExecution", "afterShellExecution",
			"beforeMCPExecution", "afterMCPExecution",
			"beforeReadFile", "afterFileEdit",
			"beforeSubmitPrompt", "preCompact", "stop",
			"afterAgentResponse", "afterAgentThought",
			"beforeTabFileRead", "afterTabFileEdit",
		)
	})
	Attribute("conversation_id", String, "Cursor conversation ID")
	Attribute("generation_id", String, "Cursor generation ID")
	Attribute("model", String, "Model being used")
	Attribute("cursor_version", String, "Cursor version")
	Attribute("workspace_roots", ArrayOf(String), "Workspace root paths")
	Attribute("user_email", String, "User email")
	Attribute("transcript_path", String, "Path to conversation transcript")

	// Tool-related fields
	Attribute("tool_name", String, "The name of the tool (for tool-related events)")
	Attribute("tool_use_id", String, "The unique ID for this tool use")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_response", Any, "The response from the tool (postToolUse only)")
	Attribute("error", Any, "The error from the tool (postToolUseFailure only)")

	// Shell execution fields
	Attribute("command", String, "Shell command being executed")
	Attribute("cwd", String, "Current working directory")

	// File operation fields
	Attribute("file_path", String, "Path to file being accessed or edited")
	Attribute("file_content", String, "Content of the file")

	// MCP execution fields
	Attribute("mcp_server", String, "MCP server name")
	Attribute("mcp_tool", String, "MCP tool name")

	// Subagent fields
	Attribute("subagent_id", String, "Subagent identifier")
	Attribute("subagent_prompt", String, "Prompt given to subagent")

	// Response/thought fields
	Attribute("response_text", String, "Agent response text")
	Attribute("thought_text", String, "Agent thought/reasoning text")

	// Additional hook-specific data
	Attribute("additional_data", MapOf(String, Any), "Additional hook-specific data")
})

// Unified Cursor hook result
var CursorHookResult = Type("CursorHookResult", func() {
	Description("Unified result for all Cursor hook events")
	// Permission-based response
	Attribute("permission", String, "Permission decision: allow, deny, or ask", func() {
		Enum("allow", "deny", "ask")
	})
	Attribute("user_message", String, "Optional message to display to user")
	Attribute("agent_message", String, "Optional message to send to agent")

	// Environment and context
	Attribute("env", MapOf(String, String), "Environment variables to inject")
	Attribute("additional_context", String, "Additional context for the agent")

	// Loop continuation
	Attribute("followup_message", String, "Auto-submit this message (stop/subagentStop hooks)")
})

// Server name override types
var ServerNameOverride = Type("ServerNameOverride", func() {
	Description("User-defined display name for a hooks server")
	Required("id", "raw_server_name", "display_name")
	Attribute("id", String, "Override ID")
	Attribute("raw_server_name", String, "Original server name from hooks")
	Attribute("display_name", String, "User-friendly display name")
})

var _ = Service("hooks", func() {
	Description("Receives Claude Code hook events for tool usage observability.")

	shared.DeclareErrorResponses()

	Method("claude", func() {
		Description("Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.")

		Payload(ClaudeHookPayload)
		Result(ClaudeHookResult)
		HTTP(func() {
			POST("/rpc/hooks.claude")
		})
	})

	Method("cursor", func() {
		Description("Unified endpoint for all Cursor hook events. Handles all Cursor hook types including sessionStart, preToolUse, postToolUse, and many others.")

		Payload(CursorHookPayload)
		Result(CursorHookResult)
		HTTP(func() {
			POST("/rpc/hooks.cursor")
		})
	})

	Method("logs", func() {
		Description("Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks") // NOTE: This is the ONLY endpoint that should allow the hooks scope
		})

		Payload(func() {
			Extend(OTELLogsPayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/hooks.otel/v1/logs")
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusAccepted)
		})
	})
})

var _ = Service("hooksServerNames", func() {
	Description("Manages display name overrides for hooks servers.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})

	shared.DeclareErrorResponses()

	Method("list", func() {
		Description("List all server name display overrides for a project")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ArrayOf(ServerNameOverride))

		HTTP(func() {
			GET("/rpc/hooks.listServerNameOverrides")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listServerNameOverrides")
	})

	Method("upsert", func() {
		Description("Create or update a server name display override")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("raw_server_name", String, "Original server name from hooks")
			Attribute("display_name", String, "User-friendly display name")
			Required("raw_server_name", "display_name")
		})

		Result(ServerNameOverride)

		HTTP(func() {
			POST("/rpc/hooks.upsertServerNameOverride")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "upsertServerNameOverride")
	})

	Method("delete", func() {
		Description("Delete a server name display override")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("override_id", String, "Override ID to delete")
			Required("override_id")
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/hooks.deleteServerNameOverride")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "deleteServerNameOverride")
	})
})
