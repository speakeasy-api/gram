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

var FunctionEnvironmentVariable = Type("FunctionEnvironmentVariable", func() {
	Meta("struct:pkg:path", "types")

	Attribute("description", String, "Description of the function environment variable")
	Attribute("auth_input_type", String, "Optional value of the function variable comes from a specific auth input")
	Attribute("name", String, "The environment variables")
	Required("name")
})

var OAuthEnablementMetadata = Type("OAuthEnablementMetadata", func() {
	Meta("struct:pkg:path", "types")

	Attribute("oauth2_security_count", Int, "Count of security variables that are OAuth2 supported")
	Required("oauth2_security_count")
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
	Attribute("function_environment_variables", ArrayOf(FunctionEnvironmentVariable), "The function environment variables that are relevant to the toolset")
	Attribute("oauth_enablement_metadata", OAuthEnablementMetadata, "The metadata surrounding oauth enabled tools within this server")
	Attribute("tools", ArrayOf(Tool), "The tools in this toolset")
	Attribute("tool_urns", ArrayOf(String), "The tool URNs in this toolset")
	Attribute("toolset_version", Int64, "The version of the toolset (will be 0 if none exists)")
	Attribute("resources", ArrayOf(Resource), "The resources in this toolset")
	Attribute("resource_urns", ArrayOf(String), "The resource URNs in this toolset")

	Attribute("prompt_templates", ArrayOf(PromptTemplate), "The prompt templates in this toolset -- Note: these are actual prompts, as in MCP prompts")
	Attribute("mcp_slug", Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("tool_selection_mode", String, "The mode to use for tool selection")
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
	Required("id", "project_id", "organization_id", "account_type", "name", "slug", "tools", "tool_selection_mode", "toolset_version", "prompt_templates", "tool_urns", "resources", "resource_urns", "oauth_enablement_metadata", "created_at", "updated_at")
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
	Attribute("function_environment_variables", ArrayOf(FunctionEnvironmentVariable), "The function environment variables that are relevant to the toolset")
	Attribute("tools", ArrayOf(ToolEntry), "The tools in this toolset")
	Attribute("tool_urns", ArrayOf(String), "The tool URNs in this toolset")
	Attribute("resources", ArrayOf(ResourceEntry), "The resources in this toolset")
	Attribute("resource_urns", ArrayOf(String), "The resource URNs in this toolset")

	Attribute("prompt_templates", ArrayOf(PromptTemplateEntry), "The prompt templates in this toolset -- Note: these are actual prompts, as in MCP prompts")
	Attribute("mcp_slug", Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("tool_selection_mode", String, "The mode to use for tool selection")
	Attribute("custom_domain_id", String, "The ID of the custom domain to use for the toolset")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "organization_id", "name", "slug", "tools", "tool_selection_mode", "prompt_templates", "tool_urns", "resources", "resource_urns", "created_at", "updated_at")
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
	Attribute("provider_type", String, func() {
		Description("The type of OAuth provider")
		Enum("custom", "gram")
	})
	Attribute("authorization_endpoint", String, "The authorization endpoint URL")
	Attribute("token_endpoint", String, "The token endpoint URL")
	Attribute("scopes_supported", ArrayOf(String), "The OAuth scopes supported by this provider")
	Attribute("grant_types_supported", ArrayOf(String), "The grant types supported by this provider")
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String), "The token endpoint auth methods supported by this provider")
	Attribute("environment_slug", Slug, "The environment slug where OAuth credentials are stored")
	Attribute("created_at", String, func() {
		Description("When the OAuth proxy provider was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the OAuth proxy provider was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "slug", "provider_type", "authorization_endpoint", "token_endpoint", "created_at", "updated_at")
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

var OAuthProxyServerForm = Type("OAuthProxyServerForm", func() {
	Meta("struct:pkg:path", "types")

	Attribute("slug", Slug, "The slug of the OAuth proxy server")
	Attribute("provider_type", String, func() {
		Description("The type of OAuth provider")
		Enum("custom", "gram")
	})
	Attribute("authorization_endpoint", String, "The authorization endpoint URL")
	Attribute("token_endpoint", String, "The token endpoint URL")
	Attribute("scopes_supported", ArrayOf(String), "OAuth scopes to request")
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String), "Auth methods (client_secret_basic or client_secret_post)")
	Attribute("environment_slug", Slug, "The environment slug to store secrets")
	Required("slug", "provider_type")
})
