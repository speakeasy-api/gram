package tools

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("instances", func() {
	Description("Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("consumer")
	})
	shared.DeclareErrorResponses()

	Method("getInstance", func() {
		Description("Load all relevant data for an instance of a toolset and environment")

		Payload(GetInstanceForm)

		Result(GetInstanceResult)

		HTTP(func() {
			GET("/rpc/instances.get")
			Param("toolset_slug")
			Param("environment_slug")
			security.SessionHeader()
			security.ProjectHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "getBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Instance"}`)
	})
})

var GetInstanceForm = Type("GetInstanceForm", func() {
	security.SessionPayload()
	security.ByKeyPayload()
	security.ProjectPayload()
	Attribute("toolset_slug", shared.Slug, "The slug of the toolset to load")
	Attribute("environment_slug", shared.Slug, "The slug of the environment to load")
	Required("toolset_slug")
})

var GetInstanceResult = Type("GetInstanceResult", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "The description of the toolset")
	Attribute("tools", ArrayOf(shared.Tool), "The list of tools")
	Attribute("prompt_templates", ArrayOf(shared.PromptTemplate), "The list of prompt templates")
	Attribute("security_variables", ArrayOf(shared.SecurityVariable), "The security variables that are relevant to the toolset")
	Attribute("server_variables", ArrayOf(shared.ServerVariable), "The server variables that are relevant to the toolset")
	Attribute("function_environment_variables", ArrayOf(shared.FunctionEnvironmentVariable), "The function environment variables that are relevant to the toolset")
	Attribute("environment", shared.Environment, "The environment")
	Required("name", "tools", "environment")
})
