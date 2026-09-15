// Package assets declares the adminAssets Goa service: the platform-admin
// (Speakeasy-only) surface for uploading platform-tier assets (project_id IS
// NULL AND organization_id IS NULL) shared across every organization.
// Implemented on the existing *assets.Service; reuses the existing result
// types.
package assets

import (
	. "goa.design/goa/v3/dsl"

	assetsdesign "github.com/speakeasy-api/gram/server/design/assets"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("adminAssets", func() {
	Description("Platform-admin management of platform-tier assets — files shared across every organization (project_id NULL, organization_id NULL). Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("uploadPlatformImage", func() {
		Description("Upload a platform-tier image (e.g. a global remote identity provider logo). Requires platform admin.")

		Payload(UploadPlatformImageForm)

		Result(assetsdesign.UploadImageResult)

		HTTP(func() {
			POST("/rpc/adminAssets.uploadImage")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadPlatformImage")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadImage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadPlatformImage"}`)
	})
})

var UploadPlatformImageForm = Type("UploadPlatformImageForm", func() {
	Required("content_type", "content_length")
	security.SessionPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})
