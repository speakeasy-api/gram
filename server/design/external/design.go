package external

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("external", func() {
	Description("Endpoints for external services to interact with gram.")

	shared.DeclareErrorResponses()

	Method("receiveWorkOSWebhook", func() {
		Description("Endpoint to receive WorkOS webhooks.")

		Security(security.WorkOSSignature)

		Payload(func() {
			security.WorkOSSignaturePayload()
		})

		HTTP(func() {
			POST("/rpc/external.receiveWorkOSWebhook")
			security.WorkOSSignatureHeader()
			Response(StatusNoContent)

			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:generate", "false")
	})
})
