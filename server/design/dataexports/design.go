package dataexports

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("dataExports", func() {
	Description("Manage project-scoped data export destinations and routes.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() { Scope("producer") })
	shared.DeclareErrorResponses()

	Method("listDestinations", func() {
		Description("List data export destinations for the selected project.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListDestinationsResult)
		HTTP(func() {
			GET("/rpc/dataExports.listDestinations")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listDataExportDestinations")
		Meta("openapi:extension:x-speakeasy-name-override", "listDestinations")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DataExportDestinations"}`)
	})

	Method("createDestination", func() {
		Description("Create a data export destination in the selected project.")
		Payload(func() {
			Extend(CreateDestinationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(Destination)
		HTTP(func() {
			POST("/rpc/dataExports.createDestination")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createDataExportDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "createDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateDataExportDestination"}`)
	})

	Method("updateDestination", func() {
		Description("Replace a data export destination in the selected project.")
		Payload(func() {
			Extend(UpdateDestinationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(Destination)
		HTTP(func() {
			POST("/rpc/dataExports.updateDestination")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateDataExportDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "updateDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateDataExportDestination"}`)
	})

	Method("deleteDestination", func() {
		Description("Delete a data export destination that is not referenced by an active route.")
		Payload(func() {
			Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
			Attribute("destination_type", String, "Destination transport type.", func() { Enum("otel") })
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Required("id", "destination_type")
		})
		HTTP(func() {
			DELETE("/rpc/dataExports.deleteDestination")
			Param("id")
			Param("destination_type")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "deleteDataExportDestination")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteDestination")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteDataExportDestination"}`)
	})

	Method("listRoutes", func() {
		Description("List data source export route configurations for the selected project.")
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
		Description("Create the selected data source's export route configuration.")
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
		Description("Replace the selected data source's export route configuration.")
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
		Description("Delete a data source's export route configuration.")
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

var UpdateOtelDestinationHeaderInput = Type("UpdateOtelDestinationHeaderInput", func() {
	Description("An HTTP header supplied when updating an OTEL destination. Omit value to preserve the encrypted value stored for the same case-insensitive name.")
	Attribute("name", String, "Header name.")
	Attribute("value", String, "Write-only header value. Omit to preserve an existing value; provide an empty string to clear it.")
	Required("name")
})

var OtelDestination = Type("OtelDestination", func() {
	Description("OTEL-specific destination configuration.")
	Attribute("endpoint_url", String, "OTEL collector endpoint URL. Transport-specific paths may be appended during delivery.", func() { Format(FormatURI) })
	Attribute("headers", ArrayOf(OtelDestinationHeader), "Configured HTTP header names. Header values are never returned.")
	Required("endpoint_url", "headers")
})

var CreateOtelDestinationInput = Type("CreateOtelDestinationInput", func() {
	Description("OTEL-specific configuration supplied when creating a destination.")
	Attribute("endpoint_url", String, "OTEL collector endpoint URL.", func() { Format(FormatURI) })
	Attribute("headers", ArrayOf(CreateOtelDestinationHeaderInput), "Write-only HTTP headers.")
	Required("endpoint_url", "headers")
})

var UpdateOtelDestinationInput = Type("UpdateOtelDestinationInput", func() {
	Description("OTEL-specific configuration supplied when updating a destination.")
	Attribute("endpoint_url", String, "OTEL collector endpoint URL.", func() { Format(FormatURI) })
	Attribute("headers", ArrayOf(UpdateOtelDestinationHeaderInput), "Complete HTTP header-name set for the OTEL destination.")
	Required("endpoint_url", "headers")
})

var Destination = Type("Destination", func() {
	Description("A reusable project-scoped data export destination. The destination_type discriminator identifies the populated transport configuration.")
	Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Project that owns the destination.", func() { Format(FormatUUID) })
	Attribute("name", String, "Human-readable destination name.")
	Attribute("destination_type", String, "Destination transport type.", func() { Enum("otel") })
	Attribute("sensitive_data", String, "Whether sensitive data is included in payloads sent to this destination.", func() {
		Enum("exclude", "include")
	})
	Attribute("otel", OtelDestination, "OTEL configuration. Present when destination_type is otel.")
	Attribute("created_at", String, "Creation timestamp.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update timestamp.", func() { Format(FormatDateTime) })
	Required("id", "project_id", "name", "destination_type", "sensitive_data", "created_at", "updated_at")
})

var CreateDestinationForm = Type("CreateDestinationForm", func() {
	Description("Form for creating a data export destination. Supply the transport configuration selected by destination_type.")
	Attribute("name", String, "Human-readable destination name.")
	Attribute("destination_type", String, "Destination transport type.", func() { Enum("otel") })
	Attribute("sensitive_data", String, "Sensitive-data policy.", func() {
		Enum("exclude", "include")
		Default("exclude")
	})
	Attribute("otel", CreateOtelDestinationInput, "OTEL configuration. Required when destination_type is otel.")
	Required("name", "destination_type")
})

var UpdateDestinationForm = Type("UpdateDestinationForm", func() {
	Description("Full replacement form for a data export destination. Supply the transport configuration selected by destination_type.")
	Attribute("id", String, "Destination ID.", func() { Format(FormatUUID) })
	Attribute("name", String, "Human-readable destination name.")
	Attribute("destination_type", String, "Destination transport type.", func() { Enum("otel") })
	Attribute("sensitive_data", String, "Sensitive-data policy.", func() { Enum("exclude", "include") })
	Attribute("otel", UpdateOtelDestinationInput, "OTEL configuration. Required when destination_type is otel. Header entries with omitted values preserve existing encrypted values by case-insensitive name.")
	Required("id", "name", "destination_type", "sensitive_data")
})

var DataExportRoute = Type("DataExportRoute", func() {
	Description("The export configuration for one class of project data. A route may contain one destination of each supported type.")
	Attribute("id", String, "Route ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "Project that owns the route.", func() { Format(FormatUUID) })
	Attribute("data_source", String, "Class of data exported by this route.", func() {
		Enum("product_telemetry")
	})
	Attribute("enabled", Boolean, "Whether the route is enabled.")
	Attribute("otel_destination_id", String, "OTEL destination configured on this route. Omitted when no OTEL destination is selected.", func() { Format(FormatUUID) })
	Attribute("created_at", String, "Creation timestamp.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "Last update timestamp.", func() { Format(FormatDateTime) })
	Required("id", "project_id", "data_source", "enabled", "created_at", "updated_at")
})

var CreateDataExportRouteForm = Type("CreateDataExportRouteForm", func() {
	Description("Form for creating the selected data source's export route.")
	Attribute("data_source", String, "Class of data exported by this route.", func() { Enum("product_telemetry") })
	Attribute("enabled", Boolean, "Whether the route is enabled.", func() { Default(true) })
	Attribute("otel_destination_id", String, "OTEL destination configured on this route. Required when enabled.", func() { Format(FormatUUID) })
	Required("data_source")
})

var UpdateDataExportRouteForm = Type("UpdateDataExportRouteForm", func() {
	Description("Full replacement form for the selected data source's export route. Omit otel_destination_id to clear its OTEL destination.")
	Attribute("id", String, "Route ID.", func() { Format(FormatUUID) })
	Attribute("data_source", String, "Class of data exported by this route.", func() { Enum("product_telemetry") })
	Attribute("enabled", Boolean, "Whether the route is enabled.")
	Attribute("otel_destination_id", String, "OTEL destination configured on this route. Required when enabled.", func() { Format(FormatUUID) })
	Required("id", "data_source", "enabled")
})

var ListDestinationsResult = Type("ListDestinationsResult", func() {
	Attribute("destinations", ArrayOf(Destination), "Active data export destinations in the selected project.")
	Required("destinations")
})

var ListDataExportRoutesResult = Type("ListDataExportRoutesResult", func() {
	Attribute("routes", ArrayOf(DataExportRoute), "Active data export routes in the selected project.")
	Required("routes")
})
