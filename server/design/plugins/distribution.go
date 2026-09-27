package plugins

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var DistributionPlugin = Type("DistributionPlugin", func() {
	Description("Minimal plugin metadata for distributing an authorized skill. Does not expose configuration or assignments.")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String)
	Attribute("description", String)
	Attribute("is_default", Boolean)
	Required("id", "name", "is_default")
})

var ListDistributionPluginsResult = Type("ListDistributionPluginsResult", func() {
	Attribute("plugins", ArrayOf(DistributionPlugin))
	Required("plugins")
})

func distributionMethods() {
	Method("listDistributionPlugins", func() {
		Description("List minimal distribution targets for a skill the caller can read. Requires skill:read, not org:read.")
		Payload(func() {
			Attribute("skill_id", String, func() { Format(FormatUUID) })
			Required("skill_id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListDistributionPluginsResult)
		HTTP(func() {
			GET("/rpc/plugins.listDistributionPlugins")
			Param("skill_id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listDistributionPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "listDistributionPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DistributionPlugins"}`)
	})
	Method("getDistributionPlugin", func() {
		Description("Get minimal distribution target metadata for a skill the caller can read. Requires skill:read, not org:read.")
		Payload(func() {
			Attribute("skill_id", String, func() { Format(FormatUUID) })
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("skill_id", "id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(DistributionPlugin)
		HTTP(func() {
			GET("/rpc/plugins.getDistributionPlugin")
			Param("skill_id")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getDistributionPlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "getDistributionPlugin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DistributionPlugin"}`)
	})
}
