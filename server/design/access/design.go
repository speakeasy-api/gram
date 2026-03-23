package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("access", func() {
	Description("Manage access permissions for users and roles across your organization.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listGrants", func() {
		Description("List all permissions in your organization, optionally filtered to a specific user or role.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("Filter to a specific user or role (e.g. \"user:user_abc\", \"role:admin\"). Omit to return all grants.")
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListGrantsResult)

		HTTP(func() {
			GET("/rpc/access.listGrants")
			Param("principal_urn")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Grants"}`)
	})

	Method("upsertGrants", func() {
		Description("Grant permissions to one or more users or roles. Safe to call multiple times — if a permission already exists it is left unchanged.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(UpsertGrantsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(UpsertGrantsResult)

		HTTP(func() {
			POST("/rpc/access.upsertGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "upsert")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertGrants"}`)
	})

	Method("removeGrants", func() {
		Description("Revoke specific permissions from users or roles. Each entry must exactly match an existing grant (who, what action, which resource).")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(RemoveGrantsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.removeGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removeGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "remove")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveGrants"}`)
	})

	Method("removePrincipalGrants", func() {
		Description("Revoke all permissions for a specific user or role.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("The user or role to revoke all permissions from (e.g. \"user:user_abc\", \"role:admin\").")
				Meta("struct:field:type", "urn.Principal", "github.com/speakeasy-api/gram/server/internal/urn")
			})
			Required("principal_urn")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.removePrincipalGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removePrincipalGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "removePrincipal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemovePrincipalGrants"}`)
	})
})

var RemoveGrantEntry = Type("RemoveGrantEntry", func() {
	Description("A permission to revoke, identified by who holds it, what action it covers, and which resource it applies to.")
	Required("principal_urn", "scope", "resource")

	Attribute("principal_urn", String, func() {
		Description("The user or role that holds this permission (e.g. \"user:user_abc\", \"role:admin\").")
		Meta("struct:field:type", "urn.Principal", "github.com/speakeasy-api/gram/server/internal/urn")
	})
	Attribute("scope", String, func() {
		Description("The action being permitted (e.g. \"build:read\", \"mcp:connect\").")
		MinLength(3)
		MaxLength(60)
	})
	Attribute("resource", String, func() {
		Description("The resource this permission applies to. Use \"*\" for unrestricted access.")
		MaxLength(260)
	})
})

var RemoveGrantsForm = Type("RemoveGrantsForm", func() {
	Attribute("grants", ArrayOf(RemoveGrantEntry), func() {
		Description("The permissions to revoke.")
		MinLength(1)
		MaxLength(100)
	})
	Required("grants")
})

var AddGrantEntry = Type("AddGrantEntry", func() {
	Description("A permission to grant: who gets it, what action they can perform, and which resource it applies to.")
	Required("principal_urn", "scope", "resource")

	Attribute("principal_urn", String, func() {
		Description("The user or role receiving this permission (e.g. \"user:user_abc\", \"role:admin\").")
		Meta("struct:field:type", "urn.Principal", "github.com/speakeasy-api/gram/server/internal/urn")
	})
	Attribute("scope", String, func() {
		Description("The action being permitted (e.g. \"build:read\", \"mcp:connect\").")
		MinLength(3)
		MaxLength(60)
	})
	Attribute("resource", String, func() {
		Description("The resource this permission applies to. Use \"*\" for unrestricted access.")
		MaxLength(260)
	})
})

var UpsertGrantsForm = Type("UpsertGrantsForm", func() {
	Attribute("grants", ArrayOf(AddGrantEntry), func() {
		Description("The permissions to grant.")
		MinLength(1)
		MaxLength(100)
	})
	Required("grants")
})

var Grant = Type("Grant", func() {
	Description("A permission record giving a user or role the ability to perform an action on a resource.")
	Required("id", "organization_id", "principal_urn", "principal_type", "scope", "resource", "created_at", "updated_at")

	Attribute("id", String, func() {
		Description("Unique identifier of this permission.")
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization this permission belongs to.")
	Attribute("principal_urn", String, func() {
		Description("The user or role that holds this permission (e.g. \"user:user_abc\", \"role:admin\").")
	})
	Attribute("principal_type", String, func() {
		Description("Whether the principal is a user or a role.")
	})
	Attribute("scope", String, func() {
		Description("The action this permission allows (e.g. \"build:read\", \"mcp:connect\").")
	})
	Attribute("resource", String, func() {
		Description("The resource this permission applies to. \"*\" means all resources.")
	})
	Attribute("created_at", String, func() {
		Description("When this permission was granted.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When this permission was last updated.")
		Format(FormatDateTime)
	})
})

var ListGrantsResult = Type("ListGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The permissions in your organization.")
})

var UpsertGrantsResult = Type("UpsertGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The permissions that were created or already existed.")
})
