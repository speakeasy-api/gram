package plugins

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// PluginServerModel represents an MCP server included in a plugin.
var PluginServerModel = Type("PluginServer", func() {
	Required("id", "display_name", "policy", "sort_order", "created_at")

	Attribute("id", String, func() {
		Description("Unique plugin server identifier.")
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, func() {
		Description("Gram toolset ID, if this server is a first-party toolset.")
		Format(FormatUUID)
	})
	Attribute("registry_id", String, func() {
		Description("MCP registry ID, if this server is from a catalog.")
		Format(FormatUUID)
	})
	Attribute("registry_server_specifier", String, "Registry server specifier (e.g. io.modelcontextprotocol.anonymous/exa).")
	Attribute("external_url", String, "External MCP server URL.")
	Attribute("display_name", String, "Display name shown in generated plugin config.")
	Attribute("policy", String, func() {
		Description("Whether this server is required or optional.")
		Enum("required", "optional")
	})
	Attribute("sort_order", Int32, "Ordering within the plugin.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
})

// PluginAssignmentModel represents a role or user assignment for a plugin.
var PluginAssignmentModel = Type("PluginAssignment", func() {
	Required("id", "principal_urn", "created_at")

	Attribute("id", String, func() {
		Description("Unique assignment identifier.")
		Format(FormatUUID)
	})
	Attribute("principal_urn", String, "Principal URN (e.g. role:engineering, user:id, or *).")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
})

// PluginModel is the full plugin representation.
var PluginModel = Type("Plugin", func() {
	Required("id", "name", "slug", "created_at", "updated_at")

	Attribute("id", String, func() {
		Description("Unique plugin identifier.")
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name.")
	Attribute("slug", String, "URL-safe identifier, unique per org.")
	Attribute("description", String, "Optional description.")
	Attribute("server_count", Int64, "Number of active servers in this plugin.")
	Attribute("assignment_count", Int64, "Number of role/user assignments.")
	Attribute("servers", ArrayOf(PluginServerModel), "Servers included in this plugin.")
	Attribute("assignments", ArrayOf(PluginAssignmentModel), "Role/user assignments.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

// --- Forms ---

var CreatePluginForm = Type("CreatePluginForm", func() {
	Required("name")

	Attribute("name", String, "Display name for the plugin.")
	Attribute("description", String, "Optional description.")
})

var UpdatePluginForm = Type("UpdatePluginForm", func() {
	Required("id", "name", "slug")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Updated display name.")
	Attribute("slug", String, "Updated slug.")
	Attribute("description", String, "Updated description.")
})

var AddPluginServerForm = Type("AddPluginServerForm", func() {
	Required("plugin_id", "display_name")

	Attribute("plugin_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("registry_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("registry_server_specifier", String)
	Attribute("external_url", String)
	Attribute("display_name", String, "Display name for the server.")
	Attribute("policy", String, func() {
		Enum("required", "optional")
		Default("required")
	})
	Attribute("sort_order", Int32, func() {
		Default(0)
	})
})

var UpdatePluginServerForm = Type("UpdatePluginServerForm", func() {
	Required("id", "plugin_id", "display_name")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("plugin_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("display_name", String)
	Attribute("policy", String, func() {
		Enum("required", "optional")
		Default("required")
	})
	Attribute("sort_order", Int32, func() {
		Default(0)
	})
})

var SetPluginAssignmentsForm = Type("SetPluginAssignmentsForm", func() {
	Required("plugin_id", "principal_urns")

	Attribute("plugin_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("principal_urns", ArrayOf(String), "List of principal URNs to assign.")
})

// --- Results ---

var ListPluginsResult = Type("ListPluginsResult", func() {
	Required("plugins")
	Attribute("plugins", ArrayOf(PluginModel), "The plugins in the organization.")
})

var PublishPluginsResult = Type("PublishPluginsResult", func() {
	Required("published", "repo_url")
	Attribute("published", Boolean, "Whether the publish succeeded.")
	Attribute("repo_url", String, "GitHub repository URL where plugins were published.")
	Attribute("commit_sha", String, "Git commit SHA of the push.")
})

var GitHubConnectionModel = Type("PluginGitHubConnection", func() {
	Required("id", "installation_id", "repo_owner", "repo_name", "created_at")

	Attribute("id", String, func() {
		Description("Unique connection identifier.")
		Format(FormatUUID)
	})
	Attribute("installation_id", Int64, "GitHub App installation ID.")
	Attribute("repo_owner", String, "GitHub org or user that owns the repo.")
	Attribute("repo_name", String, "Repository name.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
})

var ConnectGitHubForm = Type("ConnectGitHubForm", func() {
	Attribute("installation_id", Int64, "GitHub App installation ID. Auto-detected if omitted.")
})

// --- Service ---

var _ = Service("plugins", func() {
	Description("Manage distributable plugin bundles of MCP servers and hooks.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listPlugins", func() {
		Description("List all plugins for the current organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListPluginsResult)

		HTTP(func() {
			GET("/rpc/plugins.listPlugins")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "listPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Plugins"}`)
	})

	Method("getPlugin", func() {
		Description("Get a plugin with its servers and assignments.")

		Payload(func() {
			Attribute("id", String, func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			GET("/rpc/plugins.getPlugin")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "getPlugin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Plugin"}`)
	})

	Method("createPlugin", func() {
		Description("Create a new plugin.")

		Payload(func() {
			Extend(CreatePluginForm)
			security.SessionPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			POST("/rpc/plugins.createPlugin")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createPlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "createPlugin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreatePlugin"}`)
	})

	Method("updatePlugin", func() {
		Description("Update plugin metadata.")

		Payload(func() {
			Extend(UpdatePluginForm)
			security.SessionPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			PUT("/rpc/plugins.updatePlugin")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updatePlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "updatePlugin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdatePlugin"}`)
	})

	Method("deletePlugin", func() {
		Description("Delete a plugin.")

		Payload(func() {
			Attribute("id", String, func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/plugins.deletePlugin")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deletePlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "deletePlugin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeletePlugin"}`)
	})

	Method("addPluginServer", func() {
		Description("Add an MCP server to a plugin.")

		Payload(func() {
			Extend(AddPluginServerForm)
			security.SessionPayload()
		})

		Result(PluginServerModel)

		HTTP(func() {
			POST("/rpc/plugins.addPluginServer")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "addPluginServer")
		Meta("openapi:extension:x-speakeasy-name-override", "addPluginServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddPluginServer"}`)
	})

	Method("updatePluginServer", func() {
		Description("Update a server's configuration within a plugin.")

		Payload(func() {
			Extend(UpdatePluginServerForm)
			security.SessionPayload()
		})

		Result(PluginServerModel)

		HTTP(func() {
			PUT("/rpc/plugins.updatePluginServer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updatePluginServer")
		Meta("openapi:extension:x-speakeasy-name-override", "updatePluginServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdatePluginServer"}`)
	})

	Method("removePluginServer", func() {
		Description("Remove a server from a plugin.")

		Payload(func() {
			Attribute("id", String, func() {
				Description("The plugin server ID to remove.")
				Format(FormatUUID)
			})
			Attribute("plugin_id", String, func() {
				Format(FormatUUID)
			})
			Required("id", "plugin_id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/plugins.removePluginServer")
			Param("id")
			Param("plugin_id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removePluginServer")
		Meta("openapi:extension:x-speakeasy-name-override", "removePluginServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemovePluginServer"}`)
	})

	Method("setPluginAssignments", func() {
		Description("Replace all assignments for a plugin with the given list of principal URNs.")

		Payload(func() {
			Extend(SetPluginAssignmentsForm)
			security.SessionPayload()
		})

		Result(func() {
			Required("assignments")
			Attribute("assignments", ArrayOf(PluginAssignmentModel), "The updated assignments.")
		})

		HTTP(func() {
			PUT("/rpc/plugins.setPluginAssignments")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setPluginAssignments")
		Meta("openapi:extension:x-speakeasy-name-override", "setPluginAssignments")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetPluginAssignments"}`)
	})

	Method("getGitHubInstallURL", func() {
		Description("Get the GitHub App installation URL and whether it is already installed.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Required("url", "installed")
			Attribute("url", String, "GitHub App installation URL.")
			Attribute("installed", Boolean, "Whether the app is already installed on at least one account.")
		})

		HTTP(func() {
			GET("/rpc/plugins.getGitHubInstallURL")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGitHubInstallURL")
		Meta("openapi:extension:x-speakeasy-name-override", "getGitHubInstallURL")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GitHubInstallURL"}`)
	})

	Method("connectGitHub", func() {
		Description("Connect the organization to a GitHub App installation, creating a repo in the customer's org.")

		Payload(func() {
			Extend(ConnectGitHubForm)
			security.SessionPayload()
		})

		Result(GitHubConnectionModel)

		HTTP(func() {
			POST("/rpc/plugins.connectGitHub")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "connectGitHub")
		Meta("openapi:extension:x-speakeasy-name-override", "connectGitHub")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ConnectGitHub"}`)
	})

	Method("disconnectGitHub", func() {
		Description("Disconnect the organization's GitHub integration.")

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/plugins.disconnectGitHub")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "disconnectGitHub")
		Meta("openapi:extension:x-speakeasy-name-override", "disconnectGitHub")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DisconnectGitHub"}`)
	})

	Method("getGitHubConnection", func() {
		Description("Get the current GitHub connection for the organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(GitHubConnectionModel)

		HTTP(func() {
			GET("/rpc/plugins.getGitHubConnection")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGitHubConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "getGitHubConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GitHubConnection"}`)
	})

	Method("publishPlugins", func() {
		Description("Generate platform-specific plugin packages and push them to the connected GitHub repository.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(PublishPluginsResult)

		HTTP(func() {
			POST("/rpc/plugins.publishPlugins")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "publishPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "publishPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PublishPlugins"}`)
	})

	Method("downloadPluginPackage", func() {
		Description("Download a ZIP archive of generated plugin packages for the organization, filtered by platform.")

		Payload(func() {
			Attribute("platform", String, func() {
				Description("Target platform to download plugins for.")
				Enum("claude", "cursor")
			})
			Required("platform")
			security.SessionPayload()
		})

		Result(func() {
			Attribute("content_type", String)
			Attribute("content_disposition", String)
			Required("content_type", "content_disposition")
		})

		HTTP(func() {
			GET("/rpc/plugins.downloadPluginPackage")
			Param("platform")
			security.SessionHeader()
			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_disposition:Content-Disposition")
			})
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "downloadPluginPackage")
		Meta("openapi:extension:x-speakeasy-name-override", "downloadPluginPackage")
	})
})
