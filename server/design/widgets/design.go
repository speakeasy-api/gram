package widgets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var Widget = Type("Widget", func() {
	Description("A named question against a catalog dataset together with how it is drawn. Belongs to a project and is visible to everyone in the organization. Widgets are the building block dashboards will place.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("organization_id", String)
	Attribute("created_by_user_id", String, "Who saved it, when known")
	Attribute("name", String, "Display name. Not unique: two people saving the same name is normal.")
	Attribute("description", String, "What the widget is for, when its creator said")
	Attribute("dataset", String, "The catalog dataset the widget asks", func() { Example("sessions") })
	Attribute("query", MapOf(String, Any), "The question: window, grain, dimensions, measures, filters, order and limit. Planned against the catalog on save and on read.")
	Attribute("visualization", MapOf(String, Any), "How the question is drawn: a chart type and its options. The client owns the chart vocabulary; the server checks only that the chart can draw the question.")
	Attribute("invalid_reason", String, "Present when the widget no longer works, naming what is wrong: a field the catalog dropped, or a chart that cannot draw the question. A catalog change fails visibly rather than returning wrong numbers.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "project_id", "organization_id", "name", "dataset", "query", "visualization", "created_at", "updated_at")
})

var ListWidgetsResult = Type("ListWidgetsResult", func() {
	Attribute("widgets", ArrayOf(Widget), "Widgets in the project, most recently updated first")
	Required("widgets")
})

func widgetForm() {
	Attribute("name", String, "Display name, at most 200 characters", func() {
		MinLength(1)
		MaxLength(200)
	})
	Attribute("description", String, "What the widget is for, at most 2000 characters", func() {
		MaxLength(2000)
	})
	Attribute("dataset", String, "Catalog dataset the widget asks", func() { MinLength(1) })
	Attribute("query", MapOf(String, Any), "The question. Validated against the catalog on save.")
	Attribute("visualization", MapOf(String, Any), "How the question is drawn. Checked against the question on save.")
}

func widgetID(description string) {
	Attribute("id", String, description, func() { Format(FormatUUID) })
}

var _ = Service("widgets", func() {
	Description("Widgets: Explore's saved questions, each kept with the chart that draws it.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listWidgets", func() {
		Description("List the project's widgets, most recently updated first. Each is validated as it is read, so a widget a catalog change broke says so.")
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListWidgetsResult)
		HTTP(func() {
			GET("/rpc/widgets.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listWidgets")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Widgets", "type": "query"}`)
	})

	Method("getWidget", func() {
		Description("Get one widget by id, validated as it is read.")
		Payload(func() {
			widgetID("The widget to get")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Widget)
		HTTP(func() {
			GET("/rpc/widgets.get")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Widget", "type": "query"}`)
	})

	Method("createWidget", func() {
		Description("Save a widget. Any member of the project can. The question is validated against the catalog, and the chart against the question, before it is stored.")
		Payload(func() {
			widgetForm()
			Required("name", "dataset", "query", "visualization")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Widget)
		HTTP(func() {
			POST("/rpc/widgets.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateWidget"}`)
	})

	Method("updateWidget", func() {
		Description("Replace a widget's name, description, dataset, query and visualization. Any member of the project can.")
		Payload(func() {
			widgetID("The widget to update")
			widgetForm()
			Required("id", "name", "dataset", "query", "visualization")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Widget)
		HTTP(func() {
			POST("/rpc/widgets.update")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateWidget"}`)
	})

	Method("duplicateWidget", func() {
		Description("Copy a widget into a new one the caller owns, named \"<name> (copy)\". This is how a widget is shared: a teammate duplicates it rather than editing it. Like every save, the copy is validated, so a broken widget cannot be duplicated until it is fixed.")
		Payload(func() {
			widgetID("The widget to copy")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
			// Named explicitly: an id-only body otherwise dedupes onto an
			// unrelated schema of the same shape in the generated SDK.
			Meta("openapi:typename", "DuplicateWidgetRequestBody")
		})
		Result(Widget)
		HTTP(func() {
			POST("/rpc/widgets.duplicate")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "duplicateWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "duplicate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DuplicateWidget"}`)
	})

	Method("deleteWidget", func() {
		Description("Delete a widget. Its creator can; deleting someone else's needs project write access.")
		Payload(func() {
			widgetID("The widget to delete")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		HTTP(func() {
			DELETE("/rpc/widgets.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteWidget"}`)
	})
})
