package about

import (
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("about", func() {
	Meta("openapi:generate", "false")

	Description("Information about the Gram platform and its components.")
	shared.DeclareErrorResponses()

	Method("openapi", func() {
		Description("The OpenAPI description of the Gram API.")

		Result(func() {
			Attribute("contentType", String, "The content type of the OpenAPI document")
			Attribute("contentLength", Int64, "The content length of the OpenAPI document")

			Required("contentType", "contentLength")
		})

		HTTP(func() {
			GET("/openapi.yaml")
			SkipResponseBodyEncodeDecode()
			Response(StatusOK, func() {
				Header("contentLength:Content-Length")
				Header("contentType:Content-Type")
			})
		})
	})

	Method("version", func() {
		Description("Get version information for the Gram components.")

		Result(func() {
			Attribute("server_version", String, "The version of the Gram server")
			Attribute("dashboard_version", String, "The version of the Gram dashboard")
			Attribute("git_sha", String, "The Git SHA of the current build")

			Required("server_version", "dashboard_version", "git_sha")
		})

		HTTP(func() {
			GET("/version")
			Response(StatusOK)
		})
	})
})
