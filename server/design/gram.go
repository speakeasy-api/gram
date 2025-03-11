package design

import (
	. "goa.design/goa/v3/dsl"
)

// Service definition
var _ = Service("gram", func() {
	Description("The concerts service manages music concert data.")

	Method("createDeployment", func() {
		Description("Create a deployment to load tool definitions.")

		Payload(DeploymentCreateForm)

		Result(DeploymentCreateResult)

		HTTP(func() {
			POST("/rpc/deployments.create")

			Response(StatusOK)
		})
	})
})
