package usersessions

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("gatewaySessions", func() {
	Description("Sessions authorized for a project gateway or the Organization Gateway. Issuer-scoped sessions may authorize several endpoints; resource-bound sessions are restricted to the selected endpoint. Server filters describe configured upstream associations, not historical usage.")
	Security(security.Session)
	Security(security.ByKey, func() { Scope("producer") })
	shared.DeclareErrorResponses()
	Method("listGatewaySessions", func() {
		Payload(func() {
			gatewayScopePayload()
			Attribute("search", String, "Case-insensitive literal search by subject URN, name, or email.", func() { MaxLength(200) })
			Attribute("server_id", String, "Filter by an upstream account associated with this member server.", func() { Format(FormatUUID) })
			Attribute("provider_id", String, "Filter by upstream provider issuer.", func() { Format(FormatUUID) })
			Attribute("status", String, "Inbound authorization status.", func() { Enum("active", "expired", "revoked", "all"); Default("active") })
			Attribute("created_after", String, "Inclusive creation time lower bound.", func() { Format(FormatDateTime) })
			Attribute("created_before", String, "Exclusive creation time upper bound.", func() { Format(FormatDateTime) })
			Attribute("sort", String, "Ordering; expiry uses the inbound authorization deadline, not access-token expiry. Unknown last-use times sort last.", func() { Enum("newest", "oldest", "last_used", "expiry"); Default("newest") })
			Attribute("cursor", String, "Opaque cursor bound to the current filters and ordering.")
			Attribute("limit", Int, "Page size.", func() { Minimum(1); Maximum(100); Default(50) })
		})
		Result(GatewaySessionsResult)
		HTTP(func() {
			GET("/rpc/gatewaySessions.list")
			gatewayScopeHeaders()
			Param("search")
			Param("server_id")
			Param("provider_id")
			Param("status")
			Param("created_after")
			Param("created_before")
			Param("sort")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listGatewaySessions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GatewaySessions"}`)
	})
	Method("listGatewaySessionFacets", func() {
		Description("Complete configured member server and provider choices, independent of pagination and active filters.")
		Payload(gatewayScopePayload)
		Result(GatewaySessionFacets)
		HTTP(func() { GET("/rpc/gatewaySessions.listFacets"); gatewayScopeHeaders(); Response(StatusOK) })
		Meta("openapi:operationId", "listGatewaySessionFacets")
		Meta("openapi:extension:x-speakeasy-name-override", "listFacets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GatewaySessionFacets"}`)
	})
})

func gatewayScopePayload() {
	Attribute("gateway_id", String, "Project gateway id. Omit for the Organization Gateway; organization access is then required and the selected project is ignored.", func() { Format(FormatUUID) })
	security.SessionPayload()
	security.ByKeyPayload()
}
func gatewayScopeHeaders() {
	Param("gateway_id")
	security.SessionHeader()
	security.ByKeyHeader()
}

var GatewaySessionsResult = Type("GatewaySessionsResult", func() {
	Attribute("items", ArrayOf(UserSession))
	Attribute("next_cursor", String)
	Required("items")
})
var GatewaySessionFacet = Type("GatewaySessionFacet", func() {
	Attribute("value", String)
	Attribute("label", String)
	Required("value", "label")
})
var GatewaySessionFacets = Type("GatewaySessionFacets", func() {
	Attribute("servers", ArrayOf(GatewaySessionFacet))
	Attribute("providers", ArrayOf(GatewaySessionFacet))
	Required("servers", "providers")
})
