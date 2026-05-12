package otelforwarding

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// Header is one customer-supplied HTTP header entry attached to forwarded
// OTEL requests. Values are write-only — reads return the name plus a
// has_value flag so the dashboard can show whether a secret is set without
// exposing it.
var HeaderModel = Type("OtelForwardingHeader", func() {
	Description("HTTP header forwarded with each OTEL payload.")
	Required("name", "has_value")
	Attribute("name", String, "Header name.")
	Attribute("has_value", Boolean, "Whether a non-empty value is currently stored for this header. Always false on write-only operations.")
})

var HeaderInput = Type("OtelForwardingHeaderInput", func() {
	Description("HTTP header value provided when upserting a forwarding config.")
	Required("name", "value")
	Attribute("name", String, "Header name.")
	Attribute("value", String, "Header value. Stored encrypted at rest; never returned on reads.")
})

var Config = Type("OtelForwardingConfig", func() {
	Description("Per-organization config that controls forwarding of OTEL payloads received on the hooks endpoints to a customer-owned URL.")
	Required("id", "organization_id", "endpoint_url", "enabled", "headers", "created_at", "updated_at")
	Attribute("id", String, "Config ID.")
	Attribute("organization_id", String, "Organization the config belongs to.")
	Attribute("endpoint_url", String, "URL each OTEL payload is POSTed to.")
	Attribute("enabled", Boolean, "Whether forwarding is currently active.")
	Attribute("headers", ArrayOf(HeaderModel), "Headers configured for this endpoint. Values are never returned.")
	Attribute("created_at", String, "ISO 8601 timestamp when the config was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change.", func() {
		Format(FormatDateTime)
	})
})

var _ = Service("otelForwarding", func() {
	Description("Manage per-organization forwarding of inbound OTEL hook payloads to a customer-owned endpoint.")

	shared.DeclareErrorResponses()

	Method("getConfig", func() {
		Description("Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(Config)

		HTTP(func() {
			GET("/rpc/otelForwarding.getConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOtelForwardingConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OtelForwardingConfig"}`)
	})

	Method("upsertConfig", func() {
		Description("Create or update the org-wide OTEL forwarding config. Replaces the full header set on each call.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("endpoint_url", String, "URL to forward OTEL payloads to.")
			Attribute("enabled", Boolean, "Whether forwarding should be active.")
			Attribute("headers", ArrayOf(HeaderInput), "Full set of headers to attach. Replaces any existing headers.")
			Required("endpoint_url", "enabled")
		})

		Result(Config)

		HTTP(func() {
			POST("/rpc/otelForwarding.upsertConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertOtelForwardingConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertOtelForwardingConfig"}`)
	})

	Method("deleteConfig", func() {
		Description("Delete the org-wide OTEL forwarding config.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/otelForwarding.deleteConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteOtelForwardingConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOtelForwardingConfig"}`)
	})
})
