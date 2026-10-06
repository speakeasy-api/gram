package dashboards

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var DashboardRange = Type("DashboardRange", func() {
	Description("The date range a dashboard opens on: a relative window, or an absolute range with the label the date picker gave it.")
	Attribute("preset", String, "A relative window, one a widget may be saved with", func() { Example("7d") })
	Attribute("from", String, "Start of an absolute range", func() { Format(FormatDateTime) })
	Attribute("to", String, "End of an absolute range", func() { Format(FormatDateTime) })
	Attribute("label", String, "How the date picker named the absolute range", func() { Example("Last Tuesday") })
})

var DashboardFilters = Type("DashboardFilters", func() {
	Description("What a dashboard opens on: its date range and the values picked for each catalog dimension its filter bar offers. Changes in the bar are a personal view until they are saved here.")
	Attribute("range", DashboardRange, "The date range; absent means the page's default")
	Attribute("values", MapOf(String, ArrayOf(String)), "Values picked per catalog dimension, by field name")
	Required("values")
})

var DashboardPlacement = Type("DashboardPlacement", func() {
	Description("One card on a dashboard: the saved widget it links to and where it sits on the 12-column grid. The same widget may be placed more than once.")
	Attribute("id", String, "Stable across layout edits, so the grid keeps the card's identity", func() { Format(FormatUUID) })
	Attribute("widget_id", String, func() { Format(FormatUUID) })
	Attribute("x", Int, "Column the card starts at, from 0")
	Attribute("y", Int, "Row the card starts at, from 0")
	Attribute("w", Int, "Width in columns")
	Attribute("h", Int, "Height in rows")
	Required("id", "widget_id", "x", "y", "w", "h")
})

