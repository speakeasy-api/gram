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
	Attribute("tool_error", Any, "The error from the tool (PostToolUseFailure only)")
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

var _ = Service("hooks", func() {
	Meta("openapi:generate", "false")
	Description("Receives Claude Code hook events for tool usage observability.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("consumer")
	})

	shared.DeclareErrorResponses()

	Method("claude", func() {
		Description("Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Extend(ClaudeHookPayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ClaudeHookResult)
		HTTP(func() {
			POST("/rpc/hooks.claude")
			security.ByKeyHeader()
			security.ProjectHeader()
		})
	})
})
