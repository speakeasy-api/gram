package deployments

import (
	"github.com/speakeasy-api/gram/design/security"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("deployments", func() {
	Description("Manages deployments of tools from upstream sources.")

	Security(security.Session, security.ProjectSlug)

	Method("getDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(func() {
			Extend(GetDeploymentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetDeploymentResult)

		HTTP(func() {
			POST("/rpc/deployments.get")
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Deployment"}`)
	})

	Method("createDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(func() {
			Extend(CreateDeploymentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(CreateDeploymentResult)

		HTTP(func() {
			POST("/rpc/deployments.create")
			security.SessionHeader()
			security.ProjectHeader()
			Header("idempotency_key:Idempotency-Key")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateDeployment"}`)
	})

	Method("listDeployments", func() {
		Description("List all deployments in descending order of creation.")

		Payload(func() {
			Extend(ListDeploymentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListDeploymentResult)

		HTTP(func() {
			GET("/rpc/deployments.list")
			security.SessionHeader()
			security.ProjectHeader()
			Param("cursor")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListDeployments"}`)
	})
})

var Deployment = Type("Deployment", func() {
	Required("id", "created_at", "organization_id", "project_id", "user_id", "openapiv3_assets")

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

	Attribute("openapiv3_assets", ArrayOf(OpenAPIv3DeploymentAsset), func() {
		Description("The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions.")
	})
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
	Attribute("slug", String, func() {
		Description("The slug to give the document as it will be displayed in URLs.")
	})
})

var DeploymentSummary = Type("DeploymentSummary", func() {
	Required("id", "created_at", "user_id", "asset_count")

	Attribute("id", String, func() {
		Description("The ID to of the deployment.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("user_id", String, func() {
		Description("The ID of the user that created the deployment.")
	})
	Attribute("created_at", String, func() {
		Description("The creation date of the deployment.")
		Format(FormatDateTime)
	})
	Attribute("asset_count", Int64, func() {
		Description("The number of upstream assets.")
	})
})

var CreateDeploymentForm = Type("CreateDeploymentForm", func() {
	Required("idempotency_key")

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

	Attribute("openapiv3_assets", ArrayOf(OpenAPIv3DeploymentAssetForm))
})

var OpenAPIv3DeploymentAssetForm = Type("OpenAPIv3DeploymentAssetForm", func() {
	Required("asset_id", "name", "slug")

	Attribute("asset_id", String, func() {
		Description("The ID of the uploaded asset.")
	})
	Attribute("name", String, func() {
		Description("The name to give the document as it will be displayed in UIs.")
	})
	Attribute("slug", String, func() {
		Description("The slug to give the document as it will be displayed in URLs.")
	})
})

var CreateDeploymentResult = Type("CreateDeploymentResult", func() {
	Attribute("deployment", Deployment, func() {
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
	Extend(Deployment)
})
