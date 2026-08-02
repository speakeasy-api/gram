package litellm

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	Description("Receives LiteLLM Generic Guardrail callbacks and OpenTelemetry exports.")
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

	Method("traces", func() {
		Meta("openapi:generate", "false")
		Description("Accepts LiteLLM OTLP trace exports. Send the standard OTLP JSON ExportTraceServiceRequest shape shown here with application/json, or the binary opentelemetry.proto.collector.trace.v1.ExportTraceServiceRequest with application/x-protobuf or application/protobuf. Content-Encoding may be gzip.")
		Error(string(oops.CodeRequestTooLarge), func() { Description(oops.CodeRequestTooLarge.UserMessage()) })
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("resourceSpans", ArrayOf(Any), "Standard OTLP ResourceSpans objects. OTLP integer fields use their canonical decimal-string JSON representation and trace/span IDs use case-insensitive hexadecimal strings.")
			Required("resourceSpans")
		})

		Result(Empty)

		HTTP(func() {
			// Served on the canonical hooks.otel base so every OTLP signal shares
			// one customer-facing endpoint; provider semantics resolve from the
			// API key, not the route.
			POST("/rpc/hooks.otel/v1/traces") //nolint:glint // OTLP ingestion path must match OpenTelemetry conventions
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusAccepted)
			Response(string(oops.CodeRequestTooLarge), StatusRequestEntityTooLarge, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "ingestLiteLLMOTLPTraces")
		Meta("openapi:extension:x-speakeasy-name-override", "ingestOTLPTraces")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("metrics", func() {
		Meta("openapi:generate", "false")
		Description("Accepts LiteLLM OTLP metric exports. Send the standard OTLP JSON ExportMetricsServiceRequest shape shown here with application/json, or the binary opentelemetry.proto.collector.metrics.v1.ExportMetricsServiceRequest with application/x-protobuf or application/protobuf. Content-Encoding may be gzip.")
		Error(string(oops.CodeRequestTooLarge), func() { Description(oops.CodeRequestTooLarge.UserMessage()) })
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("resourceMetrics", ArrayOf(Any), "Standard OTLP ResourceMetrics objects. OTLP integer fields use their canonical decimal-string JSON representation.")
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/litellm.otel/v1/metrics") //nolint:glint // LiteLLM uses the standard OTLP HTTP path suffix
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusAccepted)
			Response(string(oops.CodeRequestTooLarge), StatusRequestEntityTooLarge, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "ingestLiteLLMOTLPMetrics")
		Meta("openapi:extension:x-speakeasy-name-override", "ingestOTLPMetrics")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})
})
