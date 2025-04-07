package tools

import (
	"github.com/speakeasy-api/gram/design/environments"
	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/tools"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("instances", func() {
	Description("Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("consumer")
	})

	Method("loadInstance", func() {
		Description("load all relevant data for an instance of a toolset and environment")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("toolset_slug", String, "The slug of the toolset to load")
			Attribute("environment_slug", String, "The slug of the environment to load")
			Required("toolset_slug")
		})

		Result(InstanceResult)

		HTTP(func() {
			GET("/rpc/instances.load")
			Param("toolset_slug")
			Param("environment_slug")
			security.SessionHeader()
			security.ProjectHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LoadInstance"}`)
	})
})

var InstanceResult = Type("InstanceResult", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "The description of the toolset")
	Attribute("tools", ArrayOf(tools.HTTPToolDefinition), "The list of tools")
	Attribute("relevant_environment_variables", ArrayOf(String), "The environment variables that are relevant to the toolset")
	Attribute("environment", environments.Environment, "The environment")
	Required("name", "tools", "environment")
})
