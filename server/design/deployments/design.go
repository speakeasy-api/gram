package deployments

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var _ = Service("deployments", func() {
	Description("Manages deployments of tools from upstream sources.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getDeployment", func() {
		Description("Get a deployment by its ID.")

		Payload(func() {
			Extend(GetDeploymentForm)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetDeploymentResult)

		HTTP(func() {
			GET("/rpc/deployments.get")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "getById")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Deployment"}`)
	})

	Method("getLatestDeployment", func() {
		Description("Get the latest deployment for a project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetLatestDeploymentResult)

		HTTP(func() {
			GET("/rpc/deployments.latest")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getLatestDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "latest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LatestDeployment"}`)
	})

	Method("getActiveDeployment", func() {
		Description("Get the active deployment for a project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetActiveDeploymentResult)

		HTTP(func() {
			GET("/rpc/deployments.active")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getActiveDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "active")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ActiveDeployment"}`)
	})

	Method("createDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(func() {
			Extend(CreateDeploymentForm)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(CreateDeploymentResult)

		HTTP(func() {
			POST("/rpc/deployments.create")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Header("idempotency_key:Idempotency-Key")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateDeployment"}`)
	})

	Method("evolve", func() {
		Description("Create a new deployment with additional or updated tool sources.")

		Payload(func() {
			Extend(EvolveForm)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(EvolveResult)

		HTTP(func() {
			POST("/rpc/deployments.evolve")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "evolveDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "evolveDeployment")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EvolveDeployment"}`)
	})

	Method("redeploy", func() {
		Description("Redeploys an existing deployment.")

		Payload(func() {
			Attribute("deployment_id", String, "The ID of the deployment to redeploy.")
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Required("deployment_id")
		})

		Result(RedeployResult)

		HTTP(func() {
			POST("/rpc/deployments.redeploy")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "redeployDeployment")
		Meta("openapi:extension:x-speakeasy-name-override", "redeployDeployment")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RedeployDeployment"}`)
	})

	Method("listDeployments", func() {
		Description("List all deployments in descending order of creation.")

		Payload(func() {
			Extend(ListDeploymentForm)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListDeploymentResult)

		HTTP(func() {
			GET("/rpc/deployments.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("cursor")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listDeployments")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListDeployments"}`)
	})

	Method("getDeploymentLogs", func() {
		Description("Get logs for a deployment.")

		Payload(func() {
			Extend(GetDeploymentLogsForm)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetDeploymentLogsResult)

		HTTP(func() {
			GET("/rpc/deployments.logs")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("deployment_id")
			Param("cursor")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeploymentLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "logs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeploymentLogs"}`)
	})

	Method("evaluateOpenAPIv3", func() {
		Description("Compare an uploaded OpenAPI document with the version of the same source in the active deployment. Reports changes to tools, inputs and security without deploying anything.")

		Payload(func() {
			Extend(EvaluateOpenAPIv3Form)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(OpenAPIv3Evaluation)

		HTTP(func() {
			POST("/rpc/deployments.evaluateOpenAPIv3")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "evaluateOpenAPIv3")
		Meta("openapi:extension:x-speakeasy-name-override", "evaluateOpenAPIv3")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EvaluateOpenAPIv3"}`)
	})

	Method("getDeploymentEvaluation", func() {
		Description("Compare the OpenAPI documents in a deployment with the deployment it was created from.")

		Payload(func() {
			Required("deployment_id")
			Attribute("deployment_id", String, "The ID of the deployment to evaluate.")
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetDeploymentEvaluationResult)

		HTTP(func() {
			GET("/rpc/deployments.evaluation")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Param("deployment_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeploymentEvaluation")
		Meta("openapi:extension:x-speakeasy-name-override", "evaluation")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeploymentEvaluation"}`)
	})
})

var DeploymentSummary = Type("DeploymentSummary", func() {
	Required("id", "created_at", "user_id", "status", "openapiv3_asset_count", "openapiv3_tool_count", "functions_asset_count", "functions_tool_count", "external_mcp_asset_count", "external_mcp_tool_count")

	Attribute("id", String, func() {
		Description("The ID to of the deployment.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("user_id", String, func() {
		Description("The ID of the user that created the deployment.")
	})
	Attribute("status", String, func() {
		Description("The status of the deployment.")
	})
	Attribute("created_at", String, func() {
		Description("The creation date of the deployment.")
		Format(FormatDateTime)
	})
	Attribute("openapiv3_asset_count", Int64, func() {
		Description("The number of upstream OpenAPI assets.")
	})
	Attribute("openapiv3_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from OpenAPI documents.")
	})
	Attribute("functions_asset_count", Int64, func() {
		Description("The number of Functions assets.")
	})
	Attribute("functions_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from Functions.")
	})
	Attribute("external_mcp_asset_count", Int64, func() {
		Description("The number of external MCP server assets.")
	})
	Attribute("external_mcp_tool_count", Int64, func() {
		Description("The number of tools in the deployment generated from external MCP servers.")
	})
})

var CreateDeploymentForm = Type("CreateDeploymentForm", func() {
	Required("idempotency_key")

	Attribute("idempotency_key", String, func() {
		Description("A unique identifier that will mitigate against duplicate deployments.")
		Example("01jqq0ajmb4qh9eppz48dejr2m")
	})
	Attribute("non_blocking", Boolean, func() {
		Description("If true, the deployment will be created in non-blocking mode where the request will return immediately and the deployment will proceed asynchronously.")
		Example(false)
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

	Attribute("openapiv3_assets", ArrayOf(AddOpenAPIv3DeploymentAssetForm))
	Attribute("functions", ArrayOf(AddFunctionsForm))
	Attribute("packages", ArrayOf(AddDeploymentPackageForm))
	Attribute("external_mcps", ArrayOf(AddExternalMCPForm))
})

var AddOpenAPIv3DeploymentAssetForm = Type("AddOpenAPIv3DeploymentAssetForm", func() {
	Required("asset_id", "name", "slug")

	Attribute("asset_id", String, func() {
		Description("The ID of the uploaded asset.")
	})
	Attribute("name", String, func() {
		Description("The name to give the document as it will be displayed in UIs.")
	})
	Attribute("slug", shared.Slug, func() {
		Description("The slug to give the document as it will be displayed in URLs.")
	})
})

var AddFunctionsForm = Type("AddFunctionsForm", func() {
	Required("asset_id", "name", "slug", "runtime")

	Attribute("asset_id", String, func() {
		Description("The ID of the functions file from the assets service.")
	})
	Attribute("name", String, func() {
		Description("The functions file display name.")
	})
	Attribute("slug", shared.Slug, func() {
		Description("A URL-friendly string that identifies the functions file. Usually derived from the name.")
	})
	Attribute("runtime", String, func() {
		Description("The runtime to use when executing functions. Allowed values are: nodejs:22, nodejs:24, python:3.12.")
	})
	Attribute("memory_mib", UInt, func() {
		Description("The amount of memory in MiB to allocate for the function (1 MiB = 1024 * 1024 bytes).")
		Minimum(0)
		Maximum(constants.MaxFunctionMemoryMiB)
	})
	Attribute("scale", UInt, func() {
		Description("The number of instances to scale the function to.")
		Minimum(0)
		Maximum(constants.MaxFunctionScale)
	})
})

var AddPackageForm = Type("AddPackageForm", func() {
	Required("name")

	Attribute("name", String, func() {
		Description("The name of the package to add.")
	})
	Attribute("version", String, func() {
		Description("The version of the package to add. If omitted, the latest version will be used.")
	})
})

var AddDeploymentPackageForm = Type("AddDeploymentPackageForm", func() {
	Required("name")

	Attribute("name", String, func() {
		Description("The name of the package.")
	})
	Attribute("version", String, func() {
		Description("The version of the package.")
	})
})

var AddExternalMCPForm = Type("AddExternalMCPForm", func() {
	Required("name", "slug", "registry_server_specifier")

	Attribute("registry_id", String, func() {
		Description("The ID of the external MCP registry the server is from.")
		Format(FormatUUID)
	})
	Attribute("name", String, func() {
		Description("The display name for the external MCP server.")
		Example("My Slack Integration")
	})
	Attribute("slug", shared.Slug, func() {
		Description("A URL-friendly identifier used for tool prefixing (e.g., 'exa').")
		Example("exa")
	})
	Attribute("registry_server_specifier", String, func() {
		Description("The canonical server name used to look up the server in the registry (e.g., 'slack', 'ai.exa/exa').")
		Example("slack")
	})
	Attribute("selected_remotes", ArrayOf(String), func() {
		Description("URLs of the remotes to use for this MCP server. If not provided, the backend will auto-select based on transport type preference.")
		Example([]string{"https://mcp.example.com/sse"})
	})
})

var CreateDeploymentResult = Type("CreateDeploymentResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("A deployment that was successfully created.")
		Meta("openapi:example", "false")
	})
})

var ListDeploymentForm = Type("ListDeploymentForm", func() {
	Attribute("cursor", String, "The cursor to fetch results from")
})

var ListDeploymentResult = Type("ListDeploymentResult", func() {
	Required("items")

	Attribute("next_cursor", String, "The cursor to fetch results from", func() {
		Example("01jp3f054qc02gbcmpp0qmyzed")
	})
	Attribute("items", ArrayOf(DeploymentSummary), "A list of deployments")
})

var GetDeploymentForm = Type("GetDeploymentForm", func() {
	Required("id")
	Attribute("id", String, "The ID of the deployment")
})

var GetDeploymentResult = Type("GetDeploymentResult", func() {
	Extend(shared.Deployment)
})

var GetLatestDeploymentResult = Type("GetLatestDeploymentResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("The latest deployment for a project if available.")
		Meta("openapi:example", "false")
	})
})

var GetActiveDeploymentResult = Type("GetActiveDeploymentResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("The active deployment for a project if available.")
		Meta("openapi:example", "false")
	})
})

var AddOpenAPIv3SourceForm = Type("AddOpenAPIv3SourceForm", func() {
	Extend(AddOpenAPIv3DeploymentAssetForm)
})

var AddOpenAPIv3SourceResult = Type("AddOpenAPIv3SourceResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("A deployment that was successfully created.")
		Meta("openapi:example", "false")
	})
})

