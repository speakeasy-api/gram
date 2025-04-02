package assets

import (
	"github.com/speakeasy-api/gram/design/sessions"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("assets", func() {
	Description("Manages assets used by Gram projects.")

	Security(sessions.Session)

	Method("uploadOpenAPIv3", func() {
		Description("Upload an OpenAPI v3 document to Gram.")

		Payload(func() {
			Extend(UploadOpenAPIv3Form)
		})

		Result(UploadOpenAPIv3Result)

		HTTP(func() {
			POST("/rpc/assets.uploadOpenAPIv3")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			sessions.ProjectHeader()
			sessions.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})
	})
})

var UploadOpenAPIv3Form = Type("UploadOpenAPIv3Form", func() {
	Required("content_type", "content_length")
	sessions.SessionPayload()
	sessions.ProjectPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var UploadOpenAPIv3Result = Type("UploadOpenAPIv3Result", func() {
	Required("asset")

	Attribute("asset", Asset, "The URL to the uploaded OpenAPI document")
})

var Asset = Type("Asset", func() {
	Required("id", "url", "kind", "sha256", "content_type", "content_length", "created_at", "updated_at")

	Attribute("id", String, "The ID of the asset")
	Attribute("url", String, "The URL to the uploaded asset")
	Attribute("kind", String, func() {
		Enum("openapiv3", "unknown")
	})
	Attribute("sha256", String, "The SHA256 hash of the asset")
	Attribute("content_type", String, "The content type of the asset")
	Attribute("content_length", Int64, "The content length of the asset")
	Attribute("created_at", String, func() {
		Description("The creation date of the asset.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the asset.")
		Format(FormatDateTime)
	})
})
