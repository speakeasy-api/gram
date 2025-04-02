package system

import (
	. "goa.design/goa/v3/dsl"
)

var _ = Service("system", func() {
	Description("Exposes service health and status information.")

	Method("healthCheck", func() {
		Description("Check the health of the service.")

		Result(HealthCheckResult)

		HTTP(func() {
			GET("/health")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})
})

var HealthCheckResult = Type("HealthCheckResult", func() {
	Required("status")
	Attribute("status", String, "The status of the service.")
})
