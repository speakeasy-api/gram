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
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListGrants"}`)
	})

	Method("upsertGrant", func() {
		Description("Create or update a principal grant. If a grant with the same (org, principal, scope, resource) already exists, its updated_at timestamp is refreshed.")

		Payload(func() {
			Extend(UpsertGrantForm)
			security.SessionPayload()
		})

		Result(Grant)

		HTTP(func() {
			POST("/rpc/access.upsertGrant")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "upsert")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertGrant"}`)
	})

	Method("deleteGrant", func() {
		Description("Delete a specific principal grant by ID.")

		Payload(func() {
			Attribute("id", String, func() {
				Description("The ID of the grant to delete.")
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			Param("id")
			DELETE("/rpc/access.deleteGrant")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGrant"}`)
	})
})

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

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