var Dashboard = Type("Dashboard", func() {
	Description("A project's layout of saved widgets on a 12-column grid, under one date range and filter bar. Belongs to a project and is visible to everyone in it. Widgets are linked, not copied: editing one changes it on every dashboard it is on.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("organization_id", String)
	Attribute("created_by_user_id", String, "Who made it, when known")
	Attribute("name", String, "Display name. Not unique.")
	Attribute("description", String, "What the dashboard is for, when its creator said")
	Attribute("filters", DashboardFilters, "The saved date range and filter values it opens on")
	Attribute("widgets", ArrayOf(DashboardPlacement), "Its cards, in no particular order; the grid places them by position")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
	Required("id", "project_id", "organization_id", "name", "filters", "widgets", "created_at", "updated_at")
})

var ListDashboardsResult = Type("ListDashboardsResult", func() {
	Attribute("dashboards", ArrayOf(Dashboard), "Dashboards in the project, most recently updated first")
	Required("dashboards")
})

var PlacementInput = Type("PlacementInput", func() {
	Description("A card's place in a layout being saved. With an id it moves or resizes the existing card; without one it adds the widget as a new card.")
	Attribute("id", String, "The existing placement, when the card is already on the dashboard", func() { Format(FormatUUID) })
	Attribute("widget_id", String, func() { Format(FormatUUID) })
	Attribute("x", Int, "Column the card starts at, on a 12-column grid", func() {
		Minimum(0)
		Maximum(11)
	})
	Attribute("y", Int, "Row the card starts at", func() {
		Minimum(0)
		Maximum(9999)
	})
	Attribute("w", Int, "Width in columns; each chart type also has a minimum, so x + w stays within 12", func() {
		Minimum(1)
		Maximum(12)
	})
	Attribute("h", Int, "Height in rows; each chart type also has a minimum", func() {
		Minimum(1)
		Maximum(10000)
	})
	Required("widget_id", "x", "y", "w", "h")
})

func dashboardForm() {
	Attribute("name", String, "Display name, at most 200 characters", func() {
		MinLength(1)
		MaxLength(200)
	})
	Attribute("description", String, "What the dashboard is for, at most 2000 characters", func() {
		MaxLength(2000)
	})
}

func dashboardID(description string) {
	Attribute("id", String, description, func() { Format(FormatUUID) })
}

var _ = Service("dashboards", func() {
	Description("Dashboards: a project's layouts of saved widgets. Any member can make one; editing someone else's needs project write access.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listDashboards", func() {
		Description("List the project's dashboards, most recently updated first, each with its cards.")
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListDashboardsResult)
		HTTP(func() {
			GET("/rpc/dashboards.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listDashboards")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Dashboards", "type": "query"}`)
	})

	Method("getDashboard", func() {
		Description("Get one dashboard by id, with its cards and saved filters.")
		Payload(func() {
			dashboardID("The dashboard to get")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			GET("/rpc/dashboards.get")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getDashboard")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Dashboard", "type": "query"}`)
	})

	Method("createDashboard", func() {
		Description("Make an empty dashboard. Any member of the project can.")
		Payload(func() {
			dashboardForm()
			Required("name")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createDashboard")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateDashboard"}`)
	})

	Method("updateDashboard", func() {
		Description("Rename a dashboard or change its description. Its creator can; editing someone else's needs project write access.")
		Payload(func() {
			dashboardID("The dashboard to update")
			dashboardForm()
			Required("id", "name")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.update")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateDashboard")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateDashboard"}`)
	})

	Method("saveDashboardLayout", func() {
		Description("Replace a dashboard's layout with the given cards. A card with an id moves or resizes the existing placement, one without an id is added, and any placement not listed is removed. The grid is 12 columns wide, and each chart type has a minimum size. Layout autosaves, so the last save wins.")
		Payload(func() {
			dashboardID("The dashboard to lay out")
			Attribute("placements", ArrayOf(PlacementInput), "Every card and where it sits; a dashboard holds at most 100", func() { MaxLength(100) })
			Required("id", "placements")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.saveLayout")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "saveDashboardLayout")
		Meta("openapi:extension:x-speakeasy-name-override", "saveLayout")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SaveDashboardLayout"}`)
	})

	Method("addDashboardWidget", func() {
		Description("Place a saved widget on a dashboard, as a new card at the bottom, sized for its chart type.")
		Payload(func() {
			dashboardID("The dashboard to place it on")
			Attribute("widget_id", String, "The widget to place", func() { Format(FormatUUID) })
			Required("id", "widget_id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.addWidget")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "addDashboardWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "addWidget")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddDashboardWidget"}`)
	})

	Method("removeDashboardWidget", func() {
		Description("Take a card off a dashboard. The widget itself stays saved.")
		Payload(func() {
			dashboardID("The dashboard the card is on")
			Attribute("placement_id", String, "The card to remove", func() { Format(FormatUUID) })
			Required("id", "placement_id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.removeWidget")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "removeDashboardWidget")
		Meta("openapi:extension:x-speakeasy-name-override", "removeWidget")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveDashboardWidget"}`)
	})

	Method("saveDashboardFilters", func() {
		Description("Store the date range and filter values a dashboard opens on, for everyone. Until saved, changes in the filter bar are the viewer's own.")
		Payload(func() {
			dashboardID("The dashboard to save filters on")
			Attribute("filters", DashboardFilters)
			Required("id", "filters")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.saveFilters")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "saveDashboardFilters")
		Meta("openapi:extension:x-speakeasy-name-override", "saveFilters")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SaveDashboardFilters"}`)
	})

	Method("duplicateDashboard", func() {
		Description("Copy a dashboard into a new one the caller owns, named \"<name> (copy)\". Every card's widget is copied into a new saved widget too, so the copy is fully independent of the original. Like every widget save, each copy is validated, so a dashboard with a broken widget cannot be duplicated until the widget is fixed.")
		Payload(func() {
			dashboardID("The dashboard to copy")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
			Meta("openapi:typename", "DuplicateDashboardRequestBody")
		})
		Result(Dashboard)
		HTTP(func() {
			POST("/rpc/dashboards.duplicate")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "duplicateDashboard")
		Meta("openapi:extension:x-speakeasy-name-override", "duplicate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DuplicateDashboard"}`)
	})

	Method("deleteDashboard", func() {
		Description("Delete a dashboard and its cards. Its widgets stay saved. Its creator can; deleting someone else's needs project write access.")
		Payload(func() {
			dashboardID("The dashboard to delete")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		HTTP(func() {
			DELETE("/rpc/dashboards.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteDashboard")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteDashboard"}`)
	})
})
