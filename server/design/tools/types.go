package tools

import (
	. "goa.design/goa/v3/dsl"
)

var HTTPToolDefinition = Type("HTTPToolDefinition", func() {
	Attribute("id", String, "The ID of the HTTP tool")
	Attribute("name", String, "The name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("tags", ArrayOf(String), "The tags list for this http tool")
	Attribute("server_env_var", String, "Environment variable for the server URL")
	Attribute("security_type", String, "Type of security (http:bearer, http:basic, apikey)")
	Attribute("bearer_env_var", String, "Environment variable for bearer token")
	Attribute("apikey_env_var", String, "Environment variable for API key")
	Attribute("username_env_var", String, "Environment variable for username")
	Attribute("password_env_var", String, "Environment variable for password")
	Attribute("http_method", String, "HTTP method for the request")
	Attribute("path", String, "Path for the request")
	Attribute("headers_schema", String, "JSON schema for headers")
	Attribute("queries_schema", String, "JSON schema for query parameters")
	Attribute("pathparams_schema", String, "JSON schema for path parameters")
	Attribute("body_schema", String, "JSON schema for request body")
	Attribute("created_at", String, func() {
		Description("The creation date of the tool.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the tool.")
		Format(FormatDateTime)
	})
	Required("id", "name", "description", "tags", "server_env_var", "security_type", "http_method", "path", "created_at", "updated_at")
})
