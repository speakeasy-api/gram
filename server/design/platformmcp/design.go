package platformmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var _ = Service("platformMcp", func() {
	Description("Read Platform MCP lifecycle state and revoke organization-scoped Platform MCP connections. OAuth credentials and client metadata are never returned.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})

	Method("getLifecycle", func() {
		Description("Get Platform MCP onboarding, publication, authorization, and discovery facts for the active organization. Requires org:admin.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(PlatformMCPLifecycle)

		HTTP(func() {
			GET("/rpc/platformMcp.getLifecycle")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "getPlatformMcpLifecycle")
		Meta("openapi:extension:x-speakeasy-name-override", "getLifecycle")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformMcpLifecycle"}`)
	})

	Method("revokeConnection", func() {
		Description("Revoke the active Platform MCP connection for the active organization. This remains available when Platform MCP rollout gates are unavailable. Requires live org:admin.")

		Payload(func() {
			Attribute("connection_id", String, "Opaque Platform MCP connection identifier.", func() {
				Format(FormatUUID)
			})
			Required("connection_id")
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/platformMcp.revokeConnection")
			security.SessionHeader()
			Response(StatusNoContent)
			Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "revokePlatformMcpConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokePlatformMcpConnection"}`)
	})
})

var PlatformMCPLifecycle = Type("PlatformMCPLifecycle", func() {
	Required("admission", "reason_code", "marketplace_published", "authorized", "ready")

	Attribute("admission", String, "Current package admission evaluation.", func() {
		Enum("enabled", "disabled", "indeterminate")
	})
	Attribute("reason_code", String, "Bounded lifecycle state reason.", func() {
		Enum("eligible", "default_project_missing", "marketplace_unpublished", "gate_disabled", "status_indeterminate", "authorized_awaiting_discovery", "ready")
	})
	Attribute("default_project_id", String, "Literal default project identifier when it exists.", func() {
		Format(FormatUUID)
	})
	Attribute("marketplace_published", Boolean, "Whether the literal default project has a published marketplace repository.")
	Attribute("connections", ArrayOf(PlatformMCPConnection), "Active Platform MCP connections, each scoped to this organization and usable only to revoke within it.")
	Attribute("authorized", Boolean, "Whether the organization has an active Platform MCP connection.")
	Attribute("ready", Boolean, "Whether at least one active connection completed authenticated tools/list discovery.")
})

var PlatformMCPConnection = Type("PlatformMCPConnection", func() {
	Required("id", "ready")

	Attribute("id", String, "Opaque active Platform MCP connection identifier, usable only to revoke within this organization.", func() {
		Format(FormatUUID)
	})
	Attribute("authorized_at", String, "When this connection was authorized.", func() {
		Format(FormatDateTime)
	})
	Attribute("reauthorized_at", String, "When this connection was most recently reauthorized.", func() {
		Format(FormatDateTime)
	})
	Attribute("ready", Boolean, "Whether authenticated tools/list completed for this connection generation.")
})
