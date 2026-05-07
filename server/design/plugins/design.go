package plugins

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

var _ = Service("plugins", func() {
	Description("Manage distributable plugin bundles of MCP servers and hooks.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listPlugins", func() {
		Description("List all plugins for the current project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListPluginsResult)

		HTTP(func() {
			GET("/rpc/plugins.listPlugins")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			GET("/rpc/plugins.getPlugin")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			POST("/rpc/plugins.createPlugin")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(PluginModel)

		HTTP(func() {
			PUT("/rpc/plugins.updatePlugin")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/plugins.deletePlugin")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(PluginServerModel)

		HTTP(func() {
			POST("/rpc/plugins.addPluginServer")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(PluginServerModel)

		HTTP(func() {
			PUT("/rpc/plugins.updatePluginServer")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/plugins.removePluginServer")
			Param("id")
			Param("plugin_id")
			security.SessionHeader()
			security.ProjectHeader()
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
			security.ProjectPayload()
		})

		Result(func() {
			Required("assignments")
			Attribute("assignments", ArrayOf(PluginAssignmentModel), "The updated assignments.")
		})

		HTTP(func() {
			PUT("/rpc/plugins.setPluginAssignments")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setPluginAssignments")
		Meta("openapi:extension:x-speakeasy-name-override", "setPluginAssignments")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetPluginAssignments"}`)
	})

	Method("downloadPluginPackage", func() {
		Description("Download a ZIP of a single plugin package for direct installation.")

		Payload(func() {
			Attribute("plugin_id", String, func() {
				Description("The plugin to download.")
				Format(FormatUUID)
			})
			Attribute("platform", String, func() {
				Description("Target platform to download plugins for.")
				Enum("claude", "cursor", "codex")
			})
			Required("plugin_id", "platform")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("content_type", String)
			Attribute("content_disposition", String)
			Required("content_type", "content_disposition")
		})

		HTTP(func() {
			GET("/rpc/plugins.downloadPluginPackage")
			Param("plugin_id")
			Param("platform")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK, func() {
				ContentType("application/zip")
				Header("content_type:Content-Type")
				Header("content_disposition:Content-Disposition")
			})
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "downloadPluginPackage")
		Meta("openapi:extension:x-speakeasy-name-override", "downloadPluginPackage")
	})

	Method("downloadObservabilityPlugin", func() {
		Description("Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.")

		Payload(func() {
			Attribute("platform", String, func() {
				Description("Target platform.")
				Enum("claude", "cursor")
			})
			Required("platform")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("content_type", String)
			Attribute("content_disposition", String)
			Required("content_type", "content_disposition")
		})

		HTTP(func() {
			GET("/rpc/plugins.downloadObservabilityPlugin")
			Param("platform")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK, func() {
				ContentType("application/zip")
				Header("content_type:Content-Type")
				Header("content_disposition:Content-Disposition")
			})
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "downloadObservabilityPlugin")
		Meta("openapi:extension:x-speakeasy-name-override", "downloadObservabilityPlugin")
	})

	Method("getPublishStatus", func() {
		Description("Check whether GitHub publishing is configured and connected for this project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(PublishStatusResult)

		HTTP(func() {
			GET("/rpc/plugins.getPublishStatus")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPublishStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "getPublishStatus")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PublishStatus"}`)
	})

	Method("publishPlugins", func() {
		Description("Generate and publish all plugin packages to a GitHub repository.")

		Payload(func() {
			Attribute("github_username", String, "GitHub username to add as a collaborator on the repo.")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(PublishPluginsResult)

		HTTP(func() {
			POST("/rpc/plugins.publishPlugins")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "publishPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "publishPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PublishPlugins"}`)
	})
})

// --- Models ---

// PluginServerModel represents a toolset-backed MCP server included in a plugin.
var PluginServerModel = Type("PluginServer", func() {
	Required("id", "toolset_id", "display_name", "policy", "sort_order", "created_at")

	Attribute("id", String, func() {
		Description("Unique plugin server identifier.")
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, func() {
		Description("Gram toolset ID.")
		Format(FormatUUID)
	})
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
	Attribute("slug", String, "Optional URL-safe identifier. Auto-generated from name if omitted.")
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
	Required("plugin_id", "toolset_id", "display_name")

	Attribute("plugin_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, func() {
		Description("Gram toolset ID for the MCP server.")
		Format(FormatUUID)
	})
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

var PublishStatusResult = Type("PublishStatusResult", func() {
	Required("configured", "connected")

	Attribute("configured", Boolean, "Whether GitHub publishing is configured on the server.")
	Attribute("connected", Boolean, "Whether this project has a GitHub connection.")
	Attribute("repo_owner", String, "GitHub repo owner, if connected.")
	Attribute("repo_name", String, "GitHub repo name, if connected.")
	Attribute("repo_url", String, "Full GitHub repository URL, if connected.")
	Attribute("marketplace_url", String, "URL-based Claude Code marketplace install URL — the value to pass to `/plugin marketplace add`. Present once a marketplace token has been minted, which happens automatically on the first publish.")
})

var PublishPluginsResult = Type("PublishPluginsResult", func() {
	Required("repo_url")
	Attribute("repo_url", String, "The URL of the published GitHub repository.")
})
