package teams

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("teams", func() {
	Description("Manages team members and invites for organizations in Gram.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listMembers", func() {
		Description("List all members of an organization.")

		Payload(func() {
			Required("organization_id")
			Attribute("organization_id", String, "The ID of the organization")
			security.SessionPayload()
		})
		Result(ListMembersResult)

		HTTP(func() {
			GET("/rpc/teams.listMembers")
			security.SessionHeader()
			Param("organization_id")
		})

		Meta("openapi:operationId", "listTeamMembers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMembers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListTeamMembers"}`)
	})

	Method("inviteMember", func() {
		Description("Invite a new member to the organization.")

		Payload(func() {
			Extend(InviteMemberForm)
			security.SessionPayload()
		})
		Result(InviteMemberResult)

		HTTP(func() {
			POST("/rpc/teams.invite")
			security.SessionHeader()
		})

		Meta("openapi:operationId", "inviteTeamMember")
		Meta("openapi:extension:x-speakeasy-name-override", "inviteMember")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "InviteTeamMember"}`)
	})

	Method("listInvites", func() {
		Description("List pending invites for an organization.")

		Payload(func() {
			Required("organization_id")
			Attribute("organization_id", String, "The ID of the organization")
			security.SessionPayload()
		})
		Result(ListInvitesResult)

		HTTP(func() {
			GET("/rpc/teams.listInvites")
			security.SessionHeader()
			Param("organization_id")
		})

		Meta("openapi:operationId", "listTeamInvites")
		Meta("openapi:extension:x-speakeasy-name-override", "listInvites")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListTeamInvites"}`)
	})

	Method("cancelInvite", func() {
		Description("Cancel a pending invite.")

		Payload(func() {
			Required("invite_id")
			Attribute("invite_id", String, "The ID of the invite to cancel", func() { Format(FormatUUID) })
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/teams.cancelInvite")
			security.SessionHeader()
			Param("invite_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "cancelTeamInvite")
		Meta("openapi:extension:x-speakeasy-name-override", "cancelInvite")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CancelTeamInvite"}`)
	})

	Method("resendInvite", func() {
		Description("Resend an invite email.")

		Payload(func() {
			Required("invite_id")
			Attribute("invite_id", String, "The ID of the invite to resend", func() { Format(FormatUUID) })
			security.SessionPayload()
		})
		Result(ResendInviteResult)

		HTTP(func() {
			POST("/rpc/teams.resendInvite")
			security.SessionHeader()
		})

		Meta("openapi:operationId", "resendTeamInvite")
		Meta("openapi:extension:x-speakeasy-name-override", "resendInvite")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResendTeamInvite"}`)
	})

	Method("removeMember", func() {
		Description("Remove a member from the organization.")

		Payload(func() {
			Required("organization_id", "user_id")
			Attribute("organization_id", String, "The ID of the organization")
			Attribute("user_id", String, "The ID of the user to remove")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/teams.removeMember")
			security.SessionHeader()
			Param("organization_id")
			Param("user_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removeTeamMember")
		Meta("openapi:extension:x-speakeasy-name-override", "removeMember")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveTeamMember"}`)
	})
})

// Types

var TeamMember = Type("TeamMember", func() {
	Required("id", "email", "display_name", "joined_at")

	Attribute("id", String, "The user's ID")
	Attribute("email", String, "The user's email address")
	Attribute("display_name", String, "The user's display name")
	Attribute("photo_url", String, "URL to the user's profile photo")
	Attribute("joined_at", String, func() {
		Description("When the user joined the organization")
		Format(FormatDateTime)
	})
})

var TeamInvite = Type("TeamInvite", func() {
	Required("id", "email", "status", "invited_by", "created_at", "expires_at")

	Attribute("id", String, "The invite ID", func() { Format(FormatUUID) })
	Attribute("email", String, "The invited email address")
	Attribute("status", String, func() {
		Enum("pending", "accepted", "expired", "cancelled")
	})
	Attribute("invited_by", String, "Name of the user who sent the invite")
	Attribute("created_at", String, func() {
		Description("When the invite was created")
		Format(FormatDateTime)
	})
	Attribute("expires_at", String, func() {
		Description("When the invite expires")
		Format(FormatDateTime)
	})
})

var ListMembersResult = Type("ListMembersResult", func() {
	Required("members")
	Attribute("members", ArrayOf(TeamMember), "List of organization members")
})

var InviteMemberForm = Type("InviteMemberForm", func() {
	Required("organization_id", "email")
	Attribute("organization_id", String, "The ID of the organization")
	Attribute("email", String, "Email address to invite", func() {
		Format(FormatEmail)
		MaxLength(255)
	})
})

var InviteMemberResult = Type("InviteMemberResult", func() {
	Required("invite")
	Attribute("invite", TeamInvite, "The created invite")
})

var ListInvitesResult = Type("ListInvitesResult", func() {
	Required("invites")
	Attribute("invites", ArrayOf(TeamInvite), "List of pending invites")
})

var ResendInviteResult = Type("ResendInviteResult", func() {
	Required("invite")
	Attribute("invite", TeamInvite, "The updated invite")
})
