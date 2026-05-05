package design

import (
	. "goa.design/goa/v3/dsl" //nolint:staticcheck // Goa DSL convention
)

var ListInvitationsResult = Type("ListInvitationsResult", func() {
	Attribute("items", ArrayOf(Invitation), "Invitations on this page.")
	PaginationResult()
	Required("items")
})

var _ = Service("invitations", func() {
	Description("Dev-idp invitations: create, list, get, find-by-token, revoke, resend, accept. Mirrors the WorkOS user_management invitation lifecycle. The accept-flow is dev-idp-specific — clicking 'accept' in the dashboard hits `invitations.accept`, which flips state and find-or-creates the corresponding user + membership. Permanently unauthenticated.")

	Method("create", func() {
		Description("Create a pending invitation. The token is generated server-side; the dev-idp doesn't actually send email.")

		Payload(func() {
			Attribute("email", String, "Invitee email.")
			Attribute("organization_id", String, "Organization UUID the invitee will be added to on accept.", func() {
				Format(FormatUUID)
			})
			Attribute("inviter_user_id", String, "Inviter UUID (optional).", func() {
				Format(FormatUUID)
			})
			Required("email", "organization_id")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.create")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List invitations for an organization, keyset-paginated by id.")

		Payload(func() {
			PaginationPayload()
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Required("organization_id")
		})

		Result(ListInvitationsResult)

		HTTP(func() {
			POST("/rpc/invitations.list")
			Response(StatusOK)
		})
	})

	Method("get", func() {
		Description("Fetch a single invitation by id.")

		Payload(func() {
			Attribute("id", String, "Invitation UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.get")
			Response(StatusOK)
		})
	})

	Method("findByToken", func() {
		Description("Resolve an invitation by its token. Used by the dashboard accept-flow URL.")

		Payload(func() {
			Attribute("token", String, "Opaque invitation token.")
			Required("token")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.findByToken")
			Response(StatusOK)
		})
	})

	Method("revoke", func() {
		Description("Revoke a pending invitation. Idempotent — repeated revokes return the same row.")

		Payload(func() {
			Attribute("id", String, "Invitation UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.revoke")
			Response(StatusOK)
		})
	})

	Method("resend", func() {
		Description("Touch the invitation's updated_at as if a fresh email were sent. The dev-idp doesn't actually send email.")

		Payload(func() {
			Attribute("id", String, "Invitation UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.resend")
			Response(StatusOK)
		})
	})

	Method("accept", func() {
		Description("Accept an invitation: flip state to `accepted`, find-or-create the user for the invited email, and idempotently attach a membership. Returns the updated invitation.")

		Payload(func() {
			Attribute("id", String, "Invitation UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		Result(Invitation)

		HTTP(func() {
			POST("/rpc/invitations.accept")
			Response(StatusOK)
		})
	})
})
