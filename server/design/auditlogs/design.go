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

	Method("listByProject", func() {
		Description("List project logs for a given project.")

		Payload(func() {
			Extend(ListProjectAuditLogsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})
		Result(ListProjectAuditLogsResult)

		HTTP(func() {
			GET("/rpc/auditlogs.listByProject")
			security.ByKeyHeader()
			security.SessionHeader()
			Param("cursor")
			Param("project_slug")
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listProjectAuditLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "listByProject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ProjectAuditLogs"}`)
	})
})

var AuditLog = Type("AuditLog", func() {
	Required("id", "actor_id", "actor_type", "action", "subject_id", "subject_type", "created_at")

	Attribute("id", String)

	Attribute("actor_id", String)
	Attribute("actor_type", String)
	Attribute("actor_display_name", String)
	Attribute("actor_slug", String)

	Attribute("action", String)

	Attribute("subject_id", String)
	Attribute("subject_type", String)
	Attribute("subject_display_name", String)
	Attribute("subject_slug", String)

	Attribute("before_snapshot", String, func() {
		Meta("struct:field:type", "json.RawMessage")
	})
	Attribute("after_snapshot", String, func() {
		Meta("struct:field:type", "json.RawMessage")
	})

	Attribute("metadata", MapOf(String, Any))

	Attribute("created_at", String, func() {
		Description("The creation date of the audit log.")
		Format(FormatDateTime)
	})
})

var ListProjectAuditLogsForm = Type("ListProjectAuditLogsForm", func() {
	Required("project_slug")

	Attribute("cursor", String, func() {
		Description("The cursor for paginating through audit logs.")
	})
	Attribute("project_slug", String, func() {
		Description("Project slug to filter audit logs to a specific project.")
	})
})

var ListProjectAuditLogsResult = Type("ListProjectAuditLogsResult", func() {
	Required("logs")
	Attribute("logs", ArrayOf(AuditLog), "List of audit logs")
	Attribute("next_cursor", String, func() {
		Description("The cursor to be used for the next page of results.")
	})
})
