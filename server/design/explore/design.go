package explore

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var ExploreQuery = Type("ExploreQuery", func() {
	Description("A named, saved question against a catalog dataset, kept with the builder state it was built with. Belongs to a project and is visible to everyone in the organization.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("organization_id", String)
	Attribute("created_by_user_id", String, "Who saved it, when known")
	Attribute("name", String, "Display name. Not unique: two people saving the same name is normal.")
	Attribute("dataset", String, "The catalog dataset the query asks", func() { Example("sessions") })
	Attribute("spec", MapOf(String, Any), "Builder state: chart type, window, dimensions, measures, filters, grain, sort and limit. The client interprets it; the server validates the parts the catalog knows.")
	Attribute("invalid_reason", String, "Present when the saved spec no longer compiles against the catalog, naming what is wrong, so a catalog change fails visibly rather than returning wrong numbers.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "project_id", "organization_id", "name", "dataset", "spec", "created_at", "updated_at")
})

var ListQueriesResult = Type("ExploreListQueriesResult", func() {
	Attribute("queries", ArrayOf(ExploreQuery), "Queries in the project, most recently updated first")
	Required("queries")
})

func queryForm() {
	Attribute("name", String, "Display name, at most 200 characters", func() {
		MinLength(1)
		MaxLength(200)
	})
	Attribute("dataset", String, "Catalog dataset the query asks", func() { MinLength(1) })
	Attribute("spec", MapOf(String, Any), "Builder state. Validated against the catalog on save.")
}

var _ = Service("explore", func() {
	Description("Queries: the saved questions Explore keeps. Explore's only server-side surface; everything it shows comes from the analytics service.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listQueries", func() {
		Description("List the project's saved queries, most recently updated first. Each is validated against the catalog on read, so a query a catalog change broke says so.")
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListQueriesResult)
		HTTP(func() {
			GET("/rpc/explore.listQueries")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listExploreQueries")
		Meta("openapi:extension:x-speakeasy-name-override", "listQueries")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreQueries", "type": "query"}`)
	})

	Method("createQuery", func() {
		Description("Save a query. Any member of the project can. The spec is validated against the catalog before it is stored.")
		Payload(func() {
			queryForm()
			Required("name", "dataset", "spec")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ExploreQuery)
		HTTP(func() {
			POST("/rpc/explore.createQuery")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createExploreQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "createQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateExploreQuery"}`)
	})

	Method("updateQuery", func() {
		Description("Replace a saved query's name, dataset and spec. Any member of the project can.")
		Payload(func() {
			Attribute("id", String, "The query to update", func() { Format(FormatUUID) })
			queryForm()
			Required("id", "name", "dataset", "spec")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ExploreQuery)
		HTTP(func() {
			POST("/rpc/explore.updateQuery")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateExploreQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "updateQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateExploreQuery"}`)
	})

	Method("deleteQuery", func() {
		Description("Delete a saved query. Its creator can; deleting someone else's needs project write access.")
		Payload(func() {
			Attribute("id", String, "The query to delete", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		HTTP(func() {
			DELETE("/rpc/explore.deleteQuery")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteExploreQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteExploreQuery"}`)
	})
})
