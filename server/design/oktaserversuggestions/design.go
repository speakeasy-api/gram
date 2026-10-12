package oktaserversuggestions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var RemoteHeader = Type("OktaServerSuggestionRemoteHeader", func() {
	Description("A header the remote endpoint expects, as the catalog entry lists it; the install flow collects required ones from the administrator.")
	Required("name", "is_required", "is_secret")
	Attribute("name", String, "Header name.")
	Attribute("description", String)
	Attribute("is_required", Boolean)
	Attribute("is_secret", Boolean)
})

var Remote = Type("OktaServerSuggestionRemote", func() {
	Description("One remote endpoint of the suggested server, as the catalog entry lists it. The administrator picks one when there are several.")
	Required("type", "url", "headers")
	Attribute("type", String, "Transport type, for example streamable-http.")
	Attribute("url", String, "Endpoint URL to create the remote MCP server with.", func() {
		Format(FormatURI)
	})
	Attribute("headers", ArrayOf(RemoteHeader), "Headers the endpoint expects, in catalog order.")
})

var Application = Type("OktaServerSuggestionApplication", func() {
	Description("One synced Okta application instance that maps to the suggested server.")
	Required("okta_app_id", "label", "name", "sign_on_mode", "xaa_supported", "user_assignments", "group_assignments")
	Attribute("okta_app_id", String, "Okta application id.")
	Attribute("label", String, "Admin-facing label. Admin-editable; never a key.")
	Attribute("name", String, "Okta application template name; the key that matched the catalog entry.")
	Attribute("sign_on_mode", String)
	Attribute("xaa_supported", Boolean, "Whether this instance's sign-on mode supports Cross App Access. False for password-vault (SWA) and bookmark instances.")
	Attribute("user_assignments", Int, "Live direct and group-derived user assignments.")
	Attribute("group_assignments", Int, "Live group assignments.")
})

var Suggestion = Type("OktaServerSuggestion", func() {
	Description("A Speakeasy-owned catalog entry whose Okta mapping matches at least one active, assigned application in the organization's Okta snapshot. Suggest only; nothing is created until the administrator accepts through the remote MCP install flow.")
	Required("registry_entry_id", "server_name", "description", "remotes", "okta_applications", "state", "installed_urls", "supports_dcr")
	Attribute("registry_entry_id", String, "Catalog entry id; the key for dismiss and restore.", func() {
		Format(FormatUUID)
	})
	Attribute("server_name", String, "Catalog server name, for example io.example/linear; also the registry specifier for catalog lookups.")
	Attribute("title", String, "Catalog display title when the entry has one.")
	Attribute("description", String)
	Attribute("documentation_url", String, "Public documentation URL when the entry has one.")
	Attribute("icon_url", String, "HTTPS URL of the entry's icon when it has one.", func() {
		Format(FormatURI)
	})
	Attribute("supports_dcr", Boolean, "Whether the catalog records that the server's authorization server supports OAuth dynamic client registration; install flows default to per-user OAuth when true.")
	Attribute("remotes", ArrayOf(Remote), "Remote endpoints in catalog order.")
	Attribute("okta_applications", ArrayOf(Application), "Matching Okta application instances, by label.")
	Attribute("xaa_issuer", String, "Vendor Issuer URL for Cross App Access when the catalog knows it.")
	Attribute("state", String, "open: not yet acted on; dismissed: an administrator dismissed it; installed: a live MCP server in one of the organization's projects fronts one of the entry's URLs (trailing slashes ignored; legacy deployment attachments are not consulted). Installed outranks dismissed.", func() {
		Enum("open", "dismissed", "installed")
	})
	Attribute("dismissed_at", String, "When the suggestion was dismissed; present while a dismissal is recorded, whatever the state.", func() {
		Format(FormatDateTime)
	})
	Attribute("installed_urls", ArrayOf(String), "The entry URLs the organization already fronts with a live MCP server.")
})

var ListResult = Type("ListOktaServerSuggestionsResult", func() {
	Required("suggestions", "open_count", "total_count")
	Attribute("suggestions", ArrayOf(Suggestion), "Open suggestions first, then by server name. By default only open ones; include_all adds dismissed and installed.")
	Attribute("open_count", Int, "Suggestions the administrator has not acted on.")
	Attribute("total_count", Int, "Matching entries before filtering.")
	Attribute("connection_id", String, "The organization's identity provider connection the snapshot came from.", func() {
		Format(FormatUUID)
	})
	Attribute("snapshot_at", String, "When the applications snapshot was last synced; the suggestions are only as fresh as it.", func() {
		Format(FormatDateTime)
	})
})

var _ = Service("oktaServerSuggestions", func() {
	Description("MCP servers suggested from the organization's Okta applications: catalog entries whose staff-curated Okta mapping matches a synced, assigned application. Administrators accept by creating the server through the usual remote MCP flow, or dismiss; dismissals are remembered per organization and audited.")

	shared.DeclareErrorResponses()
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})
	Error(string(oops.CodeFailedPrecondition), func() { Description(oops.CodeFailedPrecondition.UserMessage()) })
	HTTP(func() {
		shared.DeclareHTTPErrorResponses()
		Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() {
			ContentType("application/json")
		})
		Response(string(oops.CodeFailedPrecondition), StatusPreconditionFailed, func() {
			ContentType("application/json")
		})
	})

	Method("list", func() {
		Description("List the catalog servers suggested from the Okta applications snapshot. Requires org:admin, the okta-connections rollout and a verified identity provider connection.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("include_all", Boolean, "Include dismissed and installed suggestions.", func() {
				Default(false)
			})
		})

		Result(ListResult)

		HTTP(func() {
			GET("/rpc/oktaServerSuggestions.list")
			security.SessionHeader()
			Param("include_all")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listOktaServerSuggestions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OktaServerSuggestions"}`)
	})

	Method("dismiss", func() {
		Description("Dismiss a suggestion currently made to the organization. Remembered until restored, even across Okta reconnects. Requires org:admin and the okta-connections rollout.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "DismissOktaServerSuggestionRequestBody")
			Attribute("registry_entry_id", String, "Catalog entry of the suggestion to dismiss.", func() {
				Format(FormatUUID)
			})
			Required("registry_entry_id")
		})

		Result(Suggestion)

		HTTP(func() {
			POST("/rpc/oktaServerSuggestions.dismiss")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "dismissOktaServerSuggestion")
		Meta("openapi:extension:x-speakeasy-name-override", "dismiss")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DismissOktaServerSuggestion"}`)
	})

	Method("restore", func() {
		Description("Restore a dismissed suggestion currently made to the organization. Requires org:admin and the okta-connections rollout.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "RestoreOktaServerSuggestionRequestBody")
			Attribute("registry_entry_id", String, "Catalog entry of the suggestion to restore.", func() {
				Format(FormatUUID)
			})
			Required("registry_entry_id")
		})

		Result(Suggestion)

		HTTP(func() {
			POST("/rpc/oktaServerSuggestions.restore")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "restoreOktaServerSuggestion")
		Meta("openapi:extension:x-speakeasy-name-override", "restore")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RestoreOktaServerSuggestion"}`)
	})
})
