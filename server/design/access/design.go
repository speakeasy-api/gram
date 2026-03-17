package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("access", func() {
	Description("Managing RBAC principal grants for an organization.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listGrants", func() {
		Description("List all principal grants for an organization, optionally filtered by principal URN.")

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("Optional principal URN to filter by (e.g. \"user:user_abc\", \"role:admin\"). Omit to list all grants.")
			})
			security.SessionPayload()
		})

		Result(ListGrantsResult)

		HTTP(func() {
			GET("/rpc/access.listGrants")
			Param("principal_urn")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Grants"}`)
	})

	Method("upsertGrants", func() {
		Description("Create or update one or more principal grants in batch. For each grant, if one with the same (org, principal, scope, resource) already exists, the record is kept as is.")

		Payload(func() {
			Attribute("grants", ArrayOf(UpsertGrantForm), func() {
				Description("The list of grants to upsert.")
				MinLength(1)
				MaxLength(100)
			})
			Required("grants")
			security.SessionPayload()
		})

		Result(UpsertGrantsResult)

		HTTP(func() {
			POST("/rpc/access.upsertGrants")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "upsert")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertGrants"}`)
	})

	Method("removeGrants", func() {
		Description("Remove one or more grants by their exact (principal, scope, resource) tuples.")

		Payload(func() {
			Attribute("grants", ArrayOf(RemoveGrantEntry), func() {
				Description("The list of grants to remove, each identified by (principal_urn, scope, resource).")
				MinLength(1)
				MaxLength(100)
			})
			Required("grants")
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.removeGrants")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removeGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "remove")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveGrants"}`)
	})

	Method("removePrincipalGrants", func() {
		Description("Remove all grants for a specific principal within the organization.")

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("The principal URN whose grants should be removed (e.g. \"user:user_abc\", \"role:admin\").")
				MinLength(3)
				MaxLength(260)
			})
			Required("principal_urn")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/access.removePrincipalGrants")
			Param("principal_urn")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removePrincipalGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "removePrincipal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemovePrincipalGrants"}`)
	})
})

var RemoveGrantEntry = Type("RemoveGrantEntry", func() {
	Description("Identifies a single grant to remove by its (principal, scope, resource) tuple.")
	Required("principal_urn", "scope")

	Attribute("principal_urn", String, func() {
		Description("The principal URN (e.g. \"user:user_abc\", \"role:admin\").")
		MinLength(3)
		MaxLength(260)
	})
	Attribute("scope", String, func() {
		Description("The scope of the grant (e.g. \"build:read\").")
		MinLength(3)
		MaxLength(60)
	})
	Attribute("resource", String, func() {
		Description("The resource of the grant. Defaults to \"*\".")
		Default("*")
		MaxLength(260)
	})
})

var UpsertGrantForm = Type("UpsertGrantForm", func() {
	Description("Form for creating or updating a principal grant.")
	Required("principal_urn", "scope")

	Attribute("principal_urn", String, func() {
		Description("The principal URN (e.g. \"user:user_abc\", \"role:admin\").")
		MinLength(3) // shortest valid: "x:y"
		MaxLength(260)
	})
	Attribute("scope", String, func() {
		Description("The scope to grant (e.g. \"build:read\", \"mcp:connect\").")
		MinLength(3)
		MaxLength(60)
	})
	Attribute("resource", String, func() {
		Description("The resource ID this grant applies to. Omit or set to \"*\" for unrestricted access.")
		Default("*")
		MaxLength(260)
	})
})

var Grant = Type("Grant", func() {
	Description("A principal grant representing a single RBAC permission.")
	Required("id", "organization_id", "principal_urn", "principal_type", "scope", "resource", "created_at", "updated_at")

	Attribute("id", String, func() {
		Description("Unique identifier of the grant.")
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization this grant belongs to.")
	Attribute("principal_urn", String, func() {
		Description("The principal URN (e.g. \"user:user_abc\", \"role:admin\").")
	})
	Attribute("principal_type", String, func() {
		Description("The type portion of the principal URN (e.g. \"user\", \"role\"). Derived from principal_urn.")
	})
	Attribute("scope", String, func() {
		Description("The scope being granted (e.g. \"build:read\").")
	})
	Attribute("resource", String, func() {
		Description("The resource this grant applies to. \"*\" means unrestricted.")
	})
	Attribute("created_at", String, func() {
		Description("When the grant was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the grant was last updated.")
		Format(FormatDateTime)
	})
})

var ListGrantsResult = Type("ListGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The list of grants.")
})

var UpsertGrantsResult = Type("UpsertGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The list of grants that were added or updated.")
})
