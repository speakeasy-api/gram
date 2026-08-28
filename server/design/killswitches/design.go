// Package killswitches declares the customer-facing MCP Killswitch API.
package killswitches

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var CapabilityKey = Type("KillswitchCapabilityKey", String, func() { Enum("mcp_tool_calls") })
var ScopeType = Type("KillswitchScopeType", String, func() { Enum("all_servers", "selected_servers") })
var ScheduleStart = Type("KillswitchScheduleStart", String, func() { Enum("now", "scheduled") })
var ScheduleEnd = Type("KillswitchScheduleEnd", String, func() { Enum("until_lifted", "bounded") })
var Status = Type("KillswitchStatus", String, func() { Enum("active", "scheduled", "expired", "lifted") })
var HistoryAction = Type("KillswitchHistoryAction", String, func() { Enum("created", "edited", "lifted", "expired", "restored") })
var OverlapStatus = Type("KillswitchOverlapStatus", String, func() { Enum("active", "scheduled") })

var Capability = Type("KillswitchCapability", func() {
	Required("key", "label")
	Attribute("key", CapabilityKey)
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
	Attribute("type", ScopeType)
	Attribute("server_ids", ArrayOf(String, func() { Format(FormatUUID) }), func() { MaxLength(1000) })
})

var Schedule = Type("KillswitchSchedule", func() {
	Required("start", "end")
	Attribute("start", ScheduleStart)
	Attribute("starts_at", String, func() { Format(FormatDateTime) })
	Attribute("end", ScheduleEnd)
	Attribute("ends_at", String, func() { Format(FormatDateTime) })
})

var Summary = Type("KillswitchSummary", func() {
	Required("id", "capability_key", "capability_label", "user_id", "version", "status", "scope", "schedule")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("capability_key", CapabilityKey)
	Attribute("capability_label", String)
	Attribute("user_id", String)
	Attribute("version", Int64)
	Attribute("status", Status)
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
})

var HistoryEvent = Type("KillswitchHistoryEvent", func() {
	Required("sequence", "version", "action", "status", "scope", "schedule", "external_note", "internal_note", "changed_at")
	Attribute("sequence", Int64)
	Attribute("version", Int64)
	Attribute("action", HistoryAction)
	Attribute("status", Status)
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
	Attribute("status", OverlapStatus)
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
})

var MutationResult = Type("KillswitchMutationReceipt", func() {
	Required("id", "version", "replayed")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("version", Int64)
	Attribute("replayed", Boolean)
})

