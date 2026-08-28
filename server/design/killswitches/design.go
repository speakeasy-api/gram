// Package killswitches declares the customer-facing MCP Killswitch API.
package killswitches

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var Capability = Type("KillswitchCapability", func() {
	Required("key", "label")
	Attribute("key", String, func() { Enum("mcp_tool_calls") })
	Attribute("label", String)
})

var ComingSoonCapability = Type("KillswitchComingSoonCapability", func() {
	Required("label")
	Attribute("label", String)
})

var MCPServer = Type("KillswitchMCPServer", func() {
	Required("id", "name", "project_id")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("project_id", String, func() { Format(FormatUUID) })
})

var CustomerScope = Type("KillswitchScope", func() {
	Required("type")
	Attribute("type", String, func() { Enum("all_servers", "selected_servers") })
	Attribute("server_ids", ArrayOf(String, func() { Format(FormatUUID) }), func() { MaxLength(1000) })
})

var Schedule = Type("KillswitchSchedule", func() {
	Required("start", "end")
	Attribute("start", String, func() { Enum("now", "scheduled") })
	Attribute("starts_at", String, func() { Format(FormatDateTime) })
	Attribute("end", String, func() { Enum("until_lifted", "bounded") })
	Attribute("ends_at", String, func() { Format(FormatDateTime) })
})

var Summary = Type("KillswitchSummary", func() {
	Required("id", "capability_key", "capability_label", "user_id", "version", "status", "scope", "schedule")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("capability_key", String, func() { Enum("mcp_tool_calls") })
	Attribute("capability_label", String)
	Attribute("user_id", String)
	Attribute("version", Int64)
	Attribute("status", String, func() { Enum("active", "scheduled", "expired", "lifted") })
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
})

var HistoryEvent = Type("KillswitchHistoryEvent", func() {
	Required("sequence", "version", "action", "status", "scope", "schedule", "external_note", "internal_note", "changed_at")
	Attribute("sequence", Int64)
	Attribute("version", Int64)
	Attribute("action", String, func() { Enum("created", "edited", "lifted", "expired", "restored") })
	Attribute("status", String, func() { Enum("active", "scheduled", "expired", "lifted") })
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
	Attribute("external_note", String)
	Attribute("internal_note", String)
	Attribute("actor_user_id", String)
	Attribute("actor_display_name", String)
	Attribute("changed_at", String, func() { Format(FormatDateTime) })
})

var Detail = Type("KillswitchDetail", func() {
	Extend(Summary)
	Required("external_note", "internal_note", "history", "history_truncated")
	Attribute("external_note", String)
	Attribute("internal_note", String)
	Attribute("history", ArrayOf(HistoryEvent))
	Attribute("history_truncated", Boolean)
})

var Overlap = Type("KillswitchOverlap", func() {
	Required("id", "status", "scope", "schedule")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("status", String, func() { Enum("active", "scheduled") })
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
})

var MutationResult = Type("KillswitchMutationResult", func() {
	Required("id", "version", "status", "replayed")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("version", Int64)
	Attribute("status", String, func() { Enum("active", "lifted") })
	Attribute("replayed", Boolean)
})

var UserBadge = Type("KillswitchUserBadge", func() {
	Required("user_id", "affected", "affected_now", "scheduled")
	Attribute("user_id", String)
	Attribute("affected", Boolean)
	Attribute("affected_now", Boolean)
	Attribute("scheduled", Boolean)
})

func desiredPayload() {
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
	Attribute("external_note", String, func() { MaxLength(500) })
	Attribute("internal_note", String, func() { MaxLength(4000) })
	Required("scope", "schedule", "external_note", "internal_note")
}

func mutationIdentityPayload() {
	Attribute("operation_id", String, func() { Format(FormatUUID) })
	Required("operation_id")
}

func declareErrors() {
	Error("operation_conflict", func() { Description("The operation ID was already used for a different request.") })
	Error("version_conflict", func() { Description("The killswitch changed after the supplied version.") })
	Error(string(oops.CodeUnavailable), func() { Description(oops.CodeUnavailable.UserMessage()); Fault() })
}

var _ = Service("killswitches", func() {
	Description("Manage MCP tool-call killswitches for users in the active organization. Requires an ordinary live organization-administrator session.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	declareErrors()
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response("operation_conflict", StatusConflict, func() { ContentType("application/json") })
		Response("version_conflict", StatusConflict, func() { ContentType("application/json") })
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() { ContentType("application/json") })
	})

	Method("listCapabilities", func() {
		Payload(func() { security.SessionPayload() })
		Result(func() {
			Required("capabilities", "coming_soon")
			Attribute("capabilities", ArrayOf(Capability))
			Attribute("coming_soon", ArrayOf(ComingSoonCapability))
		})
		HTTP(func() { GET("/rpc/killswitches.listCapabilities"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("listMCPServers", func() {
		Payload(func() { security.SessionPayload() })
		Result(func() { Required("servers"); Attribute("servers", ArrayOf(MCPServer)) })
		HTTP(func() { GET("/rpc/killswitches.listMCPServers"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("list", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("capability_key", String, func() { Enum("mcp_tool_calls") })
			Attribute("user_id", String)
			Attribute("status", String, func() { Enum("active", "scheduled", "expired", "lifted") })
			Attribute("limit", Int32, func() { Minimum(1); Maximum(100) })
			Attribute("cursor", String)
		})
		Result(func() { Required("items"); Attribute("items", ArrayOf(Summary)); Attribute("next_cursor", String) })
		HTTP(func() {
			GET("/rpc/killswitches.list")
			Param("capability_key")
			Param("user_id")
			Param("status")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:extension:x-speakeasy-pagination", `{"type":"cursor","inputs":[{"name":"cursor","in":"parameters","type":"cursor"}],"outputs":{"nextCursor":"$.next_cursor"}}`)
	})

	Method("get", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(Detail)
		HTTP(func() { GET("/rpc/killswitches.get"); Param("id"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("create", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("capability_key", String, func() { Enum("mcp_tool_calls") })
			Attribute("user_id", String)
			desiredPayload()
			Required("capability_key", "user_id")
		})
		Result(MutationResult)
		HTTP(func() { POST("/rpc/killswitches.create"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("edit", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("expected_version", Int64)
			desiredPayload()
			Required("id", "expected_version")
		})
		Result(MutationResult)
		HTTP(func() { POST("/rpc/killswitches.edit"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("lift", func() {
		Payload(func() {
			security.SessionPayload()
			mutationIdentityPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("expected_version", Int64)
			Required("id", "expected_version")
		})
		Result(func() {
			Required("result", "remaining_overlaps")
			Attribute("result", MutationResult)
			Attribute("remaining_overlaps", ArrayOf(Overlap))
		})
		HTTP(func() { POST("/rpc/killswitches.lift"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("previewOverlaps", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("capability_key", String, func() { Enum("mcp_tool_calls") })
			Attribute("user_id", String)
			Attribute("scope", CustomerScope)
			Attribute("schedule", Schedule)
			Required("capability_key", "user_id", "scope", "schedule")
		})
		Result(func() { Required("overlaps"); Attribute("overlaps", ArrayOf(Overlap)) })
		HTTP(func() { POST("/rpc/killswitches.previewOverlaps"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("batchUserBadges", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("user_ids", ArrayOf(String), func() { MinLength(1); MaxLength(100) })
			Required("user_ids")
		})
		Result(func() { Required("badges"); Attribute("badges", ArrayOf(UserBadge)) })
		HTTP(func() { POST("/rpc/killswitches.batchUserBadges"); security.SessionHeader(); Response(StatusOK) })
	})
})
