package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AgentAPIKey = Type("AgentAPIKey", func() {
	Required("id", "name", "created_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("expires_at", String, func() { Format(FormatDateTime) })
	Attribute("last_accessed_at", String, func() { Format(FormatDateTime) })
})

func apiKeyMethods() {
	Method("listAPIKeys", func() {
		Description("List existing credentials bound to this exact agent. Does not issue credentials.")
		Meta("openapi:operationId", "listAgentAPIKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listAPIKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentAPIKeys"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Attribute("cursor", String, func() { Format(FormatUUID) })
		})
		Result(func() {
			Required("items")
			Attribute("items", ArrayOf(AgentAPIKey))
			Attribute("next_cursor", String)
		})
		HTTP(func() {
			GET("/rpc/agents.listAPIKeys")
			security.SessionHeader()
			Param("agent_id")
			Param("cursor")
			Response(StatusOK)
		})
	})
	Method("revokeAPIKey", func() {
		Description("Revoke an existing credential bound to this exact agent.")
		Meta("openapi:operationId", "revokeAgentAPIKey")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeAPIKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeAgentAPIKey"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Attribute("key_id", String, func() { Format(FormatUUID) })
			Required("agent_id", "key_id")
		})
		HTTP(func() {
			POST("/rpc/agents.revokeAPIKey")
			security.SessionHeader()
			Body(func() { Attribute("agent_id"); Attribute("key_id"); Required("agent_id", "key_id") })
			Response(StatusNoContent)
		})
	})
}