var EvolveForm = Type("EvolveForm", func() {
	Attribute("deployment_id", String, "The ID of the deployment to evolve. If omitted, the latest deployment will be used.")
	Attribute("non_blocking", Boolean, func() {
		Description("If true, the deployment will be created in non-blocking mode where the request will return immediately and the deployment will proceed asynchronously.")
		Example(false)
	})
	Attribute("upsert_openapiv3_assets", ArrayOf(AddOpenAPIv3DeploymentAssetForm), "The OpenAPI 3.x documents to upsert in the new deployment.")
	Attribute("upsert_packages", ArrayOf(AddPackageForm), "The packages to upsert in the new deployment.")
	Attribute("upsert_functions", ArrayOf(AddFunctionsForm), "The tool functions to upsert in the new deployment.")
	Attribute("upsert_external_mcps", ArrayOf(AddExternalMCPForm), "The external MCP servers to upsert in the new deployment.")
	Attribute("exclude_openapiv3_assets", ArrayOf(String), "The OpenAPI 3.x documents, identified by slug, to exclude from the new deployment when cloning a previous deployment.")
	Attribute("exclude_packages", ArrayOf(String), "The packages to exclude from the new deployment when cloning a previous deployment.")
	Attribute("exclude_functions", ArrayOf(String), "The functions, identified by slug, to exclude from the new deployment when cloning a previous deployment.")
	Attribute("exclude_external_mcps", ArrayOf(String), "The external MCP servers, identified by slug, to exclude from the new deployment when cloning a previous deployment.")
})

