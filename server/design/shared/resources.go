package shared

import (
	"github.com/speakeasy-api/gram/server/internal/urn"
	. "goa.design/goa/v3/dsl"
)

// BaseResourceAttributes contains common fields shared by all resource types
var BaseResourceAttributes = Type("BaseResourceAttributes", func() {
	Meta("struct:pkg:path", "types")
	Meta("type:generate:force")

	Description("Common attributes shared by all resource types")

	Attribute("id", String, "The ID of the resource")
	Attribute("resource_urn", String, "The URN of this resource")
	Attribute("project_id", String, "The ID of the project")

	Attribute("name", String, "The name of the resource")
	Attribute("description", String, "Description of the resource")
	Attribute("uri", String, "The URI of the resource")
	Attribute("title", String, "Optional title for the resource")
	Attribute("mime_type", String, "Optional MIME type of the resource")

	Attribute("created_at", String, func() {
		Description("The creation date of the resource.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the resource.")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "description", "uri", "resource_urn", "created_at", "updated_at")
})

// FunctionResourceDefinition represents a function-based resource with all its attributes
var FunctionResourceDefinition = Type("FunctionResourceDefinition", func() {
	Meta("struct:pkg:path", "types")

	Description("A function resource")

	Extend(BaseResourceAttributes)

	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("function_id", String, "The ID of the function")
	Attribute("runtime", String, "Runtime environment (e.g., nodejs:22, python:3.12)")
	Attribute("variables", Any, "Variables configuration for the resource")
	Attribute("meta", MapOf(String, Any), "Meta tags for the tool")

	Required("deployment_id", "function_id", "runtime")
})

// Resource is a discriminated union for different resource types.
var Resource = Type("Resource", func() {
	Meta("struct:pkg:path", "types")
	Description("A polymorphic resource - currently only function resources are supported")

	Attribute("function_resource_definition", FunctionResourceDefinition, "The function resource definition")
})

var ResourceEntry = Type("ResourceEntry", func() {
	Meta("struct:pkg:path", "types")

	Attribute("type", String, func() {
		Enum(string(urn.ResourceKindFunction))
	})

	Attribute("id", String, "The ID of the resource")
	Attribute("resource_urn", String, "The URN of the resource")
	Attribute("name", String, "The name of the resource")
	Attribute("uri", String, "The uri of the resource")

	Required("type", "id", "name", "uri", "resource_urn")
})
