package networkingress

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var NetworkIngress = Type("NetworkIngress", func() {
	Attribute("id", String, "Network ingress ID", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "Owning organization ID")
	Attribute("provider", String, "Private-network provider", func() { Enum("tailscale") })
	Attribute("hostname", String, "Private DNS label advertised by the provider")
	Attribute("endpoint_namespace_kind", String, "Pinned endpoint namespace", func() { Enum("platform", "custom_domain") })
	Attribute("custom_domain_id", String, "Pinned custom-domain ID when endpoint_namespace_kind is custom_domain", func() { Format(FormatUUID) })
	Attribute("enabled", Boolean, "Whether desired-state reconciliation may keep the ingress online")
	Attribute("identity_required", Boolean, "Whether an attributable provider identity is required")
	Attribute("credentials_configured", Boolean, "Whether provider credentials are stored; credential values are never returned")
	Attribute("status", String, "Latest redacted lifecycle status")
	Attribute("dns_name", String, "Observed private DNS name")
	Attribute("last_error", String, "Latest redacted error code")
	Attribute("health_checked_at", String, func() { Format(FormatDateTime) })
	Attribute("connected_since", String, func() { Format(FormatDateTime) })
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "organization_id", "provider", "hostname", "endpoint_namespace_kind", "enabled", "identity_required", "credentials_configured", "status", "created_at", "updated_at")
})

var DeleteImpact = Type("NetworkIngressDeleteImpact", func() {
	Attribute("mcp_servers_dual", Int64)
	Attribute("mcp_servers_private_only", Int64)
	Attribute("meta_mcp_servers_dual", Int64)
	Attribute("meta_mcp_servers_private_only", Int64)
	Required("mcp_servers_dual", "mcp_servers_private_only", "meta_mcp_servers_dual", "meta_mcp_servers_private_only")
})

var _ = Service("networkIngress", func() {
	Description("Manage an organization's private network ingress desired state.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getIngress", func() {
		Description("Get the active network ingress for the current organization.")
		Payload(func() { security.SessionPayload() })
		Result(NetworkIngress)
		HTTP(func() {
			GET("/rpc/networkIngress.get")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "getIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"NetworkIngress"}`)
	})

	Method("createIngress", func() {
		Description("Create the organization's Tailscale ingress. Provider credentials are encrypted and never returned.")
		Payload(func() {
			security.SessionPayload()
			Attribute("provider", String, func() { Enum("tailscale") })
			Attribute("hostname", String, "Lowercase DNS label")
			Attribute("oauth_client_id", String)
			Attribute("oauth_client_secret", String)
			Attribute("identity_required", Boolean)
			Required("provider", "hostname", "oauth_client_id", "oauth_client_secret")
		})
		Result(NetworkIngress)
		HTTP(func() {
			POST("/rpc/networkIngress.create")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "createIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"CreateNetworkIngress"}`)
	})

	Method("updateIngress", func() {
		Description("Update desired ingress settings. Enabling is expansion-gated; disabling remains available after gate removal.")
		Payload(func() {
			security.SessionPayload()
			Attribute("hostname", String)
			Attribute("enabled", Boolean)
			Attribute("identity_required", Boolean)
		})
		Result(NetworkIngress)
		HTTP(func() {
			POST("/rpc/networkIngress.update")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "updateIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateNetworkIngress"}`)
	})

	Method("rotateCredentials", func() {
		Description("Replace the provider OAuth client credentials without returning either value.")
		Payload(func() {
			security.SessionPayload()
			Attribute("oauth_client_id", String)
			Attribute("oauth_client_secret", String)
			Required("oauth_client_id", "oauth_client_secret")
		})
		Result(NetworkIngress)
		HTTP(func() {
			POST("/rpc/networkIngress.rotateCredentials")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "rotateNetworkIngressCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateCredentials")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"RotateNetworkIngressCredentials"}`)
	})

	Method("getDeleteImpact", func() {
		Description("Count hosted MCP servers that retain dual or private-only modes after ingress deletion.")
		Payload(func() { security.SessionPayload() })
		Result(DeleteImpact)
		HTTP(func() {
			GET("/rpc/networkIngress.getDeleteImpact")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getNetworkIngressDeleteImpact")
		Meta("openapi:extension:x-speakeasy-name-override", "getDeleteImpact")
	})

	Method("deleteIngress", func() {
		Description("Soft-delete the ingress and retain provider identities until cleanup is confirmed.")
		Payload(func() { security.SessionPayload() })
		HTTP(func() {
			DELETE("/rpc/networkIngress.delete")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteIngress")
	})

	Method("checkHealth", func() {
		Description("Signal reconciliation and return the latest observed ingress health.")
		Payload(func() { security.SessionPayload() })
		Result(NetworkIngress)
		HTTP(func() {
			POST("/rpc/networkIngress.checkHealth")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "checkNetworkIngressHealth")
		Meta("openapi:extension:x-speakeasy-name-override", "checkHealth")
	})
})