var EvolveResult = Type("EvolveResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("A deployment that was successfully created.")
		Meta("openapi:example", "false")
	})
})

var RedeployResult = Type("RedeployResult", func() {
	Attribute("deployment", shared.Deployment, func() {
		Description("A deployment that was successfully created.")
		Meta("openapi:example", "false")
	})
})

var GetDeploymentLogsForm = Type("GetDeploymentLogsForm", func() {
	Required("deployment_id")
	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("cursor", String, "The cursor to fetch results from")
})

var GetDeploymentLogsResult = Type("GetDeploymentLogsResult", func() {
	Required("events", "status")
	Attribute("next_cursor", String, "The cursor to fetch results from")
	Attribute("status", String, "The status of the deployment")
	Attribute("events", ArrayOf(DeploymentLogEvent), "The logs for the deployment")
})

var DeploymentLogEvent = Type("DeploymentLogEvent", func() {
	Required("id", "created_at", "event", "message")

	Attribute("id", String, "The ID of the log event")
	Attribute("attachment_id", String, "The ID of the asset tied to the log event")
	Attribute("attachment_type", String, "The type of the asset tied to the log event")
	Attribute("created_at", String, "The creation date of the log event")
	Attribute("event", String, "The type of event that occurred")
	Attribute("message", String, "The message of the log event")
})

var EvaluateOpenAPIv3Form = Type("EvaluateOpenAPIv3Form", func() {
	Required("asset_id", "slug")

	Attribute("asset_id", String, "The ID of the uploaded OpenAPI document asset to evaluate.")
	Attribute("slug", String, "The slug of the OpenAPI source the document will replace. A slug that is not in the active deployment is evaluated as a new source.")
	Attribute("name", String, "The display name of the source. Defaults to the deployed name or the slug.")
})

