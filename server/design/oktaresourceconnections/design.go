package oktaresourceconnections

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var ResourceConnectionServer = Type("OktaResourceConnectionServer", func() {
	Description("One MCP server and the Okta resource connection recorded for its upstream, with the derived Cross App Access state and the exact values the administrator enters in the identity provider console. Servers that share an upstream resource share one Okta resource connection.")
	Required("mcp_server_id", "project_id", "project_slug", "server_name", "server_slug", "state", "pending", "resource_indicator", "scopes", "client_binding")
	Attribute("mcp_server_id", String, "MCP server ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "Project the server belongs to.", func() {
		Format(FormatUUID)
	})
	Attribute("project_slug", String)
	Attribute("server_name", String, "Server display name; admin-editable, never a key.")
	Attribute("server_slug", String)
	Attribute("state", String, "Derived readiness. not_applicable: see not_applicable_reason; needs_agent: no AI agent recorded on the connection; needs_connection: the agent-to-resource connection has not been confirmed for this server's upstream; connected: the administrator confirmed it. Whether the exchange works is not known here.", func() {
		Enum("not_applicable", "needs_agent", "needs_connection", "connected")
	})
	Attribute("not_applicable_reason", String, "no_idjag: the server's authorization server metadata does not advertise the identity assertion grant.", func() {
		Enum("no_idjag")
	})
	Attribute("pending", Boolean, "Whether the administrator still has a step to do for this server.")
	Attribute("issuer_id", String, "The upstream authorization server ID. Together with the resource indicator, identifies the shared readiness confirmation; independent of the confirmed identity assertion audience.", func() {
		Format(FormatUUID)
	})
	Attribute("resource_indicator", String, "The resource indicator to enter on the connection: the server's RFC 9728 resource identifier when known, otherwise its URL.")
	Attribute("client_id", String, "Speakeasy's client ID at the server's authorization server. Omitted when no client is bound yet.")
	Attribute("client_binding", String, "bound: an explicit identity chaining binding selects the client; single: the one client attached to the authorization server; ambiguous: more than one candidate and no single binding; missing: no client registered yet.", func() {
		Enum("bound", "single", "ambiguous", "missing")
	})
	Attribute("scopes", ArrayOf(String), "Scopes Speakeasy requests at the resource.")
	Attribute("deep_link", String, "Link to the add-resource-connection form of the AI agent in the identity provider console. Omitted until an agent is recorded or when the org uses a custom domain.")
	Attribute("audience", String, "The resource app's Cross App Access issuer URL as configured in the identity provider; the audience of the identity assertion. Present once confirmed.")
	Attribute("okta_application_id", String, "The identity provider app instance the administrator picked for this upstream, when recorded.")
	Attribute("okta_application_label", String, "Label of that app instance in the latest applications snapshot; omitted when the instance is no longer there.")
	Attribute("confirmed_at", String, "When the administrator confirmed the connection for this server's upstream.", func() {
		Format(FormatDateTime)
	})
})

var ListResult = Type("ListOktaResourceConnectionsResult", func() {
	Required("servers", "pending_count", "total_count", "undiscovered_count", "agent_recorded")
	Attribute("servers", ArrayOf(ResourceConnectionServer), "Pending servers first, then by name. By default only servers with a step left; include_all adds connected and not-applicable ones.")
	Attribute("pending_count", Int, "Servers that still need a step.")
	Attribute("total_count", Int, "Eligible servers before filtering.")
	Attribute("undiscovered_count", Int, "Servers left out because their authorization server metadata has not been fetched yet.")
	Attribute("agent_recorded", Boolean, "Whether the connection has an AI agent recorded; every server needs one first.")
	Attribute("connection_id", String, "The organization's identity provider connection.", func() {
		Format(FormatUUID)
	})
	Attribute("deep_link", String, "Link to the add-resource-connection form of the AI agent in the identity provider console. Omitted until an agent is recorded or when the org uses a custom domain.")
})

var Confirmation = Type("OktaResourceConnectionConfirmation", func() {
	Description("One server the administrator connected in the identity provider console, with the values they configured there.")
	Required("mcp_server_id", "audience")
	Attribute("mcp_server_id", String, "Server whose upstream was connected.", func() {
		Format(FormatUUID)
	})
	Attribute("audience", String, "The issuer URL entered on the resource app's Cross App Access settings; the identity assertion audience. Must be an https URL without query or fragment.", func() {
		Format(FormatURI)
		Pattern(`^https://[^?#]+$`)
		MaxLength(512)
	})
	Attribute("okta_application_id", String, "The identity provider app instance picked for this upstream, from the applications snapshot.", func() {
		MaxLength(64)
	})
})

var ConfirmResult = Type("ConfirmOktaResourceConnectionsResult", func() {
	Required("servers")
	Attribute("servers", ArrayOf(ResourceConnectionServer), "The confirmed servers with their new state.")
})

var _ = Service("oktaResourceConnections", func() {
	Description("Okta resource connections per MCP server: what the organization administrator still has to do in the identity provider console, what they confirmed, and what the exchange path observed. Advisory only; the exchange path never consults it.")

	shared.DeclareErrorResponses()
	Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
			ContentType("application/json")
		})
	})

	Method("list", func() {
		Description("List the Okta resource connection and derived state of every eligible MCP server. Requires org:admin and a live identity provider connection.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("include_all", Boolean, "Include connected and not-applicable servers.", func() {
				Default(false)
			})
		})

		Result(ListResult)

		HTTP(func() {
			GET("/rpc/oktaResourceConnections.list")
			security.SessionHeader()
			Param("include_all")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listOktaResourceConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OktaResourceConnections"}`)
	})

	Method("confirm", func() {
		Description("Record that the administrator created the agent-to-resource connections for these servers in the identity provider console, with the audience each resource app is configured with. Servers that share an upstream share one Okta resource connection. Confirmation is the administrator's word; Speakeasy cannot check it. The connection must be verified or degraded. Requires org:admin and the okta-connections rollout.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "ConfirmOktaResourceConnectionsRequestBody")
			Attribute("connections", ArrayOf(Confirmation), "Servers to confirm with their configured values.", func() {
				MinLength(1)
				MaxLength(200)
			})
			Required("connections")
		})

		Result(ConfirmResult)

		HTTP(func() {
			POST("/rpc/oktaResourceConnections.confirm")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "confirmOktaResourceConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "confirm")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ConfirmOktaResourceConnections"}`)
	})

	Method("reset", func() {
		Description("Delete the Okta resource connection for this server's upstream so it, and every server sharing that upstream, shows as pending again. Requires org:admin.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "ResetOktaResourceConnectionRequestBody")
			Attribute("mcp_server_id", String, "Server to reset.", func() {
				Format(FormatUUID)
			})
			Required("mcp_server_id")
		})

		Result(ResourceConnectionServer)

		HTTP(func() {
			POST("/rpc/oktaResourceConnections.reset")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "resetOktaResourceConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "reset")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResetOktaResourceConnection"}`)
	})
})
