package domains

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// CustomDomain represents a custom domain associated with a project.
var CustomDomain = Type("CustomDomain", func() {
	Attribute("id", String, "The ID of the custom domain")
	Attribute("organization_id", String, "The ID of the organization this domain belongs to")
	Attribute("domain", String, "The custom domain name")
	Attribute("verified", Boolean, "Whether the domain is verified")
	Attribute("activated", Boolean, "Whether the domain is activated in ingress")
	Attribute("created_at", String, func() {
		Description("When the custom domain was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the custom domain was last updated.")
		Format(FormatDateTime)
	})
	Attribute("is_updating", Boolean, "The custom domain is actively being registered")

	Required("id", "organization_id", "domain", "verified", "activated", "created_at", "updated_at", "is_updating")
})

var _ = Service("domains", func() {
	Description("Manage custom domains for gram.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getDomain", func() {
		Description("Get the custom domain for an organization")

		Payload(func() {
			security.SessionPayload()
		})

		Result(CustomDomain)

		HTTP(func() {
			GET("/rpc/domain.get")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "getDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getDomain"}`)
	})

	Method("createDomain", func() {
		Description("Create a custom domain for an organization")

		Payload(func() {
			security.SessionPayload()
			Attribute("domain", String, "The custom domain")
			Required("domain")
		})

		HTTP(func() {
			POST("/rpc/domain.register")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "registerDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "registerDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "registerDomain"}`)
	})

	Method("deleteDomain", func() {
		Description("Delete a custom domain")

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/domain.delete")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "deleteDomain"}`)
	})

	Method("listMcpEndpoints", func() {
		Description("List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListCustomDomainMcpEndpointsResult)

		HTTP(func() {
			GET("/rpc/domain.listMcpEndpoints")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listCustomDomainMcpEndpoints")
		Meta("openapi:extension:x-speakeasy-name-override", "listMcpEndpoints")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CustomDomainMcpEndpoints"}`)
	})
})

var CustomDomainMcpEndpoint = Type("CustomDomainMcpEndpoint", func() {
	Description("An MCP endpoint registered under a custom domain, with its parent MCP server and project denormalised for display in the dashboard's delete-impact preview.")

	Attribute("id", String, "The ID of the MCP endpoint", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "The endpoint slug")
	Attribute("project_id", String, "The ID of the project the endpoint belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("project_name", String, "The display name of the project the endpoint belongs to")
	Attribute("project_slug", String, "The url-friendly slug of the project the endpoint belongs to")
	Attribute("mcp_server_id", String, "The ID of the parent MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_name", String, "The display name of the parent MCP server. May be empty if the parent has no configured name.")
	Attribute("mcp_server_slug", String, "The url-friendly slug of the parent MCP server. May be empty if the parent has no configured slug.")

	Required("id", "slug", "project_id", "project_name", "project_slug", "mcp_server_id")
})

var ListCustomDomainMcpEndpointsResult = Type("ListCustomDomainMcpEndpointsResult", func() {
	Description("Result of listing the MCP endpoints registered under an organization's custom domain.")

	Attribute("mcp_endpoints", ArrayOf(CustomDomainMcpEndpoint))
	Required("mcp_endpoints")
})
