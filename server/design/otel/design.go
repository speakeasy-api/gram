package otel

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("otel", func() {
	Description("Receives OpenTelemetry signals from LLM providers and harnesses.")

	shared.DeclareErrorResponses()

	Method("logs", func() {
		Description("Endpoint to receive OTEL logs data from LLM providers and harnesses.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()

			Attribute("content_encoding", String, "Encoding applied to the OTLP request body. Supported values are gzip and identity.")
		})

		Result(Empty)

		HTTP(func() {
			POST("/otel/v1/logs")
			security.ByKeyHeader()
			security.ProjectHeader()
			Header("content_encoding:Content-Encoding")
			Response(StatusOK)
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadLogs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOpenTelemetryLogs"}`)
	})

	Method("traces", func() {
		Description("Endpoint to receive OTEL traces data from LLM providers and harnesses.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()

			Attribute("content_encoding", String, "Encoding applied to the OTLP request body. Supported values are gzip and identity.")
		})

		Result(Empty)

		HTTP(func() {
			POST("/otel/v1/traces")
			security.ByKeyHeader()
			security.ProjectHeader()
			Header("content_encoding:Content-Encoding")
			Response(StatusOK)
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadTraces")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadTraces")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOpenTelemetryTraces"}`)
	})
})
