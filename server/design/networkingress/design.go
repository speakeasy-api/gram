package networkingress

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// NetworkIngress represents an organization's private network ingress: an
// embedded overlay-network node (Tailscale first) that serves the org's MCP
// endpoints directly on the customer's private network.
var NetworkIngress = Type("NetworkIngress", func() {
	Attribute("id", String, "The ID of the network ingress", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The ID of the organization this ingress belongs to")
	Attribute("provider", String, "The overlay network technology serving this ingress. Only 'tailscale' is currently supported.")
	Attribute("hostname", String, "The device hostname the node advertises on the customer's network")
	Attribute("tags", ArrayOf(String), "ACL tags the node advertises on the customer's network")
	Attribute("enabled", Boolean, "Whether the ingress node should be running")
	Attribute("private_network_only", Boolean, "Whether public access is disabled: MCP traffic is rejected on the platform host and the org's custom domain")
	Attribute("identity_required", Boolean, "Whether requests without an attributable network identity are rejected at the gateway")
	Attribute("credential_kind", String, "The kind of credential configured for joining the customer's network. One of: auth_key, oauth_client.")
	Attribute("auth_key_configured", Boolean, "Whether a join auth key is currently stored. The key itself is never returned.")
	Attribute("oauth_client_configured", Boolean, "Whether an OAuth client credential is currently stored. The secret itself is never returned.")
	Attribute("status", String, "The latest observed node status. One of: pending, connecting, online, offline, error, disabled.")
	Attribute("network_name", String, "The name of the customer network the node last joined, if known")
	Attribute("dns_name", String, "The DNS name the node is reachable at on the customer network, if known")
	Attribute("last_error", String, "The most recent node error, if any")
	Attribute("last_seen_at", String, func() {
		Description("When the node last reported in.")
		Format(FormatDateTime)
	})
	Attribute("connected_since", String, func() {
		Description("When the node's current connection was established.")
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Description("When the network ingress was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the network ingress was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "provider", "hostname", "tags", "enabled", "private_network_only", "identity_required", "credential_kind", "auth_key_configured", "oauth_client_configured", "status", "created_at", "updated_at")
})

var _ = Service("networkIngress", func() {
	Description("Manage private network ingresses that serve an organization's MCP endpoints on its own overlay network.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getIngress", func() {
		Description("Get the network ingress for an organization")

		Payload(func() {
			security.SessionPayload()
		})

		Result(NetworkIngress)

		HTTP(func() {
			GET("/rpc/networkIngress.get")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "getIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "NetworkIngress"}`)
	})

	Method("createIngress", func() {
		Description("Create a network ingress for an organization. Exactly one credential mode must be provided: an auth key, or an OAuth client id and secret.")

		Payload(func() {
			security.SessionPayload()
			Attribute("provider", String, "The overlay network technology. Defaults to 'tailscale', the only supported provider.")
			Attribute("hostname", String, "The device hostname the node advertises. Defaults to 'gram-mcp'.")
			Attribute("tags", ArrayOf(String), "ACL tags the node advertises. Defaults to the provider's recommended tag set.")
			Attribute("auth_key", String, "A reusable, non-ephemeral join key for the customer network. Stored encrypted and never returned.")
			Attribute("oauth_client_id", String, "The OAuth client ID used to mint join keys on demand.")
			Attribute("oauth_client_secret", String, "The OAuth client secret. Stored encrypted and never returned.")
			Attribute("private_network_only", Boolean, "Whether to disable public access to the org's MCP endpoints. Defaults to false.")
			Attribute("identity_required", Boolean, "Whether to reject requests without an attributable network identity. Defaults to true.")
		})

		Result(NetworkIngress)

		HTTP(func() {
			POST("/rpc/networkIngress.create")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "createIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateNetworkIngress"}`)
	})

	Method("updateIngress", func() {
		Description("Update settings for the organization's network ingress. Provide at least one setting.")

		Payload(func() {
			security.SessionPayload()
			Attribute("hostname", String, "Replacement device hostname")
			Attribute("enabled", Boolean, "Whether the ingress node should be running")
			Attribute("private_network_only", Boolean, "Whether public access to the org's MCP endpoints is disabled")
			Attribute("identity_required", Boolean, "Whether requests without an attributable network identity are rejected")
		})

		Result(NetworkIngress)

		HTTP(func() {
			POST("/rpc/networkIngress.update")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "updateIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateNetworkIngress"}`)
	})

	Method("rotateCredentials", func() {
		Description("Replace the credential used to join the customer network. Exactly one credential mode must be provided. The node re-authenticates with the new credential.")

		Payload(func() {
			security.SessionPayload()
			Attribute("auth_key", String, "A reusable, non-ephemeral join key for the customer network. Stored encrypted and never returned.")
			Attribute("oauth_client_id", String, "The OAuth client ID used to mint join keys on demand.")
			Attribute("oauth_client_secret", String, "The OAuth client secret. Stored encrypted and never returned.")
		})

		Result(NetworkIngress)

		HTTP(func() {
			POST("/rpc/networkIngress.rotateCredentials")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rotateNetworkIngressCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateCredentials")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateNetworkIngressCredentials"}`)
	})

	Method("deleteIngress", func() {
		Description("Delete the organization's network ingress. The gateway logs the node out of the customer network and purges its stored node state.")

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/networkIngress.delete")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteNetworkIngress"}`)
	})
})
