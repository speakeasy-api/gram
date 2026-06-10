package remotesessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("remoteSessionIssuers", func() {
	Description("Manage remote_session_issuer records — upstream Authorization Server identity records that Gram talks to as an OAuth client.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("discoverRemoteSessionIssuer", func() {
		Description("Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createRemoteSessionIssuer. No persistence.")

		Payload(func() {
			Attribute("issuer", String, "Issuer URL to discover (e.g. https://login.linear.com).")
			Required("issuer")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuerDraft)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.discover")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "discoverRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "discover")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DiscoverRemoteSessionIssuer"}`)
	})

	Method("createRemoteSessionIssuer", func() {
		Description("Create a new remote_session_issuer.")

		Payload(func() {
			Extend(CreateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRemoteSessionIssuer"}`)
	})

	Method("updateRemoteSessionIssuer", func() {
		Description("Update fields on an existing remote_session_issuer.")

		Payload(func() {
			Extend(UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRemoteSessionIssuer"}`)
	})

	Method("listRemoteSessionIssuers", func() {
		Description("List remote_session_issuers in the caller's project.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/remoteSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionIssuers"}`)
	})

	Method("getRemoteSessionIssuer", func() {
		Description("Get a remote_session_issuer by id or by slug. Provide exactly one.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The remote_session_issuer slug.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/remoteSessionIssuers.get")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionIssuer"}`)
	})

	Method("deleteRemoteSessionIssuer", func() {
		Description("Soft-delete a remote_session_issuer. Blocked if any remote_session_clients still reference it.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/remoteSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRemoteSessionIssuer"}`)
	})
})

// organizationRemoteSessionIssuers manages organization-level (cross-project)
// remote_session_issuers — rows with a NULL project_id scoped to an
// organization. These are administered by org admins and inherited by every
// project in the org. Security is organization-scoped (no ProjectSlug); RBAC
// gates writes on org:admin and reads on org:read.
var _ = Service("organizationRemoteSessionIssuers", func() {
	Description("Manage organization-level remote_session_issuer records — cross-project upstream Authorization Server identity records inherited by every project in the organization.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createOrganizationRemoteSessionIssuer", func() {
		Description("Create a new organization-level remote_session_issuer.")

		Payload(func() {
			Extend(CreateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationRemoteSessionIssuer"}`)
	})

	Method("updateOrganizationRemoteSessionIssuer", func() {
		Description("Update fields on an existing organization-level remote_session_issuer.")

		Payload(func() {
			Extend(UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateOrganizationRemoteSessionIssuer"}`)
	})

	Method("listOrganizationRemoteSessionIssuers", func() {
		Description("List organization-level remote_session_issuers in the caller's organization.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(ListRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuers"}`)
	})

	Method("getOrganizationRemoteSessionIssuer", func() {
		Description("Get an organization-level remote_session_issuer by id.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuer"}`)
	})

	Method("deleteOrganizationRemoteSessionIssuer", func() {
		Description("Soft-delete an organization-level remote_session_issuer. Blocked if any remote_session_clients still reference it.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		HTTP(func() {
			DELETE("/rpc/organizationRemoteSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrganizationRemoteSessionIssuer"}`)
	})
})

var CreateRemoteSessionIssuerForm = Type("CreateRemoteSessionIssuerForm", func() {
	Description("Form for creating a remote_session_issuer.")

	Attribute("slug", String, "Project-unique slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Optional display name. Stored NULL when empty; clients fall back to the issuer URL/slug.")
	Attribute("logo_asset_id", String, "Optional logo asset id.", func() {
		Format(FormatUUID)
	})
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; absent for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI.")
	Attribute("scopes_supported", ArrayOf(String), "Scopes advertised by the issuer.")
	Attribute("grant_types_supported", ArrayOf(String), "Grant types advertised by the issuer.")
	Attribute("response_types_supported", ArrayOf(String), "Response types advertised by the issuer.")
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String), "Token endpoint auth methods advertised by the issuer.")
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour. Default false.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer. Default false.")

	Required("slug", "issuer")
})

var UpdateRemoteSessionIssuerForm = Type("UpdateRemoteSessionIssuerForm", func() {
	Description("Form for updating a remote_session_issuer. All non-id fields are optional patches.")

	Attribute("id", String, "The remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Rename the slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Set or clear the display name. An empty string clears it to NULL.")
	Attribute("logo_asset_id", String, "Set the logo asset id.", func() {
		Format(FormatUUID)
	})
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint.")
	Attribute("jwks_uri", String, "Upstream JWKS URI.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean)
	Attribute("passthrough", Boolean)

	Required("id")
})

var RemoteSessionIssuer = Type("RemoteSessionIssuer", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to.")

	Attribute("id", String, "The remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	// No FormatUUID: organization-level issuers have no project and serialize
	// this as an empty string, which a UUID format check would reject.
	Attribute("project_id", String, "The owning project id. Empty for organization-level issuers.")
	Attribute("organization_id", String, "The owning organization id. Empty for legacy rows not yet backfilled.")
	Attribute("slug", String, "Project-unique slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Optional display name; null when unset.")
	Attribute("logo_asset_id", String, "Optional logo asset id; null when unset.", func() {
		Format(FormatUUID)
	})
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; null for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI; null when not advertised.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "organization_id", "slug", "issuer", "oidc", "passthrough", "created_at", "updated_at")
})

var RemoteSessionIssuerDraft = Type("RemoteSessionIssuerDraft", func() {
	Meta("struct:pkg:path", "types")

	Description("A draft remote_session_issuer returned by discover. Same shape as RemoteSessionIssuer minus id/project_id/timestamps, plus discovery_warnings describing any RFC 8414 deviations.")

	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; null for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI; null when not advertised.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer.")
	Attribute("discovery_warnings", ArrayOf(String), "Warnings describing any RFC 8414 deviations encountered during discovery.")

	Required("issuer", "oidc", "passthrough", "discovery_warnings")
})

var ListRemoteSessionIssuersResult = Type("ListRemoteSessionIssuersResult", func() {
	Description("Result type for listing remote_session_issuers.")

	Attribute("items", ArrayOf(RemoteSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
