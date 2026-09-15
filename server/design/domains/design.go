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
	Attribute("ip_allowlist", ArrayOf(String), "IP addresses or CIDR ranges allowed to access this domain. Empty list means unrestricted.")
	Attribute("health_status", String, "The latest observed domain health status. One of: unknown, healthy, unhealthy.")
	Attribute("health_issue", String, "The reason the domain was last observed as unhealthy. One of: dns_not_found, dns_target_mismatch, resource_missing, certificate_missing, certificate_not_ready, certificate_expired, certificate_invalid, check_failed.")
	Attribute("health_checked_at", String, func() {
		Description("When the domain health was last checked.")
		Format(FormatDateTime)
	})
	Attribute("unhealthy_since", String, func() {
		Description("When the current unhealthy period began.")
		Format(FormatDateTime)
	})
	Attribute("certificate_expires_at", String, func() {
		Description("When the currently observed TLS certificate expires.")
		Format(FormatDateTime)
	})
	Attribute("consecutive_failures", Int32, "The number of consecutive failed health checks")
	Attribute("root_mcp_endpoint_id", String, "The MCP endpoint currently mapped to the domain root, if any", func() {
		Format(FormatUUID)
	})
	Attribute("openai_apps_challenge_token", String, "The token served for OpenAI app-submission domain verification, if configured")
	Attribute("suggested_record_type", String, func() {
		Description("The suggested DNS record type for this domain. A suggestion only — delegated subzones can make an apex-looking domain CNAME-capable.")
		Enum("cname", "a")
	})

	Required("id", "organization_id", "domain", "verified", "activated", "created_at", "updated_at", "is_updating", "ip_allowlist", "suggested_record_type")
})

// DomainDNSConfig describes the DNS targets custom domains must point at.
var DomainDNSConfig = Type("DomainDNSConfig", func() {
	Attribute("cname_target", String, "The CNAME target subdomain custom domains should point at, if configured")
	Attribute("a_records", ArrayOf(String), "The static IP addresses apex custom domains should point A records at")

	Required("a_records")
})

