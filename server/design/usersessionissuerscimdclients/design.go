package usersessionissuerscimdclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessionIssuersCimdClients", func() {
	Description("Manage the CIMD (OAuth Client ID Metadata Document) clients a user_session_issuer admits: the read-only preset catalog Gram curates, plus per-issuer custom document URLs.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listPresets", func() {
		Description("List Gram's curated CIMD preset catalog. Issuers whose admission mode is 'presets' — the default — admit every enabled entry here automatically, with no per-issuer configuration. The catalog is global and contains no tenant data.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListCimdClientPresetsResult)

		HTTP(func() {
			GET("/rpc/userSessionIssuersCimdClients.listPresets")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listCimdClientPresets")
		Meta("openapi:extension:x-speakeasy-name-override", "listPresets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CimdClientPresets"}`)
	})

	Method("createUserSessionIssuerCimdClient", func() {
		Description("Allow an additional CIMD document URL on a user_session_issuer, beyond the preset catalog. The URL is validated for draft-ietf-oauth-client-id-metadata-document-02 §3 syntax and rejected outright when malformed. Gram also probes the document and returns a warning when the fetch or validation fails, but still saves the entry — a vendor's document host being briefly unreachable must not block configuration.")

		Payload(func() {
			Extend(CreateUserSessionIssuerCimdClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(CreateUserSessionIssuerCimdClientResult)

		HTTP(func() {
			POST("/rpc/userSessionIssuersCimdClients.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateUserSessionIssuerCimdClient"}`)
	})

	Method("listUserSessionIssuerCimdClients", func() {
		Description("List the custom CIMD document URLs configured on a user_session_issuer. Does not include the preset catalog — call listPresets for that.")

		Payload(func() {
			Attribute("user_session_issuer_id", String, "The user_session_issuer whose custom CIMD clients are listed.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Required("user_session_issuer_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListUserSessionIssuerCimdClientsResult)

		HTTP(func() {
			GET("/rpc/userSessionIssuersCimdClients.list")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessionIssuerCimdClients")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuerCimdClients"}`)
	})

	Method("getUserSessionIssuerCimdClient", func() {
		Description("Get a single custom CIMD document URL entry by id.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer_cimd_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuerCimdClient)

		HTTP(func() {
			GET("/rpc/userSessionIssuersCimdClients.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuerCimdClient"}`)
	})

	Method("deleteUserSessionIssuerCimdClient", func() {
		Description("Remove a custom CIMD document URL from a user_session_issuer. New authorization requests from that client are denied immediately; sessions already issued to it are unaffected and continue until they expire.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer_cimd_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/userSessionIssuersCimdClients.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteUserSessionIssuerCimdClient"}`)
	})
})

var CreateUserSessionIssuerCimdClientForm = Type("CreateUserSessionIssuerCimdClientForm", func() {
	Description("Form for allowing an additional CIMD document URL on a user_session_issuer.")

	Attribute("user_session_issuer_id", String, "The user_session_issuer the URL is allowed on.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id_metadata_uri", String, "The exact https URL the client presents as its client_id. Matched byte for byte at authorization time — the spec forbids normalization, so this must be the vendor's published URL exactly.")

	Required("user_session_issuer_id", "client_id_metadata_uri")
})

var CreateUserSessionIssuerCimdClientResult = Type("CreateUserSessionIssuerCimdClientResult", func() {
	Description("Result of allowing a CIMD document URL, including any non-fatal probe warning.")

	Attribute("client", UserSessionIssuerCimdClient)
	Attribute("probe_warning", String, "Set when Gram could not fetch or validate the document at this URL. The entry was still saved; the URL will be admitted, but a client presenting it will fail at authorization time until the document is reachable and valid.")

	Required("client")
})

var UserSessionIssuerCimdClient = Type("UserSessionIssuerCimdClient", func() {
	Meta("struct:pkg:path", "types")

	Description("A CIMD document URL explicitly allowed on a user_session_issuer, additive to the preset catalog.")

	Attribute("id", String, "The user_session_issuer_cimd_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The owning project id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer this URL is allowed on.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id_metadata_uri", String, "The exact https URL admitted as a client_id.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "user_session_issuer_id", "client_id_metadata_uri", "created_at", "updated_at")
})

var ListUserSessionIssuerCimdClientsResult = Type("ListUserSessionIssuerCimdClientsResult", func() {
	Description("Result type for listing an issuer's custom CIMD document URLs.")

	Attribute("items", ArrayOf(UserSessionIssuerCimdClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

var CimdClientPreset = Type("CimdClientPreset", func() {
	Meta("struct:pkg:path", "types")

	Description("An entry in Gram's curated CIMD preset catalog.")

	Attribute("vendor_key", String, "Stable identifier for the publishing vendor. Not unique — a vendor may publish several client documents.")
	Attribute("display_name", String, "Human-readable client name.")
	Attribute("client_id_metadata_uri", String, "The https URL this client presents as its client_id. Usually exact; when is_pattern is true this is a wildcard pattern rather than a literal URL.")
	Attribute("is_pattern", Boolean, "True when client_id_metadata_uri contains a `*` wildcard, matching a whole namespace of client_ids rather than one URL. Used for vendors that mint a separate metadata document per connector or install, so their client_ids cannot be enumerated. A `*` stands for exactly one path segment and never widens the host.")
	Attribute("enabled", Boolean, "Whether presets-mode issuers currently admit this entry. Disabled entries are listed so operators can see that Gram knows about the vendor.")

	Required("vendor_key", "display_name", "client_id_metadata_uri", "is_pattern", "enabled")
})

var ListCimdClientPresetsResult = Type("ListCimdClientPresetsResult", func() {
	Description("Result type for listing the CIMD preset catalog.")

	Attribute("items", ArrayOf(CimdClientPreset))

	Required("items")
})
