package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminNetworkIngressGatewayNode = Type("AdminNetworkIngressGatewayNode", func() {
	Description("One tailnet gateway node identity for an ingress, as last reported by the gateway pod hosting it.")
	Required("slot", "status", "retired")

	Attribute("slot", Int32, "The node identity index; it equals the gateway pod ordinal that hosts it.")
	Attribute("status", String, "The latest heartbeat status: pending, starting, online, draining, error or stopped.")
	Attribute("pod_name", String, "The gateway pod that last reported this slot.")
	Attribute("last_error", String, "The latest failure the pod reported, if any.")
	Attribute("last_seen_at", String, "When the slot last heartbeated.", func() { Format(FormatDateTime) })
	Attribute("connected_since", String, "When the slot came online.", func() { Format(FormatDateTime) })
	Attribute("retired", Boolean, "Whether the slot is being drained and will be removed.")
})

var AdminNetworkIngress = Type("AdminNetworkIngress", func() {
	Description("An organization's private network ingress and the runtime that hosts it.")
	Required("id", "provider", "hostname", "runtime", "status", "enabled", "credentials_configured", "created_at", "gateway_nodes")

	Attribute("id", String, "The ingress ID.", func() { Format(FormatUUID) })
	Attribute("provider", String, "The network provider; always tailscale today.")
	Attribute("hostname", String, "The Tailscale Service label.")
	Attribute("dns_name", String, "The name clients reach once the ingress is online.")
	Attribute("runtime", String, "What hosts the ingress: operator for the Tailscale Kubernetes operator, gateway for the tailnet gateway.", func() {
		Enum("operator", "gateway")
	})
	Attribute("status", String, "The reconciled status: pending, online, degraded, error or disabled.")
	Attribute("last_error", String, "The latest reconcile failure code, if any.")
	Attribute("enabled", Boolean, "Whether the organization has the ingress turned on.")
	Attribute("credentials_configured", Boolean, "Whether Tailscale OAuth client credentials are saved.")
	Attribute("health_checked_at", String, "When the reconciler last observed the ingress.", func() { Format(FormatDateTime) })
	Attribute("connected_since", String, "When the ingress came online.", func() { Format(FormatDateTime) })
	Attribute("created_at", String, "When the ingress was created.", func() { Format(FormatDateTime) })
	Attribute("gateway_nodes", ArrayOf(AdminNetworkIngressGatewayNode), "The tailnet gateway node identities; empty while the operator hosts the ingress.")
})

var AdminOrganizationNetworkIngress = Type("AdminOrganizationNetworkIngress", func() {
	Description("One organization's private network ingress, for staff moving it to the tailnet gateway.")
	Required("organization_id", "gateway_tag")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("gateway_tag", String, "The ACL tag the saved OAuth client must own before the ingress can move to the tailnet gateway.")
	Attribute("ingress", AdminNetworkIngress, "The organization's live ingress; absent when it has none.")
})

func networkIngressGatewayMethods() {
	Method("getOrganizationNetworkIngress", func() {
		Description("Reports an organization's private network ingress, which runtime hosts it, and how its tailnet gateway nodes are doing.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")
		})

		Result(AdminOrganizationNetworkIngress)

		HTTP(func() {
			GET("/admin/organization.networkIngress")
			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetOrganizationNetworkIngress")
		Meta("openapi:extension:x-speakeasy-name-override", "getOrganizationNetworkIngress")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOrganizationNetworkIngress"}`)
	})

	Method("moveOrganizationNetworkIngressToGateway", func() {
		Description("Moves an organization's private network ingress from the Tailscale operator to the tailnet gateway. The saved OAuth client is checked first by minting and revoking a gateway-tagged key; on success the reconciler tears down the operator resources and brings the gateway up, with a short gap in private access while the service changes hosts. Already-moved ingresses are returned unchanged.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")

			// Goa's OpenAPI emitter deduplicates request bodies by shape, so
			// without an explicit typename this body takes another method's
			// name in the generated SDK.
			Meta("openapi:typename", "MoveOrganizationNetworkIngressToGatewayRequestBody")
		})

		Result(AdminOrganizationNetworkIngress)

		HTTP(func() {
			POST("/admin/organization.networkIngress.moveToGateway")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminMoveOrganizationNetworkIngressToGateway")
		Meta("openapi:extension:x-speakeasy-name-override", "moveOrganizationNetworkIngressToGateway")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"MoveAdminOrganizationNetworkIngressToGateway"}`)
	})
}
