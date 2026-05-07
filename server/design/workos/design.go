package workos

import (
	. "goa.design/goa/v3/dsl"
)

var _ = Service("workos", func() {
	Description("WorkOS webhook ingestion endpoints.")

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
	})
})