var GetDeploymentEvaluationResult = Type("GetDeploymentEvaluationResult", func() {
	Required("deployment_id", "documents")

	Attribute("deployment_id", String, "The ID of the evaluated deployment.")
	Attribute("base_deployment_id", String, "The ID of the deployment it is compared with. Absent for the first deployment in a project.")
	Attribute("documents", ArrayOf(OpenAPIv3Evaluation), "One evaluation per OpenAPI source that was added, changed or removed.")
})

var OpenAPIv3Evaluation = Type("OpenAPIv3Evaluation", func() {
	Description("The changes an OpenAPI document introduces compared with the deployed version of the same source.")
	Required("slug", "name", "status", "changes", "breaking_count", "warning_count", "info_count", "affected_toolsets")

	Attribute("slug", String, "The slug of the OpenAPI source.")
	Attribute("name", String, "The display name of the OpenAPI source.")
	Attribute("status", String, "Whether the source is new, changed, unchanged or removed.", func() {
		Enum("new", "changed", "unchanged", "removed")
	})
	Attribute("asset_id", String, "The asset ID of the evaluated document. Absent when the source was removed.")
	Attribute("base_asset_id", String, "The asset ID of the deployed document. Absent for a new source.")
	Attribute("base", OpenAPIv3DocumentSnapshot, "A summary of the deployed document.")
	Attribute("next", OpenAPIv3DocumentSnapshot, "A summary of the evaluated document.")
	Attribute("changes", ArrayOf(OpenAPIv3EvaluationChange), "Every change found, most severe first.")
	Attribute("breaking_count", Int, "The number of breaking changes.")
	Attribute("warning_count", Int, "The number of changes that need attention.")
	Attribute("info_count", Int, "The number of informational changes.")
	Attribute("affected_toolsets", ArrayOf(OpenAPIv3AffectedToolset), "Toolsets (MCP servers) that include tools from this source.")
})

var OpenAPIv3DocumentSnapshot = Type("OpenAPIv3DocumentSnapshot", func() {
	Required("tool_count", "skipped_operation_count", "security_schemes", "default_security")

	Attribute("tool_count", Int, "The number of tools the document produces.")
	Attribute("skipped_operation_count", Int, "The number of operations that cannot be turned into tools.")
	Attribute("security_schemes", ArrayOf(OpenAPIv3SecuritySchemeSummary), "The security schemes tools can use.")
	Attribute("default_security", String, "The document-level security requirement.")
	Attribute("server_url", String, "The default server URL tools call.")
})

var OpenAPIv3SecuritySchemeSummary = Type("OpenAPIv3SecuritySchemeSummary", func() {
	Required("key", "type", "summary", "env_variables")

	Attribute("key", String, "The name of the scheme under components.securitySchemes.")
	Attribute("type", String, "The OpenAPI security scheme type.")
	Attribute("summary", String, "A short description of the scheme.")
	Attribute("env_variables", ArrayOf(String), "The environment variables that supply credentials for the scheme.")
})

var OpenAPIv3EvaluationChange = Type("OpenAPIv3EvaluationChange", func() {
	Required("severity", "category", "code", "title", "details", "tools")

	Attribute("severity", OpenAPIv3EvaluationSeverity, "How likely the change is to break existing MCP servers.")
	Attribute("category", OpenAPIv3EvaluationCategory, "The part of the MCP the change affects.")
	Attribute("code", String, "A stable identifier for the kind of change.")
	Attribute("title", String, "A one-line summary of the change.")
	Attribute("details", ArrayOf(String), "Supporting lines, such as before and after values.")
	Attribute("tools", ArrayOf(String), "The names of the tools the change affects.")
})

var OpenAPIv3EvaluationSeverity = Type("OpenAPIv3EvaluationSeverity", String, func() {
	Description("How likely an OpenAPI change is to break existing MCP servers.")
	Enum("breaking", "warning", "info")
})

var OpenAPIv3EvaluationCategory = Type("OpenAPIv3EvaluationCategory", String, func() {
	Description("The part of the MCP an OpenAPI change affects.")
	Enum("security", "tools", "server")
})

var OpenAPIv3AffectedToolset = Type("OpenAPIv3AffectedToolset", func() {
	Required("id", "name", "slug", "mcp_enabled", "oauth_configured", "tool_count", "affected_tool_count")

	Attribute("id", String, "The ID of the toolset.")
	Attribute("name", String, "The name of the toolset.")
	Attribute("slug", String, "The slug of the toolset.")
	Attribute("mcp_slug", String, "The MCP slug of the toolset, when it is served as an MCP server.")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is served as an MCP server.")
	Attribute("oauth_configured", Boolean, "Whether the MCP server has OAuth configured.")
	Attribute("tool_count", Int, "The number of tools from this source in the toolset.")
	Attribute("affected_tool_count", Int, "The number of those tools with breaking changes or changes that need attention.")
})
