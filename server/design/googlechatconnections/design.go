package googlechatconnections

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var Connection = Type("GoogleChatConnection", func() {
	Attribute("id", String)
	Attribute("email", String)
	Attribute("status", String)
	Attribute("generation", String)
	Attribute("granted_scopes", ArrayOf(String))
	Required("id", "email", "status", "generation", "granted_scopes")
})
var Binding = Type("GoogleChatBinding", func() {
	Attribute("id", String)
	Attribute("connection_id", String)
	Attribute("project_id", String)
	Attribute("assistant_id", String)
	Attribute("space_name", String)
	Attribute("display_name", String)
	Attribute("prefix", String)
	Attribute("status", String)
	Attribute("last_error", String)
	Attribute("revision", Int64)
	Required("id", "connection_id", "project_id", "assistant_id", "space_name", "display_name", "prefix", "status", "revision")
})
var Member = Type("GoogleChatMember", func() {
	Attribute("id", String)
	Attribute("binding_id", String)
	Attribute("google_user_name", String)
	Attribute("display_name", String)
	Attribute("user_id", String)
	Attribute("active", Boolean)
	Attribute("revision", Int64)
	Required("id", "binding_id", "google_user_name", "display_name", "active", "revision")
})
var Space = Type("GoogleChatSpace", func() {
	Attribute("name", String)
	Attribute("display_name", String)
	Attribute("external_user_allowed", Boolean)
	Required("name", "display_name", "external_user_allowed")
})
var _ = Service("googleChatConnections", func() {
	Description("Connect a Google account, manage assistant spaces, and map participants to existing Speakeasy identities.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	HTTP(func() { shared.DeclareHTTPErrorResponses() })
	Method("list", func() {
		Payload(func() {
			Meta("openapi:typename", "ListGoogleChatRequestBody")
			security.SessionPayload()
		})
		Result(func() {
			Meta("openapi:typename", "ListGoogleChatResult")
			Attribute("connections", ArrayOf(Connection))
			Attribute("bindings", ArrayOf(Binding))
			Attribute("authorization_configured", Boolean)
			Required("connections", "bindings", "authorization_configured")
		})
		HTTP(func() { GET("/rpc/googleChatConnections.list"); security.SessionHeader(); Response(StatusOK) })
		meta("listGoogleChatConnections", "list", "GoogleChatConnections")
	})
	Method("begin", func() {
		Payload(func() {
			Meta("openapi:typename", "BeginGoogleChatRequestBody")
			security.SessionPayload()
		})
		Result(func() {
			Meta("openapi:typename", "BeginGoogleChatResult")
			Attribute("authorization_url", String)
			Required("authorization_url")
		})
		HTTP(func() { POST("/rpc/googleChatConnections.begin"); security.SessionHeader(); Response(StatusOK) })
		meta("beginGoogleChatConnection", "begin", "BeginGoogleChatConnection")
	})
	Method("disconnect", func() {
		Payload(func() {
			Meta("openapi:typename", "DisconnectGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("id", String)
			Attribute("generation", String)
			Required("id", "generation")
		})
		HTTP(func() {
			POST("/rpc/googleChatConnections.disconnect")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		meta("disconnectGoogleChatConnection", "disconnect", "DisconnectGoogleChatConnection")
	})
	Method("listSpaces", func() {
		Payload(func() {
			Meta("openapi:typename", "ListSpacesGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("connection_id", String)
			Required("connection_id")
		})
		Result(func() {
			Meta("openapi:typename", "ListSpacesGoogleChatResult")
			Attribute("spaces", ArrayOf(Space))
			Required("spaces")
		})
		HTTP(func() {
			GET("/rpc/googleChatConnections.listSpaces")
			security.SessionHeader()
			Param("connection_id")
			Response(StatusOK)
		})
		meta("listGoogleChatSpaces", "listSpaces", "GoogleChatSpaces")
	})
	Method("bind", func() {
		Payload(func() {
			Meta("openapi:typename", "BindGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("request_id", String, "Stable UUID reused when retrying this setup.", func() { Format(FormatUUID) })
			Attribute("connection_id", String)
			Attribute("project_id", String)
			Attribute("assistant_id", String)
			Attribute("space_name", String, "Existing space, or omit to create one.")
			Attribute("display_name", String)
			Attribute("prefix", String, func() { Default("assistant:"); MinLength(1); MaxLength(100) })
			Attribute("invite_emails", ArrayOf(String))
			Required("request_id", "connection_id", "project_id", "assistant_id", "display_name", "prefix")
		})
		Result(Binding)
		HTTP(func() { POST("/rpc/googleChatConnections.bind"); security.SessionHeader(); Response(StatusOK) })
		meta("bindGoogleChatSpace", "bind", "BindGoogleChatSpace")
	})
	Method("listMembers", func() {
		Payload(func() {
			Meta("openapi:typename", "ListMembersGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("binding_id", String)
			Required("binding_id")
		})
		Result(func() {
			Meta("openapi:typename", "ListMembersGoogleChatResult")
			Attribute("members", ArrayOf(Member))
			Required("members")
		})
		HTTP(func() {
			GET("/rpc/googleChatConnections.listMembers")
			security.SessionHeader()
			Param("binding_id")
			Response(StatusOK)
		})
		meta("listGoogleChatMembers", "listMembers", "GoogleChatMembers")
	})
	Method("syncMembers", func() {
		Payload(func() {
			Meta("openapi:typename", "SyncMembersGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("binding_id", String)
			Required("binding_id")
		})
		HTTP(func() {
			POST("/rpc/googleChatConnections.syncMembers")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		meta("syncGoogleChatMembers", "syncMembers", "SyncGoogleChatMembers")
	})
	Method("mapMember", func() {
		Payload(func() {
			Meta("openapi:typename", "MapMemberGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("binding_id", String)
			Attribute("member_id", String)
			Attribute("user_id", String, "Existing active Speakeasy person, or omit to revoke.")
			Attribute("revision", Int64)
			Required("binding_id", "member_id", "revision")
		})
		Result(Member)
		HTTP(func() { POST("/rpc/googleChatConnections.mapMember"); security.SessionHeader(); Response(StatusOK) })
		meta("mapGoogleChatMember", "mapMember", "MapGoogleChatMember")
	})
	Method("updateBinding", func() {
		Payload(func() {
			Meta("openapi:typename", "UpdateBindingGoogleChatRequestBody")
			security.SessionPayload()
			Attribute("binding_id", String)
			Attribute("revision", Int64)
			Attribute("action", String, func() { Enum("pause", "resume", "delete", "repair") })
			Required("binding_id", "revision", "action")
		})
		Result(Binding)
		HTTP(func() { POST("/rpc/googleChatConnections.updateBinding"); security.SessionHeader(); Response(StatusOK) })
		meta("updateGoogleChatBinding", "updateBinding", "UpdateGoogleChatBinding")
	})
})

func meta(operation, name, hook string) {
	Meta("openapi:operationId", operation)
	Meta("openapi:extension:x-speakeasy-name-override", name)
	Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"`+hook+`"}`)
}
