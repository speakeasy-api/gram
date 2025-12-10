package functions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("functions", func() {
	Description("Endpoints for working with functions.")
	shared.DeclareErrorResponses()

	Method("getSignedAssetURL", func() {
		Description("Get the signed asset URL for a function")
		Security(security.FunctionToken)

		Payload(func() {
			security.FunctionTokenPayload()

			Extend(GetSignedAssetURLForm)
		})

		Result(GetSignedAssetURLResult)

		HTTP(func() {
			POST("/rpc/functions.getSignedAssetURL")
			security.FunctionTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:generate", "false")
	})
})

var GetSignedAssetURLForm = Type("GetSignedAssetURLForm", func() {
	Attribute("asset_id", String, "The ID of the function asset")

	Required("asset_id")
})

var GetSignedAssetURLResult = Type("GetSignedAssetURLResult", func() {
	Required("url")

	Attribute("url", String, "The signed URL to access the asset")
})
