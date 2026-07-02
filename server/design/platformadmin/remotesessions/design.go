// Package remotesessions declares the adminRemoteSessions Goa service: the
// platform-admin (Speakeasy-only) surface for curating "global"
// remote_session_issuer / remote_session_client records (project_id IS NULL AND
// organization_id IS NULL) shared across every organization. Implemented on the
// existing *remotesessions.Service; reuses the existing form/result types.
package remotesessions

import (
	. "goa.design/goa/v3/dsl"

	rsclients "github.com/speakeasy-api/gram/server/design/remotesessionclients"
	rsissuers "github.com/speakeasy-api/gram/server/design/remotesessionissuers"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("adminRemoteSessions", func() {
	Description("Platform-admin management of global remote_session_issuer / remote_session_client records — shared across every organization (project_id NULL, organization_id NULL). Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	// --- Global issuers ---

	Method("createGlobalIssuer", func() {
		Description("Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.CreateRemoteSessionIssuerForm)
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.createGlobalIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "createGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGlobalRemoteSessionIssuer"}`)
	})

	Method("listGlobalIssuers", func() {
		Description("List global remote_session_issuers. Requires platform admin.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
		})

		Result(rsissuers.ListRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.listGlobalIssuers")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listGlobalRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalIssuers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuers"}`)
	})

	Method("getGlobalIssuer", func() {
		Description("Get a global remote_session_issuer by id. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.getGlobalIssuer")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuer"}`)
	})

	Method("updateGlobalIssuer", func() {
		Description("Update a global remote_session_issuer. Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.updateGlobalIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGlobalRemoteSessionIssuer"}`)
	})

	Method("deleteGlobalIssuer", func() {
		Description("Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/adminRemoteSessions.deleteGlobalIssuer")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGlobalRemoteSessionIssuer"}`)
	})

	// --- Global clients ---

	Method("createGlobalClient", func() {
		Description("Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.")

		Payload(func() {
			Extend(CreateGlobalRemoteSessionClientForm)
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.createGlobalClient")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGlobalRemoteSessionClient"}`)
	})

	Method("listGlobalClients", func() {
		Description("List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.")

		Payload(func() {
			Attribute("remote_session_issuer_id", String, "The global remote_session_issuer id to list clients for.", func() {
				Format(FormatUUID)
			})
			Required("remote_session_issuer_id")
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
		})

		Result(rsclients.ListRemoteSessionClientsResult)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.listGlobalClients")
			Param("remote_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listGlobalRemoteSessionClients")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalClients")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionClients"}`)
	})

	Method("getGlobalClient", func() {
		Description("Get a global remote_session_client by id. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.getGlobalClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionClient"}`)
	})

	Method("updateGlobalClient", func() {
		Description("Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.")

		Payload(func() {
			Extend(rsclients.UpdateRemoteSessionClientForm)
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.updateGlobalClient")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGlobalRemoteSessionClient"}`)
	})

	Method("deleteGlobalClient", func() {
		Description("Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/adminRemoteSessions.deleteGlobalClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGlobalRemoteSessionClient"}`)
	})
})

// scopePattern / audiencePattern and the scopeAttribute / audienceAttribute
// helpers mirror the project-scoped remoteSessionClients design so the
// global-admin create path enforces the same boundary validation on values that
// are later sent to upstream OAuth endpoints. Keep in sync with
// design/remotesessionclients.
const (
	scopePattern    = `^[!#-[\]-~]+$`
	audiencePattern = `^[!-~]+$`
)

func scopeAttribute(description string) {
	Description(description)
	Elem(func() {
		Pattern(scopePattern)
		MaxLength(128)
	})
}

func audienceAttribute() {
	Pattern(audiencePattern)
	MaxLength(512)
}

// CreateGlobalRemoteSessionClientForm is the global-client create form: like
// CreateRemoteSessionClientForm but without user_session_issuer attachments.
var CreateGlobalRemoteSessionClientForm = Type("CreateGlobalRemoteSessionClientForm", func() {
	Description("Form for creating a global remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.")

	Attribute("remote_session_issuer_id", String, "The owning global remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "client_id supplied by the caller.")
	Attribute("client_secret", String, "client_secret supplied by the caller. Gram encrypts before persisting.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", func() {
		Enum("client_secret_basic", "client_secret_post", "none")
	})
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", audienceAttribute)

	Required("remote_session_issuer_id", "client_id")
})
