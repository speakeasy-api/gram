package slackdirectoryconnections

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
	. "goa.design/goa/v3/dsl"
)

var Connection = Type("SlackDirectoryConnection", func() {
	Description("An organization Slack workspace authorization. Authorization does not indicate directory sync or invocation protection.")
	Attribute("id", String, "Durable organization workspace connection ID.", func() { Format(FormatUUID) })
	Attribute("workspace_id", String, "Verified Slack workspace ID.")
	Attribute("workspace_name", String, "Workspace display name from Slack.")
	Attribute("status", String, "Authorization status, independent of directory sync.", func() { Enum("connected", "disconnected", "reconnect_required") })
	Attribute("generation", String, "Connection generation used to reject stale changes.", func() { Format(FormatUUID) })
	Attribute("granted_scopes", ArrayOf(String), "Permissions granted by Slack.")
	Attribute("last_error_code", String, "Bounded authorization failure code, when known.")
	Attribute("disconnected_at", String, "When credentials were removed.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last connection change timestamp.", func() { Format(FormatDateTime) })
	Required("id", "workspace_id", "workspace_name", "status", "generation", "granted_scopes", "updated_at")
})

var _ = Service("slackDirectoryConnections", func() {
	Description("Manage organization-wide Slack workspace authorizations, independently of project runtime installations.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() { Description("Slack connections are unavailable or authorization is not configured."); Fault() })
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable)
	})
	Method("list", func() {
		Payload(func() { security.SessionPayload() })
		Result(func() {
			Attribute("connections", ArrayOf(Connection), "Workspace connections in creation order.")
			Attribute("authorization_configured", Boolean, "Whether the deployment can start Slack OAuth.")
			Required("connections", "authorization_configured")
		})
		HTTP(func() { GET("/rpc/slackDirectoryConnections.list"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "listSlackDirectoryConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SlackDirectoryConnections"}`)
	})
	Method("begin", func() {
		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "BeginSlackDirectoryConnectionRequestBody")
			Attribute("connection_id", String, "Reconnect this workspace.", func() { Format(FormatUUID) })
		})
		Result(func() {
			Attribute("authorization_url", String, "Slack authorization URL bound to the current session.")
			Required("authorization_url")
		})
		HTTP(func() { POST("/rpc/slackDirectoryConnections.begin"); security.SessionHeader(); Response(StatusOK) })
		Meta("openapi:operationId", "beginSlackDirectoryConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "begin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"BeginSlackDirectoryConnection"}`)
	})
	Method("disconnect", func() {
		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "DisconnectSlackDirectoryConnectionRequestBody")
			Attribute("id", String, "Durable organization workspace connection ID.", func() { Format(FormatUUID) })
			Attribute("generation", String, "Generation last read by the administrator.", func() { Format(FormatUUID) })
			Required("id", "generation")
		})
		Result(Connection)
		HTTP(func() {
			POST("/rpc/slackDirectoryConnections.disconnect")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "disconnectSlackDirectoryConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "disconnect")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"DisconnectSlackDirectoryConnection"}`)
	})
})
