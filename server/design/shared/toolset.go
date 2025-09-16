package shared

import (
	. "goa.design/goa/v3/dsl"
)

var SecurityVariable = Type("SecurityVariable", func() {
	Meta("struct:pkg:path", "types")

	Attribute("type", String, "The type of security")
	Attribute("name", String, "The name of the security scheme")
	Attribute("in_placement", String, "Where the security token is placed")
	Attribute("scheme", String, "The security scheme")
	Attribute("bearer_format", String, "The bearer format")
	Attribute("oauth_types", ArrayOf(String), "The OAuth types")
	Attribute("oauth_flows", Bytes, "The OAuth flows")
	Attribute("env_variables", ArrayOf(String), "The environment variables")
	Required("name", "in_placement", "scheme", "env_variables")
})

var ServerVariable = Type("ServerVariable", func() {
	Meta("struct:pkg:path", "types")

	Attribute("description", String, "Description of the server variable")
	Attribute("env_variables", ArrayOf(String), "The environment variables")
	Required("description", "env_variables")
})

var Toolset = Type("Toolset", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the toolset")
	Attribute("project_id", String, "The project ID this toolset belongs to")
	Attribute("organization_id", String, "The organization ID this toolset belongs to")
	Attribute("account_type", String, "The account type of the organization")
	Attribute("name", String, "The name of the toolset")
	Attribute("slug", Slug, "The slug of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("default_environment_slug", Slug, "The slug of the environment to use as the default for the toolset")
	Attribute("security_variables", ArrayOf(SecurityVariable), "The security variables that are relevant to the toolset")
	Attribute("server_variables", ArrayOf(ServerVariable), "The server variables that are relevant to the toolset")
	Attribute("http_tools", ArrayOf(HTTPToolDefinition), "The HTTP tools in this toolset")
	Attribute("prompt_templates", ArrayOf(PromptTemplate), "The prompt templates in this toolset")
	Attribute("mcp_slug", Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("custom_domain_id", String, "The ID of the custom domain to use for the toolset")
	Attribute("external_oauth_server", ExternalOAuthServer, "The external OAuth server details")
	Attribute("oauth_proxy_server", OAuthProxyServer, "The OAuth proxy server details")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "organization_id", "account_type", "name", "slug", "http_tools", "prompt_templates", "created_at", "updated_at")
})

var ToolsetEntry = Type("ToolsetEntry", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the toolset")
	Attribute("project_id", String, "The project ID this toolset belongs to")
	Attribute("organization_id", String, "The organization ID this toolset belongs to")
	Attribute("name", String, "The name of the toolset")
	Attribute("slug", Slug, "The slug of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("default_environment_slug", Slug, "The slug of the environment to use as the default for the toolset")
	Attribute("security_variables", ArrayOf(SecurityVariable), "The security variables that are relevant to the toolset")
	Attribute("server_variables", ArrayOf(ServerVariable), "The server variables that are relevant to the toolset")
	Attribute("http_tools", ArrayOf(HTTPToolDefinitionEntry), "The HTTP tools in this toolset")
	Attribute("prompt_templates", ArrayOf(PromptTemplateEntry), "The prompt templates in this toolset")
	Attribute("mcp_slug", Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("custom_domain_id", String, "The ID of the custom domain to use for the toolset")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "organization_id", "name", "slug", "http_tools", "prompt_templates", "created_at", "updated_at")
})

var ExternalOAuthServer = Type("ExternalOAuthServer", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the external OAuth server")
	Attribute("project_id", String, "The project ID this external OAuth server belongs to")
	Attribute("slug", Slug, "The slug of the external OAuth server")
	Attribute("metadata", Any, "The metadata for the external OAuth server")
	Attribute("created_at", String, func() {
		Description("When the external OAuth server was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the external OAuth server was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "slug", "metadata", "created_at", "updated_at")
})

var OAuthProxyProvider = Type("OAuthProxyProvider", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the OAuth proxy provider")
	Attribute("slug", Slug, "The slug of the OAuth proxy provider")
	Attribute("authorization_endpoint", String, "The authorization endpoint URL")
	Attribute("token_endpoint", String, "The token endpoint URL")
	Attribute("scopes_supported", ArrayOf(String), "The OAuth scopes supported by this provider")
	Attribute("grant_types_supported", ArrayOf(String), "The grant types supported by this provider")
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String), "The token endpoint auth methods supported by this provider")
	Attribute("created_at", String, func() {
		Description("When the OAuth proxy provider was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the OAuth proxy provider was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "slug", "authorization_endpoint", "token_endpoint", "created_at", "updated_at")
})

var OAuthProxyServer = Type("OAuthProxyServer", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the OAuth proxy server")
	Attribute("project_id", String, "The project ID this OAuth proxy server belongs to")
	Attribute("slug", Slug, "The slug of the OAuth proxy server")
	Attribute("oauth_proxy_providers", ArrayOf(OAuthProxyProvider), "The OAuth proxy providers for this server")
	Attribute("created_at", String, func() {
		Description("When the OAuth proxy server was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the OAuth proxy server was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "slug", "created_at", "updated_at")
})

var ExternalOAuthServerForm = Type("ExternalOAuthServerForm", func() {
	Meta("struct:pkg:path", "types")

	Attribute("slug", Slug, "The slug of the external OAuth server")
	Attribute("metadata", Any, "The metadata for the external OAuth server")
	Required("slug", "metadata")
})
