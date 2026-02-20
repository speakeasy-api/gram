package hooks

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var HookResult = Type("HookResult", func() {
	Required("ok")
	Attribute("ok", Boolean, "Whether the hook was received successfully")
})

var PreToolUsePayload = Type("PreToolUsePayload", func() {
	Required("tool_name")
	Attribute("tool_name", String, "The name of the tool being invoked")
	Attribute("tool_input", Any, "The input to the tool")
})

var PostToolUsePayload = Type("PostToolUsePayload", func() {
	Required("tool_name")
	Attribute("tool_name", String, "The name of the tool that was invoked")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_response", Any, "The response from the tool")
})

var PostToolUseFailurePayload = Type("PostToolUseFailurePayload", func() {
	Required("tool_name")
	Attribute("tool_name", String, "The name of the tool that failed")
	Attribute("tool_input", Any, "The input to the tool")
	Attribute("tool_error", Any, "The error from the tool")
})

var _ = Service("hooks", func() {
	Meta("openapi:generate", "false")
	Description("Receives Claude Code hook events for tool usage observability.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("consumer")
	})

	shared.DeclareErrorResponses()

	Method("preToolUse", func() {
		Description("Called before a tool is executed.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Extend(PreToolUsePayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(HookResult)
		HTTP(func() {
			POST("/rpc/hooks.preToolUse")
			security.ByKeyHeader()
			security.ProjectHeader()
		})
	})

	Method("postToolUse", func() {
		Description("Called after a tool executes successfully.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Extend(PostToolUsePayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(HookResult)
		HTTP(func() {
			POST("/rpc/hooks.postToolUse")
			security.ByKeyHeader()
			security.ProjectHeader()
		})
	})

	Method("postToolUseFailure", func() {
		Description("Called after a tool execution fails.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Payload(func() {
			Extend(PostToolUseFailurePayload)
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(HookResult)
		HTTP(func() {
			POST("/rpc/hooks.postToolUseFailure")
			security.ByKeyHeader()
			security.ProjectHeader()
		})
	})
})
