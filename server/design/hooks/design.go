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

// OTEL attribute value supporting string and int types
var OTELAttributeValue = Type("OTELAttributeValue", func() {
	Description("OTEL attribute value - supports stringValue or intValue")
	Attribute("stringValue", String, "String value")
	Attribute("intValue", Int64, "Integer value")
})

// OTEL attribute with key-value pair
var OTELAttribute = Type("OTELAttribute", func() {
	Description("OTEL log attribute with key and typed value")
	Required("key", "value")
	Attribute("key", String, "Attribute key")
	Attribute("value", OTELAttributeValue, "Attribute value")
})

// OTEL log body
var OTELLogBody = Type("OTELLogBody", func() {
	Description("OTEL log body")
	Attribute("stringValue", String, "String body value")
})

// OTEL log record
var OTELLogRecord = Type("OTELLogRecord", func() {
	Description("Individual OTEL log record")
	Required("timeUnixNano", "observedTimeUnixNano", "body", "attributes")
	Attribute("timeUnixNano", String, "Timestamp in nanoseconds since Unix epoch")
	Attribute("observedTimeUnixNano", String, "Observed timestamp in nanoseconds")
	Attribute("body", OTELLogBody, "Log body content")
	Attribute("attributes", ArrayOf(OTELAttribute), "Log attributes")
	Attribute("droppedAttributesCount", Int, "Number of dropped attributes")
})

// OTEL scope
var OTELScope = Type("OTELScope", func() {
	Description("OTEL instrumentation scope")
	Attribute("name", String, "Scope name")
	Attribute("version", String, "Scope version")
})

// OTEL scope logs
var OTELScopeLog = Type("OTELScopeLog", func() {
	Description("OTEL scope logs container")
	Required("logRecords")
	Attribute("scope", OTELScope, "Instrumentation scope information")
	Attribute("logRecords", ArrayOf(OTELLogRecord), "Array of log records")
})

// OTEL resource attribute
var OTELResourceAttribute = Type("OTELResourceAttribute", func() {
	Description("OTEL resource attribute")
	Required("key", "value")
	Attribute("key", String, "Resource attribute key")
	Attribute("value", OTELAttributeValue, "Resource attribute value")
})

// OTEL resource
var OTELResource = Type("OTELResource", func() {
	Description("OTEL resource information")
	Attribute("attributes", ArrayOf(OTELResourceAttribute), "Resource attributes")
	Attribute("droppedAttributesCount", Int, "Number of dropped attributes")
})

// OTEL resource logs
var OTELResourceLog = Type("OTELResourceLog", func() {
	Description("OTEL resource logs container")
	Required("scopeLogs")
	Attribute("resource", OTELResource, "Resource information")
	Attribute("scopeLogs", ArrayOf(OTELScopeLog), "Array of scope logs")
})

// OTEL logs payload
var OTELLogsPayload = Type("OTELLogsPayload", func() {
	Description("OTEL logs export payload")
	Required("resourceLogs")
	Attribute("resourceLogs", ArrayOf(OTELResourceLog), "Array of resource logs")
})

var _ = Service("hooks", func() {
	Meta("openapi:generate", "false")
	Description("Receives Claude Code hook events for tool usage observability.")

	shared.DeclareErrorResponses()

	Method("claude", func() {
		Description("Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.")

		Payload(func() {
			Extend(ClaudeHookPayload)
		})
		Result(ClaudeHookResult)
		HTTP(func() {
			POST("/rpc/hooks.claude")
		})
	})

	Method("logs", func() {
		Description("Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
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
