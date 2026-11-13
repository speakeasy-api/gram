package commandsearch

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("commandSearch", func() {
	Description("Searches authorized project resources for Command K.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("search", func() {
		Description("Returns compact openable resources matching a bounded server-side query.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("q", String, "Case-insensitive resource name or alias query.", func() { MinLength(2); MaxLength(128) })
			Required("q")
		})
		Result(ResourceIndexResult)
		HTTP(func() {
			GET("/rpc/commandSearch.search")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("q")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "searchCommandResources")
		Meta("openapi:extension:x-speakeasy-name-override", "searchResources")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchCommandResources"}`)
	})
})

var ResourceIndexResult = ResultType("CommandResourceIndex", func() {
	Attribute("schema_version", String, "Version of the compact candidate schema.", func() { Enum("1") })
	Attribute("generated_at", String, "RFC 3339 snapshot generation time.", func() { Format(FormatDateTime) })
	Attribute("items", ArrayOf(ResourceIndexItem))
	Attribute("partial", Boolean, "Whether the deadline prevented one or more resource kinds from completing.")
	Attribute("timed_out", Boolean, "Whether the shared search deadline expired.")
	Attribute("failed_kinds", ArrayOf(String), "Resource kinds that did not finish before the deadline.")
	Required("schema_version", "generated_at", "items", "partial", "timed_out", "failed_kinds")
})

var ResourceIndexItem = ResultType("CommandResourceIndexItem", func() {
	Attribute("kind", String, func() { Enum("environment", "gateway", "plugin", "rule") })
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("route_selector", String, "Canonical selector used by the dashboard route for this resource kind.")
	Attribute("keywords", ArrayOf(String), "Compact alternate names used for local matching.")
	Required("kind", "id", "name", "route_selector")
})