var Conflict = Type("KillswitchConflict", func() {
	Required("name", "message")
	ErrorName("name", String, func() { Enum("operation_conflict", "version_conflict") })
	Attribute("message", String)
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

var CreateForm = Type("KillswitchCreateRequest", func() {
	mutationIdentityPayload()
	Attribute("capability_key", CapabilityKey)
	Attribute("user_id", String)
	desiredPayload()
	Required("capability_key", "user_id")
})

var EditForm = Type("KillswitchEditRequest", func() {
	mutationIdentityPayload()
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("expected_version", Int64)
	desiredPayload()
	Required("id", "expected_version")
})

var LiftForm = Type("KillswitchLiftRequest", func() {
	mutationIdentityPayload()
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("expected_version", Int64)
	Required("id", "expected_version")
})

var PreviewOverlapsForm = Type("KillswitchPreviewOverlapsRequest", func() {
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("capability_key", CapabilityKey)
	Attribute("user_id", String)
	Attribute("scope", CustomerScope)
	Attribute("schedule", Schedule)
	Required("capability_key", "user_id", "scope", "schedule")
})

var BatchUserBadgesForm = Type("KillswitchBatchUserBadgesRequest", func() {
	Attribute("user_ids", ArrayOf(String), func() { MinLength(1); MaxLength(100) })
	Required("user_ids")
})

var ListCapabilitiesResult = Type("KillswitchListCapabilitiesResult", func() {
	Required("capabilities", "coming_soon")
	Attribute("capabilities", ArrayOf(Capability))
	Attribute("coming_soon", ArrayOf(ComingSoonCapability))
})

var ListMCPServersResult = Type("KillswitchListMCPServersResult", func() {
	Required("servers")
	Attribute("servers", ArrayOf(MCPServer))
})

var ListResult = Type("KillswitchListResult", func() {
	Required("items")
	Attribute("items", ArrayOf(Summary))
	Attribute("next_cursor", String)
})

var LiftResult = Type("KillswitchLiftResult", func() {
	Required("result", "remaining_overlaps")
	Attribute("result", MutationResult)
	Attribute("remaining_overlaps", ArrayOf(Overlap))
})

var PreviewOverlapsResult = Type("KillswitchPreviewOverlapsResult", func() {
	Required("overlaps")
	Attribute("overlaps", ArrayOf(Overlap))
})

var BatchUserBadgesResult = Type("KillswitchBatchUserBadgesResult", func() {
	Required("badges")
	Attribute("badges", ArrayOf(UserBadge))
})

func declareOperationConflict() {
	Error("operation_conflict", Conflict, "The operation ID was already used for a different request.")
}

func declareMutationErrors() {
	declareOperationConflict()
	Error("version_conflict", Conflict, "The killswitch changed after the supplied version.")
}

func declareErrors() {
	Error(string(oops.CodeUnauthorized), func() { Description(oops.CodeUnauthorized.UserMessage()) })
	Error(string(oops.CodeForbidden), func() { Description(oops.CodeForbidden.UserMessage()) })
	Error(string(oops.CodeBadRequest), func() { Description(oops.CodeBadRequest.UserMessage()) })
	Error(string(oops.CodeNotFound), func() { Description(oops.CodeNotFound.UserMessage()) })
	Error(string(oops.CodeUnsupportedMedia), func() { Description(oops.CodeUnsupportedMedia.UserMessage()) })
	Error(string(oops.CodeInvalid), func() { Description(oops.CodeInvalid.UserMessage()) })
	Error(string(oops.CodeInvariantViolation), func() { Description(oops.CodeInvariantViolation.UserMessage()); Fault() })
	Error(string(oops.CodeUnexpected), func() { Description(oops.CodeUnexpected.UserMessage()); Fault() })
	Error(string(oops.CodeGatewayError), func() { Description(oops.CodeGatewayError.UserMessage()); Fault() })
	Error(string(oops.CodeUnavailable), func() { Description(oops.CodeUnavailable.UserMessage()); Fault() })
}

func declareHTTPErrorResponses() {
	Response(string(oops.CodeUnauthorized), StatusUnauthorized, func() { ContentType("application/json") })
	Response(string(oops.CodeForbidden), StatusForbidden, func() { ContentType("application/json") })
	Response(string(oops.CodeBadRequest), StatusBadRequest, func() { ContentType("application/json") })
	Response(string(oops.CodeNotFound), StatusNotFound, func() { ContentType("application/json") })
	Response(string(oops.CodeUnsupportedMedia), StatusUnsupportedMediaType, func() { ContentType("application/json") })
	Response(string(oops.CodeInvalid), StatusUnprocessableEntity, func() { ContentType("application/json") })
	Response(string(oops.CodeInvariantViolation), StatusInternalServerError, func() { ContentType("application/json") })
	Response(string(oops.CodeUnexpected), StatusInternalServerError, func() { ContentType("application/json") })
	Response(string(oops.CodeGatewayError), StatusBadGateway, func() { ContentType("application/json") })
}

var _ = Service("killswitches", func() {
	Description("Manage MCP tool-call killswitches for users in the active organization. Requires an ordinary live organization-administrator session.")
	Security(security.Session)
	declareErrors()
	HTTP(func() {
		declareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() { ContentType("application/json") })
	})

	Method("listCapabilities", func() {
		Payload(func() { security.SessionPayload() })
		Result(ListCapabilitiesResult)
		HTTP(func() { GET("/rpc/killswitches.listCapabilities"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("listMCPServers", func() {
		Payload(func() { security.SessionPayload() })
		Result(ListMCPServersResult)
		HTTP(func() { GET("/rpc/killswitches.listMCPServers"); security.SessionHeader(); Response(StatusOK) })
	})

	Method("list", func() {
		Payload(func() {
			security.SessionPayload()
			Attribute("capability_key", CapabilityKey)
			Attribute("user_id", String)
			Attribute("status", Status)
			Attribute("limit", Int32, func() { Minimum(1); Maximum(100) })
			Attribute("cursor", String)
		})
		Result(ListResult)
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
		declareOperationConflict()
		Payload(func() {
			security.SessionPayload()
			Extend(CreateForm)
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/killswitches.create")
			security.SessionHeader()
			Body(CreateForm)
			Response(StatusOK)
			Response("operation_conflict", StatusConflict, func() { ContentType("application/json") })
		})
	})

	Method("edit", func() {
		declareMutationErrors()
		Payload(func() {
			security.SessionPayload()
			Extend(EditForm)
		})
		Result(MutationResult)
		HTTP(func() {
			POST("/rpc/killswitches.edit")
			security.SessionHeader()
			Body(EditForm)
			Response(StatusOK)
			Response("operation_conflict", StatusConflict, func() { ContentType("application/json") })
			Response("version_conflict", StatusConflict, func() { ContentType("application/json") })
		})
	})

	Method("lift", func() {
		declareMutationErrors()
		Payload(func() {
			security.SessionPayload()
			Extend(LiftForm)
		})
		Result(LiftResult)
		HTTP(func() {
			POST("/rpc/killswitches.lift")
			security.SessionHeader()
			Body(LiftForm)
			Response(StatusOK)
			Response("operation_conflict", StatusConflict, func() { ContentType("application/json") })
			Response("version_conflict", StatusConflict, func() { ContentType("application/json") })
		})
	})

	Method("previewOverlaps", func() {
		Payload(func() {
			security.SessionPayload()
			Extend(PreviewOverlapsForm)
		})
		Result(PreviewOverlapsResult)
		HTTP(func() {
			POST("/rpc/killswitches.previewOverlaps")
			security.SessionHeader()
			Body(PreviewOverlapsForm)
			Response(StatusOK)
		})
	})

	Method("batchUserBadges", func() {
		Payload(func() {
			security.SessionPayload()
			Extend(BatchUserBadgesForm)
		})
		Result(BatchUserBadgesResult)
		HTTP(func() {
			POST("/rpc/killswitches.batchUserBadges")
			security.SessionHeader()
			Body(BatchUserBadgesForm)
			Response(StatusOK)
		})
	})
})
