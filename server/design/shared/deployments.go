package shared

import (
	. "goa.design/goa/v3/dsl"
)

var Deployment = Type("Deployment", func() {
	Required("id", "created_at", "organization_id", "project_id", "user_id", "openapiv3_assets", "status", "packages", "openapiv3_tool_count", "functions_tool_count", "external_mcp_tool_count")

	Attribute("id", String, func() {
		Description("The ID to of the deployment.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("organization_id", String, func() {
		Description("The ID of the organization that the deployment belongs to.")
	})
	Attribute("project_id", String, func() {
		Description("The ID of the project that the deployment belongs to.")
	})
	Attribute("user_id", String, func() {
		Description("The ID of the user that created the deployment.")
	})
	Attribute("created_at", String, func() {
		Description("The creation date of the deployment.")
		Format(FormatDateTime)
	})
	Attribute("status", String, func() {
		Description("The status of the deployment.")
	})
	Attribute("idempotency_key", String, func() {
		Description("A unique identifier that will mitigate against duplicate deployments.")
		Example("01jqq0ajmb4qh9eppz48dejr2m")
	})
	Attribute("github_repo", String, func() {
		Description("The github repository in the form of \"owner/repo\".")
		Example("speakeasyapi/gram")
	})
	Attribute("github_pr", String, func() {
		Description("The github pull request that resulted in the deployment.")
		Example("1234")
	})
	Attribute("github_sha", String, func() {
		Description("The commit hash that triggered the deployment.")
		Example("f33e693e9e12552043bc0ec5c37f1b8a9e076161")
	})
	Attribute("external_id", String, func() {
		Description("The external ID to refer to the deployment. This can be a git commit hash for example.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("external_url", String, func() {
		Description("The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.")
	})
	Attribute("cloned_from", String, func() {
		Description("The ID of the deployment that this deployment was cloned from.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})

	Attribute("openapiv3_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from OpenAPI documents.")
	})
	Attribute("openapiv3_assets", ArrayOf(OpenAPIv3DeploymentAsset), func() {
		Description("The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions.")
	})

	Attribute("functions_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from Functions.")
	})
	Attribute("external_mcp_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from external MCP servers.")
	})
	Attribute("functions_assets", ArrayOf(DeploymentFunctions), func() {
		Description("The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions.")
	})

	Attribute("packages", ArrayOf(DeploymentPackage), func() {
		Description("The packages that were deployed.")
	})

	Attribute("external_mcps", ArrayOf(DeploymentExternalMCP), func() {
		Description("The external MCP servers that were deployed.")
	})

	Meta("struct:pkg:path", "types")
})

var OpenAPIv3DeploymentAsset = Type("OpenAPIv3DeploymentAsset", func() {
	Required("id", "asset_id", "name", "slug")

	Attribute("id", String, func() {
		Description("The ID of the deployment asset.")
	})
	Attribute("asset_id", String, func() {
		Description("The ID of the uploaded asset.")
	})
	Attribute("name", String, func() {
		Description("The name to give the document as it will be displayed in UIs.")
	})
	Attribute("slug", Slug, func() {
		Description("The slug to give the document as it will be displayed in URLs.")
	})

	Meta("struct:pkg:path", "types")
})

var DeploymentFunctions = Type("DeploymentFunctions", func() {
	Required("id", "asset_id", "name", "slug", "runtime")

	Attribute("id", String, func() {
		Description("The ID of the deployment asset.")
	})
	Attribute("asset_id", String, func() {
		Description("The ID of the uploaded asset.")
	})
	Attribute("name", String, func() {
		Description("The name to give the document as it will be displayed in UIs.")
	})
	Attribute("slug", Slug, func() {
		Description("The slug to give the document as it will be displayed in URLs.")
	})
	Attribute("runtime", String, func() {
		Description("The runtime to use when executing functions.")
	})

	Meta("struct:pkg:path", "types")
})

var DeploymentPackage = Type("DeploymentPackage", func() {
	Required("id", "name", "version")

	Attribute("id", String, func() {
		Description("The ID of the deployment package.")
	})
	Attribute("name", String, func() {
		Description("The name of the package.")
	})
	Attribute("version", String, func() {
		Description("The version of the package.")
	})

	Meta("struct:pkg:path", "types")
})

var DeploymentExternalMCP = Type("DeploymentExternalMCP", func() {
	Required("id", "registry_id", "name", "slug", "registry_server_specifier")

	Attribute("id", String, func() {
		Description("The ID of the deployment external MCP record.")
	})
	Attribute("registry_id", String, func() {
		Description("The ID of the MCP registry the server is from.")
	})
	Attribute("name", String, func() {
		Description("The display name for the external MCP server.")
	})
	Attribute("slug", Slug, func() {
		Description("A URL-friendly identifier used for tool prefixing.")
	})
	Attribute("registry_server_specifier", String, func() {
		Description("The canonical server name used to look up the server in the registry.")
	})

	Meta("struct:pkg:path", "types")
})
