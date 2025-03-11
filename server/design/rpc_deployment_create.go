package design

import (
	. "goa.design/goa/v3/dsl"
)

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
	Attribute("id", String, func() {
		Description("The ID to of the deployment.")
		Example("bc5f4a555e933e6861d12edba4c2d87ef6caf8e6")
	})
})