// RootMcpServerOption is an MCP server an org admin can map to the custom
// domain root, whether or not it already has an endpoint on the domain.
var RootMcpServerOption = Type("RootMcpServerOption", func() {
	Attribute("mcp_server_id", String, "The MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The MCP server's display name, if set")
	Attribute("slug", String, "The MCP server's slug, if set")
	Attribute("project_id", String, "The project the server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("project_name", String, "The project's display name")
	Attribute("attached_endpoint_id", String, "The server's endpoint on this custom domain, when one exists", func() {
		Format(FormatUUID)
	})
	Attribute("attached_endpoint_slug", String, "The attached endpoint's slug (its /mcp/<slug> path on the domain), when one exists")
	Attribute("is_domain_root", Boolean, "Whether this server currently serves the domain root")

	Required("mcp_server_id", "project_id", "project_name", "is_domain_root")
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

	Method("listDomains", func() {
		Description("List the custom domains for an organization. The result is empty when no custom domain has been configured.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListCustomDomainsResult)

		HTTP(func() {
			GET("/rpc/domain.list")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listDomains")
		Meta("openapi:extension:x-speakeasy-name-override", "listDomains")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "listDomains"}`)
	})

	Method("createDomain", func() {
		Description("Create a custom domain for an organization")

		Payload(func() {
			security.SessionPayload()
			Attribute("domain", String, "The custom domain")
			Attribute("ip_allowlist", ArrayOf(String), "IP addresses or CIDR ranges to allow. Leave empty for unrestricted access.")
			Required("domain")
		})

		Result(CustomDomain)

		HTTP(func() {
			POST("/rpc/domain.register")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "registerDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "registerDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "registerDomain"}`)
	})

	Method("updateDomain", func() {
		Description("Update settings for the organization's custom domain")

		Payload(func() {
			security.SessionPayload()
			Attribute("ip_allowlist", ArrayOf(String), "Replacement IP allowlist. Pass an empty list to remove all restrictions.")
			Attribute("openai_apps_challenge_token", String, "Replacement OpenAI app-submission verification token. Pass an empty string to clear it.")
		})

		Result(CustomDomain)

		HTTP(func() {
			POST("/rpc/domain.update")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateDomain")
		Meta("openapi:extension:x-speakeasy-name-override", "updateDomain")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "updateDomain"}`)
	})

	Method("setRootMcpEndpoint", func() {
		Description("Set or clear the MCP endpoint mapped to a custom domain's root. Pass mcp_endpoint_id for an endpoint already attached to the domain, or mcp_server_id to attach a server (creating its domain endpoint if needed) and map it in one call — usable while the domain is still pending verification, so a migration can be staged before DNS cuts over.")

		Payload(func() {
			security.SessionPayload()
			Attribute("custom_domain_id", String, "The custom domain whose root mapping to change", func() {
				Format(FormatUUID)
			})
			Attribute("mcp_endpoint_id", String, "The MCP endpoint to map to the domain root. Omit both ids to clear the mapping.", func() {
				Format(FormatUUID)
			})
			Attribute("mcp_server_id", String, "An MCP server to map to the domain root; its domain endpoint is created when missing. Mutually exclusive with mcp_endpoint_id.", func() {
				Format(FormatUUID)
			})
			Required("custom_domain_id")
		})

		Result(CustomDomain)

		HTTP(func() {
			POST("/rpc/domain.setRootMcpEndpoint")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setRootMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-name-override", "setRootMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetRootMcpEndpoint"}`)
	})

	Method("listRootMcpServers", func() {
		Description("List the organization's MCP servers that can be mapped to the custom domain root, including servers not yet attached to the domain")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("mcp_servers", ArrayOf(RootMcpServerOption))
			Required("mcp_servers")
		})

		HTTP(func() {
			GET("/rpc/domain.listRootMcpServers")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRootMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listRootMcpServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RootMcpServers", "type": "query"}`)
	})

	Method("checkHealth", func() {
		Description("Check the routing and certificate health of the organization's custom domain")

		Payload(func() {
			security.SessionPayload()
		})

		Result(CustomDomain)

		HTTP(func() {
			POST("/rpc/domain.checkHealth")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "checkDomainHealth")
		Meta("openapi:extension:x-speakeasy-name-override", "checkHealth")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CheckDomainHealth"}`)
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
	Attribute("mcp_server_id", String, "The ID of the parent MCP server. Null for meta-MCP-backed endpoints.", func() {
		Format(FormatUUID)
	})
	Attribute("meta_mcp_server_id", String, "The ID of the parent meta MCP server. Null for MCP-server-backed endpoints.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_name", String, "The display name of the parent server. May be empty if the parent has no configured name.")
	Attribute("mcp_server_slug", String, "The url-friendly slug of the parent MCP server. May be empty if the parent has no configured slug or is a meta MCP server.")
	Attribute("is_domain_root", Boolean, "Whether this endpoint is mapped to the custom-domain root")

	Required("id", "slug", "project_id", "project_name", "project_slug", "is_domain_root")
})

var ListCustomDomainMcpEndpointsResult = Type("ListCustomDomainMcpEndpointsResult", func() {
	Description("Result of listing the MCP endpoints registered under an organization's custom domain.")

	Attribute("mcp_endpoints", ArrayOf(CustomDomainMcpEndpoint))
	Required("mcp_endpoints")
})

var ListCustomDomainsResult = Type("ListCustomDomainsResult", func() {
	Description("Result of listing an organization's custom domains.")

	Attribute("domains", ArrayOf(CustomDomain))
	Attribute("dns_config", DomainDNSConfig, "The DNS targets custom domains must point at. Present even when no domain is configured yet, so setup instructions can be shown before registration.")
	Required("domains", "dns_config")
})
