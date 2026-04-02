package organizations

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

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
	Attribute("role_slug", String, "WorkOS role slug for the invitee.")
	Attribute("organization_id", String, "Gram organization ID.")
	Attribute("inviter_user_id", String, "Gram user ID of the inviter, when known.")
	Attribute("expires_at", String, "When the invitation expires.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "email", "state", "organization_id", "created_at", "updated_at")
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

// OrganizationUser is a row from organization_user_relationships (active members).
var OrganizationUser = Type("OrganizationUser", func() {
	Attribute("id", String, "Gram relationship row ID.")
	Attribute("organization_id", String, "Gram organization ID.")
	Attribute("user_id", String, "Gram user ID.")
	Attribute("workos_membership_id", String, "WorkOS organization membership ID when known.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "user_id", "created_at", "updated_at")
})

var ListInvitesResult = Type("ListInvitesResult", func() {
	Required("invitations")
	Attribute("invitations", ArrayOf(OrganizationInvitation), "Pending and historical invitations for the organization.")
})

var ListUsersResult = Type("ListUsersResult", func() {
	Required("users")
	Attribute("users", ArrayOf(OrganizationUser), "Users linked to the organization in Gram.")
})

var _ = Service("organizations", func() {
	Description("Organization membership, invitations, and directory.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("sendInvite", func() {
		Description("Send a WorkOS invitation for the active organization.")

		Payload(func() {
			Attribute("email", String, "Email address to invite.")
			Attribute("role_slug", String, "Optional WorkOS role slug for the invitee.")
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

	Method("getInviteByToken", func() {
		Description("Resolve a WorkOS invitation from its token (e.g. accept-flow).")

		NoSecurity()

		Payload(func() {
			Attribute("token", String, "Invitation token from the invite link.")
			Required("token")
		})

		Result(OrganizationInvitationAccept)

		HTTP(func() {
			GET("/rpc/organizations.getInviteByToken")
			Param("token")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getInviteByToken")
		Meta("openapi:extension:x-speakeasy-name-override", "getInviteByToken")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetInviteByToken"}`)
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
})
