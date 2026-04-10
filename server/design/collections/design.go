package collections

import (
	"github.com/speakeasy-api/gram/server/design/externalmcp"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("collections", func() {
	Description("MCP collection operations")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Create an MCP collection within the organization")

		Payload(func() {
			Attribute("name", String, "Display name for the collection", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("slug", String, "URL-friendly identifier for the collection", func() {
				MinLength(1)
				MaxLength(60)
			})
			Attribute("description", String, "Description of the collection", func() {
				MaxLength(500)
			})
			Attribute("mcp_registry_namespace", String, "Registry namespace (e.g., 'com.speakeasy.acme.my-tools')", func() {
				MinLength(1)
				MaxLength(200)
			})
			Attribute("visibility", String, "Visibility of the collection", func() {
				Enum("public", "private")
				Default("private")
			})
			Attribute("toolset_ids", ArrayOf(String), "Toolset IDs to attach to the collection")
			Required("name", "slug", "mcp_registry_namespace")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			POST("/rpc/collections.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("list", func() {
		Description("List MCP collections in the organization")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("collections", ArrayOf(MCPCollection), "List of collections")
			Required("collections")
		})

		HTTP(func() {
			GET("/rpc/collections.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listCollections")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListCollections"}`)
	})

	Method("update", func() {
		Description("Update an MCP collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection to update", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "Display name for the collection", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("description", String, "Description of the collection", func() {
				MaxLength(500)
			})
			Attribute("visibility", String, "Visibility of the collection", func() {
				Enum("public", "private")
			})
			Required("collection_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			PATCH("/rpc/collections.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
	})

	Method("delete", func() {
		Description("Delete an MCP collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection to delete", func() {
				Format(FormatUUID)
			})
			Required("collection_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/collections.delete")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("collection_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("attachServer", func() {
		Description("Attach a server (toolset) to a collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "ID of the toolset to attach", func() {
				Format(FormatUUID)
			})
			Required("collection_id", "toolset_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			POST("/rpc/collections.attachServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "attachServerToCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "attachServer")
	})

	Method("detachServer", func() {
		Description("Detach a server (toolset) from a collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "ID of the toolset to detach", func() {
				Format(FormatUUID)
			})
			Required("collection_id", "toolset_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/collections.detachServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "detachServerFromCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "detachServer")
	})

	Method("listServers", func() {
		Description("List published MCP servers from a collection")

		Payload(func() {
			Attribute("collection_slug", String, "Slug of the collection to serve")
			Required("collection_slug")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("servers", ArrayOf(externalmcp.ExternalMCPServer), "List of available MCP servers")
			Required("servers")
		})

		HTTP(func() {
			GET("/rpc/collections.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("collection_slug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listCollectionServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
	})
})

var MCPCollection = Type("MCPCollection", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP collection within an organization")

	Attribute("id", String, "Collection ID", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name for the collection")
	Attribute("description", String, "Description of the collection")
	Attribute("slug", String, "URL-friendly identifier")
	Attribute("mcp_registry_namespace", String, "Registry namespace")
	Attribute("visibility", String, "Visibility of the collection", func() {
		Enum("public", "private")
	})

	Required("id", "name", "slug", "visibility")
})
