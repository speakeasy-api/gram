package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var Session = Type("AgentSession", func() {
	Required("id", "issuer_id", "issuer_slug", "created_at", "expires_at", "refresh_expires_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("issuer_id", String, func() { Format(FormatUUID) })
	Attribute("issuer_slug", String)
	Attribute("client_name", String)
	Attribute("authorizer_user_id", String)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("expires_at", String, func() { Format(FormatDateTime) })
	Attribute("refresh_expires_at", String, func() { Format(FormatDateTime) })
	Attribute("last_used_at", String, func() { Format(FormatDateTime) })
})

func sessionMethods() {
	Method("listSessions", func() {
		Meta("openapi:operationId", "listAgentSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "listSessions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentSessions"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Attribute("cursor", String, func() { Format(FormatUUID) })
			Attribute("limit", Int, func() { Default(50); Minimum(1); Maximum(100) })
		})
		Result(func() {
			Required("items")
			Attribute("items", ArrayOf(Session))
			Attribute("next_cursor", String)
		})
		HTTP(func() {
			GET("/rpc/agents.listSessions")
			security.SessionHeader()
			Param("agent_id")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
	})
	Method("revokeSession", func() {
		Meta("openapi:operationId", "revokeAgentSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeSession")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeAgentSession"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Attribute("session_id", String, func() { Format(FormatUUID) })
			Required("agent_id", "session_id")
		})
		HTTP(func() {
			POST("/rpc/agents.revokeSession")
			security.SessionHeader()
			Response(StatusNoContent)
		})
	})
}
