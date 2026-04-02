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

// Cursor hook payload
var CursorHookPayload = Type("CursorHookPayload", func() {
	Description("Payload for Cursor hook events")
	Required("hook_event_name")
	Attribute("hook_event_name", String, "The type of hook event (e.g. preToolUse, postToolUse, postToolUseFailure)")
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
})

// Cursor hook result
var CursorHookResult = Type("CursorHookResult", func() {
	Description("Result for Cursor hook events")
	Attribute("permission", String, "Permission decision for preToolUse: allow or deny")
	Attribute("user_message", String, "Message to display to the user")
	Attribute("additional_context", String, "Additional context to inject into the conversation")
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

		Payload(ClaudeHookPayload)
		Result(ClaudeHookResult)
		HTTP(func() {
			POST("/rpc/hooks.claude")
		})
	})

	Method("cursor", func() {
		Description("Endpoint for Cursor hook events. Handles preToolUse, postToolUse, and postToolUseFailure.")

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
