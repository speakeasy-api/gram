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

var FailurePosture = Type("LiteLLMFailurePosture", String, func() {
	Description("How LiteLLM behaves when Gram cannot evaluate a request.")
	Enum("fail_closed", "fail_open")
})

var InstanceHealthStatus = Type("LiteLLMInstanceHealthStatus", String, func() {
	Description("Derived health of a LiteLLM integration's latest ingest activity.")
	Enum("pending", "success", "failed")
})

var InstanceErrorKind = Type("LiteLLMInstanceErrorKind", String, func() {
	Description("Safe category for the latest observed ingest error.")
	Enum("auth_failure", "decode_failure", "limit_exceeded")
})

var InstanceDiagnostics = Type("LiteLLMInstanceDiagnostics", func() {
	Description("Health and identity-attribution diagnostics for a LiteLLM integration. No prompt, credential, or identity values are returned.")
	Attribute("status", InstanceHealthStatus)
	Attribute("last_guardrail_event_at", String, func() { Format(FormatDateTime) })
	Attribute("last_otel_event_at", String, func() { Format(FormatDateTime) })
	Attribute("last_error_at", String, func() { Format(FormatDateTime) })
	Attribute("last_error_kind", InstanceErrorKind)
	Attribute("reported_litellm_version", String, func() { MaxLength(128) })
	Attribute("virtual_key_email_pct_24h", Float64, "Percentage of model requests in the last 24 hours that supplied a virtual-key email.", func() {
		Minimum(0)
		Maximum(100)
	})
	Attribute("platform_user_pct_24h", Float64, "Percentage of model requests in the last 24 hours that resolved to a Gram user.", func() {
		Minimum(0)
		Maximum(100)
	})
	Required("status")
})

var Instance = Type("LiteLLMInstance", func() {
	Description("A provisioned LiteLLM integration and its project-bound ingestion credential metadata.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("organization_id", String)
	Attribute("project", shared.ProjectEntry)
	Attribute("name", String)
	Attribute("failure_posture", FailurePosture)
	Attribute("key_prefix", String)
	Attribute("created_by_user_id", String)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Attribute("last_used_at", String, func() { Format(FormatDateTime) })
	Attribute("active", Boolean)
	Attribute("diagnostics", InstanceDiagnostics)
	Required("id", "organization_id", "project", "name", "failure_posture", "key_prefix", "created_by_user_id", "created_at", "updated_at", "active", "diagnostics")
})

var InstanceKeyResult = ResultType("application/vnd.litellm.instance-key-result", func() {
	Description("A LiteLLM instance and its newly minted plaintext key. The key is shown only once.")
	Attribute("instance", Instance)
	Attribute("key", String)
	Required("instance", "key")
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

	Method("createInstance", func() {
		Description("Provision a LiteLLM integration for a project and return its plaintext ingestion key once. Requires org:admin.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("name", String, func() { MaxLength(255) })
			Attribute("failure_posture", FailurePosture, func() { Default("fail_closed") })
			Required("name")
		})
		Result(InstanceKeyResult)
		HTTP(func() {
			POST("/rpc/litellm.createInstance")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusCreated)
		})
		Meta("openapi:operationId", "createLiteLLMInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "createInstance")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateLiteLLMInstance"}`)
	})

	Method("listInstances", func() {
		Description("List active and revoked LiteLLM integrations for a project. Plaintext keys are never returned. Requires org:admin.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(func() {
			Attribute("instances", ArrayOf(Instance))
			Required("instances")
		})
		HTTP(func() {
			GET("/rpc/litellm.listInstances")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listLiteLLMInstances")
		Meta("openapi:extension:x-speakeasy-name-override", "listInstances")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LiteLLMInstances"}`)
	})

	Method("rotateInstanceKey", func() {
		Description("Atomically replace a LiteLLM integration key and return the new plaintext value once. Requires org:admin.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(InstanceKeyResult)
		HTTP(func() {
			POST("/rpc/litellm.rotateInstanceKey")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "rotateLiteLLMInstanceKey")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateInstanceKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateLiteLLMInstanceKey"}`)
	})

	Method("revokeInstance", func() {
		Description("Revoke a LiteLLM integration and immediately invalidate its active key. Requires org:admin.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(Empty)
		HTTP(func() {
			DELETE("/rpc/litellm.revokeInstance")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "revokeLiteLLMInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeInstance")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeLiteLLMInstance"}`)
	})

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
})
