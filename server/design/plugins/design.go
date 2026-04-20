package plugins

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

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

	Method("downloadPluginPackage", func() {
		Description("Download a ZIP of a single plugin package for direct installation.")

		Payload(func() {
			Attribute("plugin_id", String, func() {
				Description("The plugin to download.")
				Format(FormatUUID)
			})
			Attribute("platform", String, func() {
				Description("Target platform to download plugins for.")
				Enum("claude", "cursor")
			})
			Required("plugin_id", "platform")
			security.SessionPayload()
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
