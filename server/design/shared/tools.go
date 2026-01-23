package shared

import (
	extmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/urn"
	. "goa.design/goa/v3/dsl"
)

var ResponseFilter = Type("ResponseFilter", func() {
	Meta("struct:pkg:path", "types")

	Description("Response filter metadata for the tool")
	Attribute("type", String, "Response filter type for the tool")
	Required("type")
	Attribute("status_codes", ArrayOf(String), "Status codes to filter for")
	Required("status_codes")
	Attribute("content_types", ArrayOf(String), "Content types to filter for")
	Required("content_types")
})

// BaseToolAttributes contains common fields shared by all tool types
var BaseToolAttributes = Type("BaseToolAttributes", func() {
	Meta("struct:pkg:path", "types")
	Meta("type:generate:force")

	Description("Common attributes shared by all tool types")

	Attribute("id", String, "The ID of the tool")
	Attribute("tool_urn", String, "The URN of this tool")
	Attribute("project_id", String, "The ID of the project")

	Attribute("name", String, "The name of the tool")
	Attribute("canonical_name", String, "The canonical name of the tool. Will be the same as the name if there is no variation.")
	Attribute("description", String, "Description of the tool")
	Attribute("schema_version", String, "Version of the schema")
	Attribute("schema", String, "JSON schema for the request")

	Attribute("confirm", String, "Confirmation mode for the tool")
	Attribute("confirm_prompt", String, "Prompt for the confirmation")
	Attribute("summarizer", String, "Summarizer for the tool")

	Attribute("created_at", String, func() {
		Description("The creation date of the tool.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the tool.")
		Format(FormatDateTime)
	})

	Attribute("canonical", CanonicalToolAttributes, "The original details of a tool, excluding any variations")
	Attribute("variation", ToolVariation, "The variation details of a tool. Only includes explicitly varied fields.")

	Required("id", "project_id", "name", "canonical_name", "description", "schema", "tool_urn", "created_at", "updated_at")
})

// HTTPTool represents an HTTP tool with all its attributes
var HTTPToolDefinition = Type("HTTPToolDefinition", func() {
	Meta("struct:pkg:path", "types")

	Description("An HTTP tool")

	Extend(BaseToolAttributes)

	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("asset_id", String, "The ID of the asset")

	Attribute("summary", String, "Summary of the tool")
	Attribute("response_filter", ResponseFilter, "Response filter metadata for the tool")

	Attribute("openapiv3_document_id", String, "The ID of the OpenAPI v3 document")
	Attribute("openapiv3_operation", String, "OpenAPI v3 operation")
	Attribute("tags", ArrayOf(String), "The tags list for this http tool")
	Attribute("security", String, "Security requirements for the underlying HTTP endpoint")
	Attribute("default_server_url", String, "The default server URL for the tool")
	Attribute("http_method", String, "HTTP method for the request")
	Attribute("path", String, "Path for the request")
	Attribute("package_name", String, "The name of the source package")

	Required("summary", "tags", "http_method", "path", "schema", "deployment_id", "asset_id")
})

// FunctionToolDefinition represents a function-based tool with all its attributes
var FunctionToolDefinition = Type("FunctionToolDefinition", func() {
	Meta("struct:pkg:path", "types")

	Description("A function tool")

	Extend(BaseToolAttributes)

	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("asset_id", String, "The ID of the asset")
	Attribute("function_id", String, "The ID of the function")
	Attribute("runtime", String, "Runtime environment (e.g., nodejs:22, python:3.12)")
	Attribute("variables", Any, "Variables configuration for the function")
	Attribute("meta", MapOf(String, Any), "Meta tags for the tool")

	Required("deployment_id", "asset_id", "function_id", "runtime")
})

