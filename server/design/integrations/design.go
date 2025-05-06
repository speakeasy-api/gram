package integrations

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("integrations", func() {
	Description("Explore third-party tools in Gram.")
	shared.DeclareErrorResponses()
	Security(security.Session, security.ProjectSlug)

	Method("get", func() {
		Description("Get a third-party integration by ID or name.")
		Payload(func() {
			Extend(GetIntegrationForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(GetIntegrationResult)

		HTTP(func() {
			GET("/rpc/integrations.get")
			Param("id")
			Param("name")
			security.SessionHeader()
			security.ProjectHeader()
		})
	})

	Method("list", func() {
		Description("List available third-party integrations.")

		Payload(func() {
			Extend(ListIntegrationsForm)

			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListIntegrationsResult)

		HTTP(func() {
			GET("/rpc/integrations.list")
			Param("keywords")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listIntegrations")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListIntegrations"}`)
	})
})

var GetIntegrationForm = Type("GetIntegrationForm", func() {
	Description("Get a third-party integration by ID or name.")

	Attribute("id", String, "The ID of the integration to get (refers to a package id).")
	Attribute("name", String, "The name of the integration to get (refers to a package name).")
})

var GetIntegrationResult = Type("GetIntegrationResult", func() {
	Attribute("integration", Integration)
})

var ListIntegrationsForm = Type("ListIntegrationsForm", func() {
	Attribute("keywords", ArrayOf(String), "Keywords to filter integrations by", func() {
		Elem(func() {
			MaxLength(20)
		})
	})
})

var ListIntegrationsResult = Type("ListIntegrationsResult", func() {
	Attribute("integrations", ArrayOf(IntegrationEntry), "List of available third-party integrations")
})

var IntegrationEntry = Type("IntegrationEntry", func() {
	Required("package_id", "package_name", "version", "version_created_at", "tool_count")

	Attribute("package_id", String)
	Attribute("package_name", String)
	Attribute("package_title", String)
	Attribute("package_summary", String)
	Attribute("package_url", String)
	Attribute("package_keywords", ArrayOf(String))
	Attribute("package_image_asset_id", String)
	Attribute("version", String)
	Attribute("version_created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("tool_count", Int)
})

var Integration = Type("Integration", func() {
	Required("package_id", "package_name", "package_title", "package_summary", "version", "version_created_at", "tool_count")

	Attribute("package_id", String)
	Attribute("package_name", String)
	Attribute("package_title", String)
	Attribute("package_summary", String)
	Attribute("package_description", String)
	Attribute("package_description_raw", String)
	Attribute("package_url", String)
	Attribute("package_keywords", ArrayOf(String))
	Attribute("package_image_asset_id", String)
	Attribute("version", String)
	Attribute("version_created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("tool_count", Int)
})
