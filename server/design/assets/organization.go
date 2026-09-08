package assets

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// organizationAssets manages organization-tier assets — rows with a NULL
// project_id scoped to an organization, inherited by every project in the
// org. Security is organization-scoped (no ProjectSlug) and session-only:
// the RBAC gate on org:admin is not enforced for API-key requests, and the
// only consumer today is the dashboard branding UI, so the API-key surface is
// deliberately not offered. RBAC gates writes on org:admin. Images are served
// through the existing public assets.serveImage endpoint, which looks assets
// up by id alone.
var _ = Service("organizationAssets", func() {
	Description("Manages organization-tier assets — files owned by an organization rather than a single project, such as remote identity provider logos.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("uploadOrganizationImage", func() {
		Description("Upload an organization-tier image (e.g. a remote identity provider logo). Requires org:admin.")

		Payload(UploadOrganizationImageForm)

		Result(UploadImageResult)

		HTTP(func() {
			POST("/rpc/organizationAssets.uploadImage")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadOrganizationImage")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadImage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOrganizationImage"}`)
	})
})

var UploadOrganizationImageForm = Type("UploadOrganizationImageForm", func() {
	Required("content_type", "content_length")
	security.SessionPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})
