package organizations

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("organizations", func() {
	Description("Organization membership, invitations, and directory.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("get", func() {
		Description("Get the active organization from the session.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(shared.Organization)

		HTTP(func() {
			GET("/rpc/organizations.get")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganization")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Organization"}`)
	})

	Method("sendInvite", func() {
		Description("Send a WorkOS invitation for the active organization.")

		Payload(func() {
			Attribute("email", String, "Email address to invite.")
			Attribute("role_id", String, "Optional role ID for the invitee.")
			Required("email")
			security.SessionPayload()
		})

		Result(OrganizationInvitation)

		HTTP(func() {
			POST("/rpc/organizations.sendInvite")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "sendInvite")
		Meta("openapi:extension:x-speakeasy-name-override", "sendInvite")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SendInvite"}`)
	})

	Method("revokeInvite", func() {
		Description("Revoke a pending WorkOS invitation.")

		Payload(func() {
			Attribute("invitation_id", String, "WorkOS invitation ID.")
			Required("invitation_id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/organizations.revokeInvite")
			Param("invitation_id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "revokeInvite")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeInvite")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeInvite"}`)
	})

	Method("updateInviteRole", func() {
		Description("Change the role assigned to a pending WorkOS invitation.")

		Payload(func() {
			Attribute("invitation_id", String, "WorkOS invitation ID.")
			Attribute("role_id", String, "Role ID to assign to the invitee.")
			Required("invitation_id", "role_id")
			security.SessionPayload()
		})

		Result(OrganizationInvitation)

		HTTP(func() {
			PUT("/rpc/organizations.updateInviteRole")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateInviteRole")
		Meta("openapi:extension:x-speakeasy-name-override", "updateInviteRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateInviteRole"}`)
	})

	Method("listInvites", func() {
		Description("List pending WorkOS invitations for the active organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListInvitesResult)

		HTTP(func() {
			GET("/rpc/organizations.listInvites")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listInvites")
		Meta("openapi:extension:x-speakeasy-name-override", "listInvites")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListInvites"}`)
	})

	Method("listUsers", func() {
		Description("List users in the active organization from Gram organization_user_relationships.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListUsersResult)

		HTTP(func() {
			GET("/rpc/organizations.listUsers")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listOrganizationUsers")
		Meta("openapi:extension:x-speakeasy-name-override", "listUsers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListOrganizationUsers"}`)
	})

	Method("removeUser", func() {
		Description("Remove a user from the active organization in Gram and delete their WorkOS organization membership.")

		Payload(func() {
			Attribute("user_id", String, "Gram user ID to remove.")
			Required("user_id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/organizations.removeUser")
			Param("user_id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removeOrganizationUser")
		Meta("openapi:extension:x-speakeasy-name-override", "removeUser")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveOrganizationUser"}`)
	})

	Method("enableWebhooks", func() {
		Description("Enable  webhooks for the active organization.")

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/organizations.enableWebhooks")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "enableWebhooks")
		Meta("openapi:extension:x-speakeasy-name-override", "enableWebhooks")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnableWebhooks"}`)
	})

	Method("disableWebhooks", func() {
		Description("Disable  webhooks for the active organization.")

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/organizations.disableWebhooks")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "disableWebhooks")
		Meta("openapi:extension:x-speakeasy-name-override", "disableWebhooks")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DisableWebhooks"}`)
	})

	Method("createPortalSession", func() {
		Description("Create a webhook portal session.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(CreatePortalSessionResult)

		HTTP(func() {
			POST("/rpc/organizations.createPortalSession")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createPortalSession")
		Meta("openapi:extension:x-speakeasy-name-override", "createPortalSession")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreatePortalSession"}`)
	})
})

// OrganizationInvitation is a non-sensitive admin view (no invitation token or accept URL).
var OrganizationInvitation = Type("OrganizationInvitation", func() {
	Attribute("id", String, "WorkOS invitation ID.")
	Attribute("email", String, "Invitee email address.")
	Attribute("state", String, "Invitation lifecycle state.", func() {
		Enum("pending", "accepted", "expired", "revoked")
	})
	Attribute("accepted_at", String, "When the invitation was accepted.", func() {
		Format(FormatDateTime)
	})
	Attribute("revoked_at", String, "When the invitation was revoked.", func() {
		Format(FormatDateTime)
	})
	Attribute("inviter_user_id", String, "Gram user ID of the inviter, when known.")
	Attribute("role_slug", String, "WorkOS role slug assigned when the invite is accepted.")
	Attribute("expires_at", String, "When the invitation expires.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "email", "state", "created_at", "updated_at")
})

// OrganizationInvitationAccept is the public accept-flow view: enough to render copy and redirect,
// without WorkOS invitation IDs or audit timestamps.
var OrganizationInvitationAccept = Type("OrganizationInvitationAccept", func() {
	Attribute("email", String, "Invitee email address.")
	Attribute("state", String, "Invitation lifecycle state.", func() {
		Enum("pending", "accepted", "expired", "revoked")
	})
	Attribute("organization_name", String, "Gram organization display name when the org is linked in Gram; empty if unknown.")
	Attribute("accept_invitation_url", String, "URL to complete acceptance in WorkOS (may be empty when not actionable).")
	Required("email", "state", "organization_name", "accept_invitation_url")
})

// OrganizationUser is a row from organization_user_relationships joined with the users table.
var OrganizationUser = Type("OrganizationUser", func() {
	Attribute("id", String, "Gram relationship row ID.")
	Attribute("organization_id", String, "Gram organization ID.")
	Attribute("user_id", String, "Gram user ID.")
	Attribute("name", String, "User display name.")
	Attribute("email", String, "User email address.")
	Attribute("photo_url", String, "User photo URL.")
	Attribute("workos_membership_id", String, "WorkOS organization membership ID when known.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_login", String, "Timestamp of the user's most recent login.", func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "user_id", "name", "email", "created_at", "updated_at")
})

var ListInvitesResult = Type("ListInvitesResult", func() {
	Required("invitations")
	Attribute("invitations", ArrayOf(OrganizationInvitation), "Pending invitations for the organization only; accepted, expired, and revoked invitations are omitted.")
})

var ListUsersResult = Type("ListUsersResult", func() {
	Required("users")
	Attribute("users", ArrayOf(OrganizationUser), "Users linked to the organization in Gram.")
})

var CreatePortalSessionResult = Type("CreatePortalSessionResult", func() {
	Attribute("url", String, "URL for the webhook portal session.")
	Attribute("token", String, "Front-end token for the webhook portal session.")

	Required("url", "token")
})
