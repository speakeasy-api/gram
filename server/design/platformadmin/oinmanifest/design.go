// Package oinmanifest declares the main-server platform-admin export of the
// Okta Integration Network (OIN) Cross App Access manifest: the platform
// catalog of global ID-JAG issuers and their global clients, shaped the way
// Okta's listing questionnaire asks for it. It reads catalog rows only and
// never exposes tenant data.
package oinmanifest

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var _ = Service("oinManifest", func() {
	Description("Platform-admin export of the OIN Cross App Access manifest built from the global remote session catalog. Requires a current users.admin entitlement on an ordinary Gram session.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })

	Method("export", func() {
		Description("Export the OIN Cross App Access manifest as JSON (for the listing questionnaire) or Markdown (for review). Requires platform admin.")

		Payload(func() {
			Attribute("format", String, "Output format.", func() {
				Enum("json", "markdown")
				Default("json")
			})
			security.SessionPayload()
		})

		Result(func() {
			Attribute("content_type", String)
			Attribute("content_disposition", String)
			Required("content_type", "content_disposition")
		})

		HTTP(func() {
			GET("/rpc/oinManifest.export")
			Param("format")
			security.SessionHeader()
			Response(StatusOK, func() {
				// JSON or Markdown depending on format; the handler sets the
				// real Content-Type header per response.
				ContentType("*/*")
				Header("content_type:Content-Type")
				Header("content_disposition:Content-Disposition")
			})
			Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
				ContentType("application/json")
			})
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "exportOinManifest")
		Meta("openapi:extension:x-speakeasy-name-override", "export")
	})
})
