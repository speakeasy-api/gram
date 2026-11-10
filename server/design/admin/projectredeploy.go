package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminProjectRedeployPreview = Type("AdminProjectRedeployPreview", func() {
	Description("What redeploying a project would act on.")
	Required("organization_id", "project_id", "project_name", "active_deployment_id", "openapi_assets", "function_assets", "packages")

	Attribute("organization_id", String, "Organization that owns the project.")
	Attribute("project_id", String, "Project ID.")
	Attribute("project_name", String, "Project name.")
	Attribute("active_deployment_id", String, "Deployment a redeploy would clone. Send it back as expected_active_deployment_id.")
	Attribute("openapi_assets", Int, "OpenAPI sources that would be processed again.")
	Attribute("function_assets", Int, "Function sources whose runners would be deployed again.")
	Attribute("packages", Int, "Packages carried into the new deployment.")
})

var AdminProjectRedeployResult = Type("AdminProjectRedeployResult", func() {
	Description("The deployment a redeploy created. Processing continues after this returns.")
	Required("project_id", "source_deployment_id", "deployment_id")

	Attribute("project_id", String, "Project ID.")
	Attribute("source_deployment_id", String, "Deployment that was cloned.")
	Attribute("deployment_id", String, "New deployment. It becomes active only if processing succeeds.")
})

func projectRedeployMethods() {
	Method("getProjectRedeployPreview", func() {
		Description("Returns what redeploying a project would act on, for review before redeployProject.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "project_id")

			Attribute("organization_id", String, "Exact organization ID.")
			Attribute("project_id", String, "Exact project ID.")
		})

		Result(AdminProjectRedeployPreview)

		HTTP(func() {
			GET("/admin/project.redeployPreview")

			Param("organization_id")
			Param("project_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetProjectRedeployPreview")
		Meta("openapi:extension:x-speakeasy-name-override", "getProjectRedeployPreview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetProjectRedeployPreview"}`)
	})

	Method("redeployProject", func() {
		Description("Clones a project's active deployment and starts processing it. Refuses when the active deployment is no longer the one reviewed. The new deployment becomes active only if processing succeeds.")

		Payload(func() {
			Meta("openapi:typename", "RedeployProjectRequestBody")
			security.AdminAuthPayload()
			Required("organization_id", "project_id", "expected_active_deployment_id")

			Attribute("organization_id", String, "Exact organization ID.")
			Attribute("project_id", String, "Exact project ID.")
			Attribute("expected_active_deployment_id", String, "active_deployment_id from getProjectRedeployPreview.")
		})

		Result(AdminProjectRedeployResult)

		HTTP(func() {
			POST("/admin/project.redeploy")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRedeployProject")
		Meta("openapi:extension:x-speakeasy-name-override", "redeployProject")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminRedeployProject"}`)
	})
}
