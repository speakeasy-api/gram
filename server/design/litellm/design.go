package litellm

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var RequestData = Type("LiteLLMRequestData", func() {
	Description("Sanitized metadata bound to the LiteLLM virtual key and request.")
	Attribute("user_api_key_hash", String)
	Attribute("user_api_key_alias", String)
	Attribute("user_api_key_user_id", String)
	Attribute("user_api_key_user_email", String)
	Attribute("user_api_key_team_id", String)
	Attribute("user_api_key_team_alias", String)
	Attribute("user_api_key_end_user_id", String)
	Attribute("user_api_key_org_id", String)
})

var StructuredMessage = Type("LiteLLMStructuredMessage", func() {
	Description("A message supplied to LiteLLM for the guarded model request.")
	Attribute("role", String)
	Attribute("content", Any)
	Required("role")
})

var GuardrailAction = Type("LiteLLMGuardrailAction", String, func() {
	Enum("BLOCKED", "NONE", "GUARDRAIL_INTERVENED")
	Meta("openapi:extension:x-speakeasy-name-override", "LiteLLMGuardrailAction")
})

var IngestResult = ResultType("application/vnd.litellm.ingest-result", func() {
	Description("LiteLLM Generic Guardrail decision.")
	Attribute("action", GuardrailAction)
	Attribute("blocked_reason", String)
	Attribute("texts", ArrayOf(String))
	Attribute("images", ArrayOf(String))
	Attribute("tools", ArrayOf(Any))
	Attribute("stream_holdback_chars", ArrayOf(Int))
	Required("action")
})

var _ = Service("litellm", func() {
	Description("Receives LiteLLM Generic Guardrail callbacks.")
	shared.DeclareErrorResponses()

	Method("ingest", func() {
		Description("Evaluates and captures a LiteLLM model request before it reaches the provider.")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("input_type", String, func() {
				Enum("request", "response")
			})
			Attribute("request_data", RequestData)
			Attribute("texts", ArrayOf(String))
			Attribute("images", ArrayOf(String))
			Attribute("tools", ArrayOf(Any))
			Attribute("tool_calls", ArrayOf(Any))
			Attribute("structured_messages", ArrayOf(StructuredMessage))
			Attribute("request_headers", MapOf(String, String))
			Attribute("litellm_call_id", String)
			Attribute("litellm_trace_id", String)
			Attribute("litellm_version", String)
			Attribute("model", String)
			Attribute("additional_provider_specific_params", MapOf(String, Any))
			Required("input_type", "request_data")
		})

		Result(IngestResult)

		HTTP(func() {
			// LiteLLM appends this fixed Generic Guardrail suffix to its configured endpoint.
			POST("/rpc/litellm.ingest/beta/litellm_basic_guardrail_api") //nolint:glint // LiteLLM requires its published Generic Guardrail route
			security.ByKeyHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "ingestLiteLLMGuardrail")
		Meta("openapi:extension:x-speakeasy-name-override", "ingestGuardrail")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})
})
