package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("mcp", func() {
	Description("Model Context Protocol server hosting.")
	shared.DeclareErrorResponses()

	Method("serve", func() {
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Description("MCP server endpoint for a toolset.")

		Error("no_content", func() {
			Attribute("ack", Boolean)
			Required("ack")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayloadNamed("project")

			Attribute("toolset", String, "The toolset to access via MCP.")
			Attribute("environment", String, "The environment to access via MCP.")
		})

		Result(func() {
			Required("contentType")
			Attribute("contentType", String)
		})

		HTTP(func() {
			POST("/mcp/{project}/{toolset}/{environment}")

			security.ByKeyNamedHeader("Authorization")
			security.ProjectParam("project")

			Param("toolset")
			Param("environment")

			SkipRequestBodyEncodeDecode()
			SkipResponseBodyEncodeDecode()

			Response(StatusOK, func() {
				ContentType("application/json")
				Header("contentType:Content-Type")
			})
			Response("no_content", StatusNoContent, func() {
				Body(Empty)
				Header("ack:NOOP")
			})
		})
	})
})
