package deployments

import (
	. "goa.design/goa/v3/dsl"
)

var _ = Service("deployments", func() {
	Description("Manages deployments of tools from upstream sources.")

	Method("getDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(DeploymentGetForm)

		Result(DeploymentGetResult)

		HTTP(func() {
			POST("/rpc/deployments.get")

			Param("id")

			Response(StatusOK)
		})
	})

	Method("createDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(DeploymentCreateForm)

		Result(DeploymentCreateResult)

		HTTP(func() {
			POST("/rpc/deployments.create")

			Response(StatusOK)
		})
	})

	Method("listDeployments", func() {
		Description("List all deployments in descending order of creation.")

		Payload(DeploymentListForm)

		Result(DeploymentListResult)

		HTTP(func() {
			POST("/rpc/deployments.list")

			Param("cursor")
			Param("limit")

			Response(StatusOK)
		})
	})
})

var Deployment = Type("Deployment", func() {
	Required("id", "created_at", "organization_id", "workspace_id", "user_id")

	Attribute("id", String, func() {
		Description("The ID to of the deployment.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("organization_id", String, func() {
		Description("The ID of the organization that the deployment belongs to.")
	})
	Attribute("workspace_id", String, func() {
		Description("The ID of the workspace that the deployment belongs to.")
	})
	Attribute("user_id", String, func() {
		Description("The ID of the user that created the deployment.")
	})
	Attribute("created_at", String, func() {
		Description("The creation date of the deployment.")
		Format(FormatDateTime)
	})
	Attribute("external_id", String, func() {
		Description("The external ID to refer to the deployment. This can be a git commit hash for example.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("external_url", String, func() {
		Description("The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.")
		Example("https://github.com/golang/go/commit/bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("openapi_3p1_tools", ArrayOf(OpenAPI3P1ToolForm), func() {
		Description("The HTTP tools available in the deployment.")
		Meta("openapi:example", "false")
	})
})

var DeploymentCreateForm = Type("DeploymentCreateForm", func() {
	Attribute("external_id", String, func() {
		Description("The external ID to refer to the deployment. This can be a git commit hash for example.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("external_url", String, func() {
		Description("The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.")
		Example("https://github.com/golang/go/commit/bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
	Attribute("openapi_3p1_tools", ArrayOf(OpenAPI3P1ToolForm), func() {
		Description("The HTTP tools available in the deployment.")
		Meta("openapi:example", "false")
	})
})

var DeploymentCreateResult = Type("DeploymentCreateResult", func() {
	Extend(Deployment)
})

var DeploymentListForm = Type("DeploymentListForm", func() {
	Attribute("cursor", String, "The cursor to fetch results from")
	Attribute("limit", Int, "Results per page", func() {
		Minimum(1)
		Maximum(100)
		Default(10)
	})
})

var DeploymentListResult = Type("DeploymentListResult", func() {
	Required("items")

	Attribute("next_cursor", String, "The cursor to fetch results from", func() {
		Example("01jp3f054qc02gbcmpp0qmyzed")
	})
	Attribute("items", ArrayOf(Deployment), "A list of deployments")
})

var DeploymentGetForm = Type("DeploymentGetForm", func() {
	Required("id")
	Attribute("id", String, "The ID of the deployment")
})

var DeploymentGetResult = Type("DeploymentGetResult", func() {
	Extend(Deployment)
})
