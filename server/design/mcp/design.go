package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("mcp", func() {
	Description("Model Context Protocol server hosting.")
	shared.DeclareErrorResponses()

	Method("servePublic", func() {
		Description("MCP server endpoint for a toolset (public, no environment param).")

		Error("no_content", func() {
			Attribute("ack", Boolean)
			Required("ack")
		})

		Payload(func() {
			Attribute("mcpSlug", String, "The unique slug of the mcp server.")
			Attribute("environment_variables", String, "The environment variables passed by user to MCP server (JSON Structured).")
			Attribute("apikey_token", String, "The API key token (OPTIONAL).")
			Required("mcpSlug")
		})

		Result(func() {
			Required("contentType")
			Attribute("contentType", String)
		})

		HTTP(func() {
			POST("/mcp/{mcpSlug}")

			Param("mcpSlug")
			Header("environment_variables:MCP-Environment")
			security.ByKeyNamedHeader("Authorization") // this is optional depending on server visibility and will be checked in the handler

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

	Method("serveAuthenticated", func() {
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})

		Description("MCP server endpoint for a toolset (environment as path param, authenticated).")

		Error("no_content", func() {
			Attribute("ack", Boolean)
			Required("ack")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayloadNamed("project")
			Attribute("toolset", String, "The toolset to access via MCP.")
			Attribute("environment", String, "The environment to access via MCP.")
			Attribute("environment_variables", String, "The environment variables passed by user to MCP server (JSON Structured).")
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
			Param("environment") // environment as path param
			Header("environment_variables:MCP-Environment")

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
