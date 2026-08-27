package dataexports

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("dataExports", func() {
	Description("Manage project-scoped OTEL destinations and data export routes.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	shared.DeclareErrorResponses()

	Method("listOtelDestinations", func() {
		Description("List OTEL destinations for the selected project.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListOtelDestinationsResult)
		HTTP(func() {
			GET("/rpc/dataExports.listOtelDestinations")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listOtelDestinations")
		Meta("openapi:extension:x-speakeasy-name-override", "listOtelDestinations")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OtelDestinations"}`)
	})

	Method("createOtelDestination", func() {
		Description("Create an OTEL destination in the selected project.")
		Payload(func() {
			Extend(CreateOtelDestinationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(OtelDestination)
		HTTP(func() {
			POST("/rpc/dataExports.createOtelDestination")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createOtelDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "createOtelDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOtelDestination"}`)
	})

	Method("updateOtelDestination", func() {
		Description("Replace an OTEL destination in the selected project.")
		Payload(func() {
			Extend(UpdateOtelDestinationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(OtelDestination)
		HTTP(func() {
			POST("/rpc/dataExports.updateOtelDestination")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateOtelDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "updateOtelDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateOtelDestination"}`)
	})

	Method("deleteOtelDestination", func() {
		Description("Delete an OTEL destination that is not referenced by an active route.")
		Payload(func() {
			Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Required("id")
		})
		HTTP(func() {
			DELETE("/rpc/dataExports.deleteOtelDestination")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "deleteOtelDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteOtelDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOtelDestination"}`)
	})

	Method("listRoutes", func() {
		Description("List data export routes for the selected project.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListDataExportRoutesResult)
		HTTP(func() {
			GET("/rpc/dataExports.listRoutes")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listDataExportRoutes")
		Meta("openapi:extension:x-speakeasy-name-override", "listRoutes")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DataExportRoutes"}`)
	})

	Method("createRoute", func() {
		Description("Create a data export route in the selected project.")
		Payload(func() {
			Extend(CreateDataExportRouteForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(DataExportRoute)
		HTTP(func() {
			POST("/rpc/dataExports.createRoute")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createDataExportRoute")
		Meta("openapi:extension:x-speakeasy-name-override", "createRoute")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateDataExportRoute"}`)
	})

	Method("updateRoute", func() {
		Description("Replace a data export route in the selected project.")
		Payload(func() {
			Extend(UpdateDataExportRouteForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(DataExportRoute)
		HTTP(func() {
			POST("/rpc/dataExports.updateRoute")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateDataExportRoute")
		Meta("openapi:extension:x-speakeasy-name-override", "updateRoute")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateDataExportRoute"}`)
	})

	Method("deleteRoute", func() {
		Description("Delete a data export route in the selected project.")
		Payload(func() {
			Attribute("id", String, "Route ID.", func() { Format(FormatUUID) })
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Required("id")
		})
		HTTP(func() {
			DELETE("/rpc/dataExports.deleteRoute")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "deleteDataExportRoute")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteRoute")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteDataExportRoute"}`)
	})
})

var OtelDestinationHeader = Type("OtelDestinationHeader", func() {
	Description("A write-only HTTP header configured on an OTEL destination.")
	Attribute("name", String, "Header name.")
	Attribute("has_value", Boolean, "Whether a non-empty encrypted value is stored for this header.")
	Required("name", "has_value")
})

var CreateOtelDestinationHeaderInput = Type("CreateOtelDestinationHeaderInput", func() {
	Description("An HTTP header supplied when creating an OTEL destination.")
	Attribute("name", String, "Header name.")
	Attribute("value", String, "Write-only header value.")
	Required("name", "value")
})

var OtelDestinationHeaderInput = Type("OtelDestinationHeaderInput", func() {
	Description("An HTTP header supplied when updating an OTEL destination. Omit value to preserve the encrypted value stored for the same case-insensitive name.")
	Attribute("name", String, "Header name.")
	Attribute("value", String, "Write-only header value. Omit to preserve an existing value; provide an empty string to clear it.")
	Required("name")
})

var OtelDestination = Type("OtelDestination", func() {
	Description("A reusable customer-owned OTLP collector connection.")
	Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Project that owns the destination.", func() { Format(FormatUUID) })
	Attribute("endpoint_url", String, "OTLP base URL. Signal-specific paths are appended during delivery.", func() { Format(FormatURI) })
	Attribute("sensitive_data", String, "Whether sensitive data is included in payloads sent to this destination.", func() {
		Enum("exclude", "include")
	})
	Attribute("headers", ArrayOf(OtelDestinationHeader), "Configured header names. Header values are never returned.")
	Attribute("created_at", String, "Creation timestamp.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update timestamp.", func() { Format(FormatDateTime) })
	Required("id", "project_id", "endpoint_url", "sensitive_data", "headers", "created_at", "updated_at")
})

var DataExportRoute = Type("DataExportRoute", func() {
	Description("A route from one class of project data to an OTEL destination.")
	Attribute("id", String, "Route ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Project that owns the route.", func() { Format(FormatUUID) })
	Attribute("data_source", String, "Class of data exported by this route.", func() {
		Enum("otel_forwarding")
	})
	Attribute("enabled", Boolean, "Whether the route is enabled.")
	Attribute("otel_destination_id", String, "OTEL destination used by this route. Omitted when no destination is selected.", func() { Format(FormatUUID) })
	Attribute("created_at", String, "Creation timestamp.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update timestamp.", func() { Format(FormatDateTime) })
	Required("id", "project_id", "data_source", "enabled", "created_at", "updated_at")
})

var CreateOtelDestinationForm = Type("CreateOtelDestinationForm", func() {
	Description("Form for creating an OTEL destination.")
	Attribute("endpoint_url", String, "OTLP base URL.", func() { Format(FormatURI) })
	Attribute("sensitive_data", String, "Sensitive-data policy.", func() {
		Enum("exclude", "include")
		Default("exclude")
	})
	Attribute("headers", ArrayOf(CreateOtelDestinationHeaderInput), "Write-only headers.")
	Required("endpoint_url", "headers")
})

var UpdateOtelDestinationForm = Type("UpdateOtelDestinationForm", func() {
	Description("Full replacement form for an OTEL destination. Header entries with omitted values preserve existing encrypted values by case-insensitive name.")
	Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
	Attribute("endpoint_url", String, "OTLP base URL.", func() { Format(FormatURI) })
	Attribute("sensitive_data", String, "Sensitive-data policy.", func() { Enum("exclude", "include") })
	Attribute("headers", ArrayOf(OtelDestinationHeaderInput), "Complete header-name set for the destination.")
	Required("id", "endpoint_url", "sensitive_data", "headers")
})

var CreateDataExportRouteForm = Type("CreateDataExportRouteForm", func() {
	Description("Form for creating a data export route.")
	Attribute("data_source", String, "Class of data exported by this route.", func() { Enum("otel_forwarding") })
	Attribute("enabled", Boolean, "Whether the route is enabled.", func() { Default(true) })
	Attribute("otel_destination_id", String, "OTEL destination used by this route. Required when enabled.", func() { Format(FormatUUID) })
	Required("data_source")
})

var UpdateDataExportRouteForm = Type("UpdateDataExportRouteForm", func() {
	Description("Full replacement form for a data export route. Omit otel_destination_id to clear the destination.")
	Attribute("id", String, "Route ID.", func() { Format(FormatUUID) })
	Attribute("data_source", String, "Class of data exported by this route.", func() { Enum("otel_forwarding") })
	Attribute("enabled", Boolean, "Whether the route is enabled.")
	Attribute("otel_destination_id", String, "OTEL destination used by this route. Required when enabled.", func() { Format(FormatUUID) })
	Required("id", "data_source", "enabled")
})

var ListOtelDestinationsResult = Type("ListOtelDestinationsResult", func() {
	Attribute("destinations", ArrayOf(OtelDestination), "Active OTEL destinations in the selected project.")
	Required("destinations")
})

var ListDataExportRoutesResult = Type("ListDataExportRoutesResult", func() {
	Attribute("routes", ArrayOf(DataExportRoute), "Active data export routes in the selected project.")
	Required("routes")
})
