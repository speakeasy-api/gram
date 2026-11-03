package remotesessions

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var PrincipalRemoteSessionBinding = Type("PrincipalRemoteSessionBinding", func() {
	for _, name := range []string{"id", "principal_id", "user_session_issuer_id", "remote_session_client_id", "remote_session_id"} {
		Attribute(name, String, func() { Format(FormatUUID) })
	}
	Attribute("remote_session", RemoteSession, "The canonical upstream session view, present only while the exact attached grant is available. Absent for unavailable bindings, which remain detachable by id. Never includes credentials.")
	Required("id", "principal_id", "user_session_issuer_id", "remote_session_client_id", "remote_session_id")
})

func principalBindingMethods() {
	bindingRefreshMethods()
	for _, name := range []string{"listBindings", "attachBinding", "detachBinding"} {
		Method(name, func() {
			Description("Manage exact remote session attachments for an agent. Reads require agent authorization or write permission. Changes require an ordinary human session and agent write permission. Attachment requires ownership of the upstream session; editors can remove any assignment. Never returns credentials.")
			Security(security.Session, security.ProjectSlug)
			Payload(func() {
				Attribute("principal_id", String, func() { Format(FormatUUID) })
				Attribute("user_session_issuer_id", String, func() { Format(FormatUUID) })
				Required("principal_id", "user_session_issuer_id")
				if name == "attachBinding" {
					Attribute("remote_session_id", String, func() { Format(FormatUUID) })
					Required("remote_session_id")
				}
				if name == "detachBinding" {
					Attribute("id", String, func() { Format(FormatUUID) })
					Required("id")
				}
				security.SessionPayload()
				security.ProjectPayload()
			})
			switch name {
			case "listBindings":
				Result(func() { Attribute("items", ArrayOf(PrincipalRemoteSessionBinding)); Required("items") })
			case "attachBinding":
				Result(PrincipalRemoteSessionBinding)
			}
			HTTP(func() {
				if name == "listBindings" {
					GET("/rpc/remoteSessions." + name)
					Param("principal_id")
					Param("user_session_issuer_id")
				} else {
					POST("/rpc/remoteSessions." + name)
				}
				security.SessionHeader()
				security.ProjectHeader()
				Response(StatusOK)
			})
			Meta("openapi:operationId", name)
			Meta("openapi:extension:x-speakeasy-name-override", name)
		})
	}
}

var BindingRefreshAttempt = Type("BindingRefreshAttempt", func() {
	Attribute("attempt_id", String, func() { Format(FormatUUID) })
	Required("attempt_id")
})

var BindingRefreshResult = Type("BindingRefreshResult", func() {
	Attribute("completed", Boolean, "Whether this popup committed an exact grant and replaced the attachment. False means no matching callback completed; the attempt is consumed without changing the attachment.")
	Attribute("binding", PrincipalRemoteSessionBinding)
	Required("completed")
})

func bindingRefreshMethods() {
	for _, name := range []string{"beginBindingRefresh", "completeBindingRefresh"} {
		Method(name, func() {
			Description("Refresh an agent attachment through one exact browser OAuth attempt. Requires an ordinary human with agent write permission and ownership of the attached connection. The one-time attempt is bound to the caller, agent, issuer and original attachment; completion never selects a different sign-in.")
			Security(security.Session, security.ProjectSlug)
			Payload(func() {
				Attribute("principal_id", String, func() { Format(FormatUUID) })
				Attribute("user_session_issuer_id", String, func() { Format(FormatUUID) })
				Required("principal_id", "user_session_issuer_id")
				if name == "beginBindingRefresh" {
					Attribute("binding_id", String, func() { Format(FormatUUID) })
					Required("binding_id")
				} else {
					Attribute("attempt_id", String, func() { Format(FormatUUID) })
					Required("attempt_id")
				}
				security.SessionPayload()
				security.ProjectPayload()
			})
			if name == "beginBindingRefresh" {
				Result(BindingRefreshAttempt)
			} else {
				Result(BindingRefreshResult)
			}
			HTTP(func() {
				POST("/rpc/remoteSessions." + name)
				security.SessionHeader()
				security.ProjectHeader()
				Response(StatusOK)
			})
			Meta("openapi:operationId", name)
			Meta("openapi:extension:x-speakeasy-name-override", name)
		})
	}
}
