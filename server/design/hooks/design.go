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
		Enum("SessionStart", "PreToolUse", "PostToolUse", "PostToolUseFailure",
			"UserPromptSubmit", "Stop", "SessionEnd", "Notification")
	})
	// Tool-related fields (PreToolUse, PostToolUse, PostToolUseFailure)
	Attribute("tool_name", String, "The name of the tool (for tool-related events)")
	Attribute("tool_use_id", String, "The unique ID for this tool use")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_response", Any, "The response from the tool (PostToolUse only)")
	Attribute("error", Any, "The error from the tool (PostToolUseFailure only)")
	Attribute("is_interrupt", Boolean, "Whether the failure was caused by user interruption (PostToolUseFailure only)")
	// Common fields
	Attribute("session_id", String, "The Claude Code session ID")
	Attribute("cwd", String, "The working directory when the event fired")
	Attribute("transcript_path", String, "Path to the conversation transcript file")
	Attribute("additional_data", MapOf(String, Any), "Additional hook-specific data")
	// SessionStart fields
	Attribute("source", String, "How the session started: startup, resume, clear, compact (SessionStart only)")
	Attribute("model", String, "The model identifier (SessionStart, Stop)")
	// UserPromptSubmit fields
	Attribute("prompt", String, "The user's prompt text (UserPromptSubmit only)")
	// Stop fields
	Attribute("last_assistant_message", String, "Claude's final response text (Stop only)")
	Attribute("stop_hook_active", Boolean, "Whether a stop hook continuation is active (Stop only)")
	// SessionEnd fields
	Attribute("reason", String, "Why the session ended (SessionEnd only)")
	// Notification fields
	Attribute("notification_type", String, "Type of notification: permission_prompt, idle_prompt, auth_success, elicitation_dialog (Notification only)")
	Attribute("message", String, "Notification message text (Notification only)")
	Attribute("title", String, "Notification title (Notification only)")
})

// Unified Claude Code hook result with proper hook response structure
var ClaudeHookResult = Type("ClaudeHookResult", func() {
	Description("Unified result for all Claude Code hook events with proper response structure")
	Attribute("continue", Boolean, "Whether to continue (SessionStart only)")
	Attribute("stopReason", String, "Reason if blocked (SessionStart only)")
	Attribute("suppressOutput", Boolean, "Whether to suppress the hook's output")
	Attribute("hookSpecificOutput", Any, "Hook-specific output as JSON object")
})

// Cursor hook payload
var CursorHookPayload = Type("CursorHookPayload", func() {
	Description("Payload for Cursor hook events")
	Required("hook_event_name")
	Attribute("hook_event_name", String, "The type of hook event (e.g. beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, afterMCPExecution)")
	Attribute("conversation_id", String, "The Cursor conversation ID")
	Attribute("generation_id", String, "The Cursor generation ID")
	Attribute("model", String, "The model being used")
	Attribute("cursor_version", String, "The Cursor IDE version")
	Attribute("user_email", String, "Email of the authenticated Cursor user, if available")
	Attribute("session_id", String, "The session ID from Cursor")
	Attribute("tool_name", String, "The name of the tool")
	Attribute("tool_use_id", String, "The unique ID for this tool use")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_response", Any, "The response from the tool (postToolUse only)")
	Attribute("error", Any, "The error from the tool (postToolUseFailure only)")
	Attribute("is_interrupt", Boolean, "Whether the failure was caused by user interruption")
	Attribute("additional_data", MapOf(String, Any), "Additional hook-specific data")
	// beforeSubmitPrompt fields
	Attribute("prompt", String, "The user's prompt text (beforeSubmitPrompt only)")
	Attribute("composer_mode", String, "The composer mode, e.g. agent (beforeSubmitPrompt only)")
	Attribute("transcript_path", String, "Path to the conversation transcript JSONL file")
	// stop fields
	Attribute("status", String, "Completion status, e.g. completed (stop only)")
	Attribute("loop_count", Int, "Number of agentic loops executed (stop only)")
	Attribute("input_tokens", Int, "Total input tokens used (stop, afterAgentResponse)")
	Attribute("output_tokens", Int, "Total output tokens used (stop, afterAgentResponse)")
	Attribute("cache_read_tokens", Int, "Tokens read from cache (stop, afterAgentResponse)")
	Attribute("cache_write_tokens", Int, "Tokens written to cache (stop, afterAgentResponse)")
	// afterAgentResponse / afterAgentThought fields
	Attribute("text", String, "The assistant's response text (afterAgentResponse) or thinking text (afterAgentThought)")
	Attribute("duration_ms", Int, "Duration in milliseconds for the thinking block (afterAgentThought only)")
	// beforeMCPExecution / afterMCPExecution fields
	Attribute("url", String, "URL of the MCP server (beforeMCPExecution / afterMCPExecution, URL-based servers only)")
	Attribute("command", String, "Command string for command-based MCP servers (beforeMCPExecution / afterMCPExecution only)")
	Attribute("result_json", String, "JSON-encoded string of the MCP tool response (afterMCPExecution only)")
	Attribute("duration", Float64, "Execution duration in milliseconds, excluding approval wait time (afterMCPExecution only)")
})

// Cursor hook result
var CursorHookResult = Type("CursorHookResult", func() {
	Description("Result for Cursor hook events")
	Attribute("permission", String, "Permission decision for preToolUse / beforeMCPExecution: allow, deny, or ask")
	Attribute("user_message", String, "Message to display to the user")
	Attribute("additional_context", String, "Additional context to inject into the conversation")
	Attribute("agent_message", String, "Message sent back to the agent (beforeMCPExecution only)")
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
	Description("Receives hook events from coding assistants for tool usage observability.")

	shared.DeclareErrorResponses()

	Method("claude", func() {
		Description("Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.")

		// Gram-Key + Gram-Project are OPTIONAL on this endpoint during the
		// migration off the OTEL-only attribution flow. When both are set
		// (e.g. by the per-org base plugin's hook script) the handler uses
		// them to attribute hooks directly. When absent, the handler falls
		// back to looking up Redis session metadata seeded by the OTEL
		// /rpc/hooks.otel/v1/logs endpoint. Once all customers move to
		// plugin-based attribution, this method should switch to the same
		// Security() block as Method("cursor").
		Payload(func() {
			Extend(ClaudeHookPayload)
			Attribute("apikey_token", String, "Optional API key for plugin-driven attribution.")
			Attribute("project_slug_input", String, "Optional project slug for plugin-driven attribution.")
		})
		Result(ClaudeHookResult)
		HTTP(func() {
			POST("/rpc/hooks.claude")
			Header("apikey_token:Gram-Key")
			Header("project_slug_input:Gram-Project")
		})
	})

	Method("cursor", func() {
		Description("Endpoint for Cursor hook events. Handles beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, and afterMCPExecution.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			Extend(CursorHookPayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(CursorHookResult)

		HTTP(func() {
			POST("/rpc/hooks.cursor")
			security.ByKeyHeader()
			security.ProjectHeader()
		})
	})

	Method("logs", func() {
		Description("Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
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

	Method("metrics", func() {
		Description("Endpoint to receive OTEL metrics data from Claude Code. Requires API key authentication.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			Extend(OTELMetricsPayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/hooks.otel/v1/metrics")
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
