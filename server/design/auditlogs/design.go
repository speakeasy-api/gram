package auditlogs

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("auditlogs", func() {
	Description("Manages audit logs in Gram.")

	Security(security.ByKey, func() {
		Scope("producer")
	})
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("list", func() {
		Description("List audit logs across organization and projects.")

		Payload(func() {
			Extend(ListAuditLogsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})
		Result(ListAuditLogsResult)

		HTTP(func() {
			GET("/rpc/auditlogs.list")
			security.ByKeyHeader()
			security.SessionHeader()
			Param("cursor")
			Param("project_slug")
			Param("actor_id")
			Param("action")
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listAuditLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AuditLogs"}`)
	})

	Method("listFacets", func() {
		Description("List available audit log facet values across organization and projects.")

		Payload(func() {
			Extend(ListAuditLogFacetsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})
		Result(ListAuditLogFacetsResult)

		HTTP(func() {
			GET("/rpc/auditlogs.listFacets")
			security.ByKeyHeader()
			security.SessionHeader()
			Param("project_slug")
		})

		Meta("openapi:operationId", "listAuditLogFacets")
		Meta("openapi:extension:x-speakeasy-name-override", "listFacets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AuditLogFacets"}`)
	})
})

var AuditLog = Type("AuditLog", func() {
	Required("id", "actor_id", "actor_type", "action", "subject_id", "subject_type", "created_at")

	Attribute("id", String)
	Attribute("project_id", String)
	Attribute("project_slug", String)

	Attribute("actor_id", String)
	Attribute("actor_type", String)
	Attribute("actor_display_name", String)
	Attribute("actor_slug", String)

	Attribute("action", String)

	Attribute("subject_id", String)
	Attribute("subject_type", String)
	Attribute("subject_display_name", String)
	Attribute("subject_slug", String)

	Attribute("before_snapshot", Any, func() {
		Meta("struct:field:type", "json.RawMessage")
	})
	Attribute("after_snapshot", Any, func() {
		Meta("struct:field:type", "json.RawMessage")
	})

	Attribute("metadata", MapOf(String, Any))

	Attribute("created_at", String, func() {
		Description("The creation date of the audit log.")
		Format(FormatDateTime)
	})
})

var ListAuditLogsForm = Type("ListAuditLogsForm", func() {
	Attribute("cursor", String, func() {
		Description("The cursor for paginating through audit logs.")
	})
	Attribute("project_slug", String, func() {
		Description("Project slug to filter audit logs to a specific project.")
	})
	Attribute("actor_id", String, func() {
		Description("Actor ID to filter audit logs to a specific actor.")
	})
	Attribute("action", String, func() {
		Description("Action to filter audit logs to a specific action.")
	})
})

var ListAuditLogsResult = Type("ListAuditLogsResult", func() {
	Required("logs")
	Attribute("logs", ArrayOf(AuditLog), "List of audit logs")
	Attribute("next_cursor", String, func() {
		Description("The cursor to be used for the next page of results.")
	})
})

var AuditLogFacetOption = Type("AuditLogFacetOption", func() {
	Attribute("value", String, "The facet value used for filtering")
	Attribute("display_name", String, "The display label shown for the facet value")
	Attribute("count", Int64, "The number of audit logs for this facet value")

	Required("value", "display_name", "count")
})

var ListAuditLogFacetsForm = Type("ListAuditLogFacetsForm", func() {
	Attribute("project_slug", String, func() {
		Description("Project slug to filter facet values to a specific project.")
	})
})

var ListAuditLogFacetsResult = Type("ListAuditLogFacetsResult", func() {
	Required("actors", "actions")
	Attribute("actors", ArrayOf(AuditLogFacetOption), "Available actor facets")
	Attribute("actions", ArrayOf(AuditLogFacetOption), "Available action facets")
})
