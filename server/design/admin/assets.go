package admin

import (
	assetsdesign "github.com/speakeasy-api/gram/server/design/assets"
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

func platformAssetMethods() {
	Method("uploadPlatformImage", func() {
		Description("Upload a global issuer logo, limited to 4 MiB and PNG, JPEG, GIF or WebP.")
		Payload(func() { security.AdminAuthPayload(); Attribute("content_type", String); Required("content_type") })
		Result(assetsdesign.UploadImageResult)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/assets.uploadImage")
			declareUnavailableResponse()
			Header("content_type:Content-Type")
			SkipRequestBodyEncodeDecode()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminUploadPlatformImage")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadPlatformImage")
	})
	Method("serveImage", func() {
		Description("Serve a public image, preserving the existing image serving contract.")
		NoSecurity()
		Payload(assetsdesign.ServeImageForm)
		Result(assetsdesign.ServeImageResult)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/assets.serveImage")
			declareUnavailableResponse()
			Param("id")
			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
				Header("access_control_allow_origin:Access-Control-Allow-Origin")
				Header("cross_origin_resource_policy:Cross-Origin-Resource-Policy")
			})
			SkipResponseBodyEncodeDecode()
		})
		Meta("openapi:operationId", "adminServeImage")
		Meta("openapi:extension:x-speakeasy-name-override", "serveImage")
	})
}
