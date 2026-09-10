package admin

import (
	. "goa.design/goa/v3/dsl"

	legacy "github.com/speakeasy-api/gram/server/design/platformadmin/remotesessions"
	rsissuers "github.com/speakeasy-api/gram/server/design/remotesessionissuers"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

func remoteSessionIssuerMethods() {
	Method("createGlobalIssuer", func() {
		Description("Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.CreateRemoteSessionIssuerForm)
			security.AdminAuthPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/admin/remote-session-issuers.createGlobalIssuer")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminCreateGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "createGlobalIssuer")
	})
	Method("getGlobalIssuerDuplicatePreflight", func() {
		Description("Report the global remote_session_issuers that already describe an upstream issuer URL, so the catalog create and edit forms can warn before curating a second entry for the same authorization server. Requires platform admin.\n\nScoped to the global partition only. Tenant issuers naming the same URL are deliberately not reported here — listGlobalIssuerConvergenceCandidates is the surface for those, and it is keyed on a global issuer that already exists.\n\nThe global tier is unique on slug but not on issuer, so nothing prevents a duplicate catalog entry and this warning is the only thing that will catch one. Advisory all the same: it never blocks the write. Matching uses the same canonicalization as the tenant-facing preflights, and an unparseable URL returns no matches rather than an error.")

		Payload(func() {
			// Not Required: Goa cannot tell an absent query parameter from an
			// empty one, so requiring it would turn a blank form field into a
			// 400 at the transport decoder, contradicting the empty-match
			// contract below before the handler ever runs.
			Attribute("issuer", String, "The upstream issuer URL being entered (e.g. https://login.linear.app). Empty or unparseable returns no matches.")
			security.AdminAuthPayload()
		})

		Result(rsissuers.RemoteSessionIssuerDuplicatePreflight)

		HTTP(func() {
			GET("/admin/remote-session-issuers.getGlobalIssuerDuplicatePreflight")
			Param("issuer")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetGlobalRemoteSessionIssuerDuplicatePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuerDuplicatePreflight")
	})
	Method("listGlobalIssuers", func() {
		Description("List global remote_session_issuers. Requires platform admin.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.AdminAuthPayload()
		})

		Result(legacy.ListGlobalRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/admin/remote-session-issuers.list")
			Param("cursor")
			Param("limit")

			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "adminListGlobalRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalIssuers")
	})
	Method("getGlobalIssuer", func() {
		Description("Get a global remote_session_issuer by id. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		Result(legacy.GlobalRemoteSessionIssuer)

		HTTP(func() {
			GET("/admin/remote-session-issuers.getGlobalIssuer")
			Param("id")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuer")
	})
	Method("updateGlobalIssuer", func() {
		Description("Update a global remote_session_issuer. Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.UpdateRemoteSessionIssuerForm)
			security.AdminAuthPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/admin/remote-session-issuers.updateGlobalIssuer")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminUpdateGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGlobalIssuer")
	})
	Method("deleteGlobalIssuer", func() {
		Description("Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		HTTP(func() {
			DELETE("/admin/remote-session-issuers.deleteGlobalIssuer")
			Param("id")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminDeleteGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobalIssuer")
	})
	Method("fetchGlobalIssuerMetadata", func() {
		Description("Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createGlobalIssuer. Keyed by issuer URL; no record need exist and nothing is persisted. Requires platform admin.")

		Payload(func() {
			Attribute("issuer", String, "Issuer URL to fetch metadata for (e.g. https://login.linear.com).")
			Required("issuer")
			security.AdminAuthPayload()
		})

		Result(rsissuers.RemoteSessionIssuerDraft)

		HTTP(func() {
			POST("/admin/remote-session-issuers.fetchGlobalIssuerMetadata")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminFetchGlobalRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "fetchGlobalIssuerMetadata")
	})
	Method("refreshGlobalIssuerMetadata", func() {
		Description("Re-fetch an existing global remote_session_issuer's RFC 8414 metadata document and persist the discovered values. Keyed by issuer id. Only RFC 8414-derived columns are written — endpoints, the *_supported arrays, client_id_metadata_document_supported, and the documentation URLs. Gram behavior and display fields (oidc, passthrough, name, slug, logo, client setup documentation) are left alone. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		Result(rsissuers.RemoteSessionIssuerRefresh)

		HTTP(func() {
			POST("/admin/remote-session-issuers.refreshGlobalIssuerMetadata")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRefreshGlobalRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshGlobalIssuerMetadata")
	})
	Method("listGlobalIssuerConvergenceCandidates", func() {
		Description("List the organization- and project-level remote_session_issuers that describe the same upstream authorization server as a given global issuer, and so could be consolidated onto it. Matching is by canonical issuer URL, collapsing trailing-slash and default-port spellings. Each candidate carries its owning organization, the number of clients that would move, and the metadata differences that would block or accompany the migration. Requires platform admin.")

		Payload(func() {
			Attribute("target_id", String, "The global remote_session_issuer that candidates would be consolidated onto.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Required("target_id")
			security.AdminAuthPayload()
		})

		Result(legacy.ListIssuerConvergenceCandidatesResult)

		HTTP(func() {
			GET("/admin/remote-session-issuers.listGlobalIssuerConvergenceCandidates")
			Param("target_id")
			Param("cursor")
			Param("limit")

			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "adminListGlobalRemoteSessionIssuerConvergenceCandidates")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalIssuerConvergenceCandidates")
	})
	Method("getGlobalIssuerMigratePreflight", func() {
		Description("Authoritative impact summary for consolidating a tenant remote_session_issuer onto a global one: the clients that would move, the affected MCP servers, and every blocker (endpoint mismatches, conflicting MCP-server bindings). Also reports how many tenant-owned clients the target already carries, since those permanently block deleting it. Requires platform admin.")

		Payload(func() {
			Attribute("source_id", String, "The organization- or project-level remote_session_issuer to migrate away from.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The global remote_session_issuer to migrate onto.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.AdminAuthPayload()
		})

		Result(legacy.IssuerMigratePreflight)

		HTTP(func() {
			GET("/admin/remote-session-issuers.getGlobalIssuerMigratePreflight")
			Param("source_id")
			Param("target_id")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetGlobalRemoteSessionIssuerMigratePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuerMigratePreflight")
	})
	Method("migrateToGlobalIssuer", func() {
		Description("Consolidate an organization- or project-level remote_session_issuer onto a global one: re-point every client from the source issuer onto the target, then soft-delete the source. Existing remote sessions are preserved, so no user re-authenticates. The source may belong to any organization; the target must be a global issuer. Both must agree on issuer (compared canonically), token_endpoint, and authorization_endpoint. One source per call. Requires platform admin.")

		Payload(func() {
			Attribute("source_id", String, "The organization- or project-level remote_session_issuer to migrate away from; soft-deleted on success.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The global remote_session_issuer to migrate onto; survives and adopts the source's clients.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.AdminAuthPayload()

			// This payload is shape-identical to organizationRemoteSessionIssuers'
			// migrateIssuer, and Goa's OpenAPI emitter deduplicates request bodies by
			// shape rather than by name: without an explicit typename the two collapse
			// into one schema and this method's generated SDK type takes the other
			// service's name.
			Meta("openapi:typename", "MigrateRemoteSessionIssuerRequestBody")
		})

		Result(legacy.MigrateRemoteSessionIssuerResult)

		HTTP(func() {
			POST("/admin/remote-session-issuers.migrateToGlobalIssuer")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminMigrateToGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "migrateToGlobalIssuer")
	})
}