// ExternalMCPToolDefinition represents a proxy tool that unfolds to external MCP server tools at runtime
var ExternalMCPToolDefinition = Type("ExternalMCPToolDefinition", func() {
	Meta("struct:pkg:path", "types")

	Description("A proxy tool that references an external MCP server")

	Attribute("id", String, "The ID of the tool definition")
	Attribute("tool_urn", String, "The URN of this tool (tools:externalmcp:<slug>:proxy)")
	Attribute("deployment_external_mcp_id", String, "The ID of the deployments_external_mcps record")
	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("registry_id", String, "The ID of the MCP registry")
	Attribute("name", String, "The reverse-DNS name of the external MCP server (e.g., ai.exa/exa)")
	Attribute("slug", String, "The slug used for tool prefixing (e.g., github)")
	Attribute("remote_url", String, "The URL to connect to the MCP server")
	Attribute("transport_type", String, func() {
		Description("The transport type used to connect to the MCP server")
		Enum(string(extmcptypes.TransportTypeStreamableHTTP), string(extmcptypes.TransportTypeSSE))
	})
	Attribute("requires_oauth", Boolean, "Whether the external MCP server requires OAuth authentication")

	// OAuth metadata
	Attribute("oauth_version", String, "OAuth version: '2.1' (MCP OAuth), '2.0' (legacy), or 'none'")
	Attribute("oauth_authorization_endpoint", String, "The OAuth authorization endpoint URL")
	Attribute("oauth_token_endpoint", String, "The OAuth token endpoint URL")
	Attribute("oauth_registration_endpoint", String, "The OAuth dynamic client registration endpoint URL")
	Attribute("oauth_scopes_supported", ArrayOf(String), "The OAuth scopes supported by the server")

	Attribute("created_at", String, func() {
		Description("When the tool definition was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the tool definition was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "tool_urn", "deployment_external_mcp_id", "deployment_id", "registry_id", "name", "slug", "remote_url", "transport_type", "requires_oauth", "oauth_version", "created_at", "updated_at")
})

// Tool is a discriminated union of HTTP tools and prompt templates.
// Custom JSON marshaling provided in goaext.
var Tool = Type("Tool", func() {
	Meta("struct:pkg:path", "types")
	Description("A polymorphic tool - can be an HTTP tool, function tool, prompt template, or external MCP proxy")

	Attribute("http_tool_definition", HTTPToolDefinition, "The HTTP tool definition")
	Attribute("function_tool_definition", FunctionToolDefinition, "The function tool definition")
	Attribute("prompt_template", PromptTemplate, "The prompt template")
	Attribute("external_mcp_tool_definition", ExternalMCPToolDefinition, "The external MCP tool definition")
})

var ToolEntry = Type("ToolEntry", func() {
	Attribute("type", String, func() {
		Enum(string(urn.ToolKindHTTP), string(urn.ToolKindPrompt), string(urn.ToolKindFunction), string(urn.ToolKindExternalMCP))
	})

	Attribute("id", String, "The ID of the tool")
	Attribute("tool_urn", String, "The URN of the tool")
	Attribute("name", String, "The name of the tool")

	Required("type", "id", "name", "tool_urn")
})

var CanonicalToolAttributes = Type("CanonicalToolAttributes", func() {
	Meta("struct:pkg:path", "types")
	Description("The original details of a tool")

	Required("variation_id", "name", "description")

	Attribute("variation_id", String, "The ID of the variation that was applied to the tool")
	Attribute("name", String, "The name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("confirm", String, "Confirmation mode for the tool")
	Attribute("confirm_prompt", String, "Prompt for the confirmation")
	Attribute("summarizer", String, "Summarizer for the tool")
})

var Environment = Type("Environment", func() {
	Meta("struct:pkg:path", "types")

	Description("Model representing an environment")

	Attribute("id", String, "The ID of the environment")
	Attribute("organization_id", String, "The organization ID this environment belongs to")
	Attribute("project_id", String, "The project ID this environment belongs to")
	Attribute("name", String, "The name of the environment")
	Attribute("slug", Slug, "The slug identifier for the environment")
	Attribute("description", String, "The description of the environment")
	Attribute("entries", ArrayOf(EnvironmentEntry), "List of environment entries")
	Attribute("created_at", String, func() {
		Description("The creation date of the environment")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the environment was last updated")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "name", "slug", "entries", "created_at", "updated_at")
})

var EnvironmentEntry = Type("EnvironmentEntry", func() {
	Meta("struct:pkg:path", "types")

	Description("A single environment entry")

	Attribute("name", String, "The name of the environment variable")
	Attribute("value", String, "Redacted values of the environment variable")
	Attribute("value_hash", String, "Hash of the value to identify matching values across environments without exposing the actual value")
	Attribute("created_at", String, func() {
		Description("The creation date of the environment entry")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the environment entry was last updated")
		Format(FormatDateTime)
	})

	Required("name", "value", "value_hash", "created_at", "updated_at")
})
