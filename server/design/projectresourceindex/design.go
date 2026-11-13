package projectresourceindex

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("projectResourceIndex", func() {
	Description("Returns the complete authorized, openable resource inventory for a project.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug)
	shared.DeclareErrorResponses()
	Method("get", func() {
		Description("Returns a complete snapshot of authorized project resources for local discovery.")
		Payload(func() { security.SessionPayload(); security.ByKeyPayload(); security.ProjectPayload() })
		Result(ProjectResourceIndexResult)
		HTTP(func() {
			GET("/rpc/projectResourceIndex.get")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getProjectResourceIndex")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ProjectResourceIndex"}`)
	})
})

var ProjectResourceIndexResult = ResultType("ProjectResourceIndex", func() {
	Attribute("schema_version", String, "Version of the project resource index schema.", func() { Enum("1") })
	Attribute("generated_at", String, "RFC 3339 snapshot generation time.", func() { Format(FormatDateTime) })
	Attribute("items", ArrayOf(ProjectResourceIndexItem))
	Attribute("kinds", ArrayOf(ProjectResourceIndexKind), "Completeness metadata for every supported resource kind.")
	Required("schema_version", "generated_at", "items", "kinds")
})

var ProjectResourceIndexKind = ResultType("ProjectResourceIndexKind", func() {
	Attribute("kind", String, func() { Enum("assistant", "environment", "gateway", "mcp", "plugin", "rule", "skill") })
	Attribute("complete", Boolean, "Whether all authorized and openable resources of this kind are present.")
	Attribute("overflow", Boolean, "Whether a safety boundary omitted resources. This endpoint currently has no count boundary and always returns false.")
	Attribute("item_count", Int, "Number of returned resources of this kind.")
	Required("kind", "complete", "overflow", "item_count")
})

var ProjectResourceIndexItem = ResultType("ProjectResourceIndexItem", func() {
	Attribute("kind", String, func() { Enum("assistant", "environment", "gateway", "mcp", "plugin", "rule", "skill") })
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("route_selector", String, "Canonical selector used by the dashboard route for this resource kind.")
	Attribute("keywords", ArrayOf(String), "Compact alternate names used for local matching.")
	Required("kind", "id", "name", "route_selector")
})
