package deployments

import (
	"net/http"

	. "goa.design/goa/v3/dsl"
)

var JSONSchema = Type("JSONSchema", String, func() {
	Format("json")
})

var OpenAPI3P1ParameterSchema = Type("OpenAPI3P1ParameterSchema", func() {
	Required("name", "in", "required")

	Attribute("name", String, func() {
		Description("The name of the parameter.")
	})
	Attribute("description", String, func() {
		Description("A brief description of the parameter.")
	})
	Attribute("in", String, func() {
		Enum("query", "header", "path", "cookie")
		Description("The location of the parameter in an HTTP request.")
	})
	Attribute("required", Boolean, func() {
		Description("Whether the parameter is required.")
	})
	Attribute("style", String, func() {
		Enum("form", "simple", "spaceDelimited", "pipeDelimited", "deepObject")
		Description("The style of the parameter.")
	})
	Attribute("explode", Boolean, func() {
		Description("Whether the parameter is exploded.")
	})
	Attribute("allowReserved", Boolean, func() {
		Description("Whether the parameter is allowed to be reserved.")
	})
	Attribute("schema", JSONSchema, func() {
		Description("The JSON Schema describing the parameter.")
	})
	Attribute("deprecated", Boolean, func() {
		Description("A brief description of the parameter.")
		Default(false)
	})
	Attribute("example", Any, func() {
		Description("An example value for the parameter.")
	})
	Attribute("examples", MapOf(String, Any), func() {
		Description("Examples of the parameter.")
	})

	Meta("openapi:additionalProperties", "true")
})

var OpenAPI3P1ToolForm = Type("OpenAPI3P1ToolForm", func() {
	Required("kind", "name", "description", "method", "path", "path_parameters", "header_parameters", "query_parameters", "body")

	Attribute("kind", String, func() {
		Enum("oas3p1_operation")
	})

	Attribute("name", String, func() {
		Description("The name of the tool.")
		Pattern(`^[a-zA-Z](?:[a-zA-Z0-9_-]*[a-zA-Z0-9])?$`)
		MinLength(1)
		MaxLength(50)
		Example("lookup_weather")
	})
	Attribute("description", String, func() {
		Description("The description that is provided with the tool.")
		Example("Looks up the weather in a given location.")
	})
	Attribute("tags", ArrayOf(String, func() { Pattern(`^[a-z](?:[a-z0-9_-]*[a-z0-9])?$`) }), func() {
		Description("The tags that are associated with the tool.")
		MaxLength(10)
		Default([]string{})
		Example([]string{"weather", "public"})
	})
	Attribute("path", String, func() {
		Description("The path of the HTTP endpoint.")
		Example("/invoices/{id}")
	})
	Attribute("method", String, func() {
		Enum(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead, "QUERY", "SEARCH")
		Description("The method to use for the HTTP request.")
		Example(http.MethodPost)
	})
	Attribute("path_parameters", MapOf(String, OpenAPI3P1ParameterSchema), func() {
		Description("A map of path parameters to interpolate into the path of the HTTP request.")
	})
	Attribute("header_parameters", MapOf(String, OpenAPI3P1ParameterSchema), func() {
		Description("A map of header schemas to send with the HTTP request.")
	})
	Attribute("query_parameters", MapOf(String, OpenAPI3P1ParameterSchema), func() {
		Description("A map of query parameters to send with the HTTP request.")
	})
	Attribute("body", JSONSchema, func() {
		Description("The JSON Schema describing the body of the HTTP request.")
	})

	Meta("openapi:additionalProperties", "false")
	Meta("openapi:example", "false")
})
