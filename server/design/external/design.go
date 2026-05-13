package external

import (
	. "goa.design/goa/v3/dsl"
)

var _ = Service("external", func() {
	Description("Endpoints for external services to interact with gram.")

	Method("receiveWorkOSWebhook", func() {
		Description("Receive and enqueue a WorkOS webhook event.")

		Payload(func() {
			Attribute("workos_signature", String, "WorkOS webhook signature header")
		})

		HTTP(func() {
			POST("/rpc/external.receiveWorkOSWebhook")
			Header("workos_signature:WorkOS-Signature", String, "WorkOS webhook signature header")
			SkipRequestBodyEncodeDecode()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "receiveWorkOSWebhook")
		Meta("openapi:extension:x-speakeasy-name-override", "receiveWorkOSWebhook")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})
})
