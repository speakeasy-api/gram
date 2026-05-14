package design

import (
	. "goa.design/goa/v3/dsl"
)

// User mirrors the dev-idp `users` table (idp-design.md §5).
var User = Type("User", func() {
	Attribute("id", String, "User UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("email", String, "Email address (unique).")
	Attribute("display_name", String, "Display name.")
	Attribute("photo_url", String, "Optional photo URL.")
	Attribute("github_handle", String, "Optional GitHub handle.")
	Attribute("admin", Boolean, "Admin flag echoed by local-speakeasy validate.")
	Attribute("whitelisted", Boolean, "Whitelist flag echoed by local-speakeasy validate.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "email", "display_name", "admin", "whitelisted", "created_at", "updated_at")
})

// Organization mirrors the dev-idp `organizations` table (idp-design.md §5).
var Organization = Type("Organization", func() {
	Attribute("id", String, "Organization UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name.")
	Attribute("slug", String, "URL slug (unique).")
	Attribute("account_type", String, "Plan tier (`free`, etc.).")
	Attribute("workos_id", String, "Optional WorkOS organization id echoed by local-speakeasy validate.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "name", "slug", "account_type", "created_at", "updated_at")
})

// OrganizationRole mirrors the dev-idp `organization_roles` table.
// Surfaced from /rpc/organizationRoles.* and the WorkOS-emulation
// surface under /local-speakeasy/authorization/organizations/{id}/roles
// (idp-design.md §7.1, "WorkOS emulation").
var OrganizationRole = Type("OrganizationRole", func() {
	Attribute("id", String, "Role UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "Organization UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Role slug — unique within the organization.")
	Attribute("name", String, "Display name.")
	Attribute("description", String, "Free-form description; empty string when unset.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "slug", "name", "description", "created_at", "updated_at")
})

// Invitation mirrors the dev-idp `invitations` table. Backs the
// /rpc/invitations.* surface and the WorkOS-emulation surface under
// /local-speakeasy/user_management/invitations*.
var Invitation = Type("Invitation", func() {
	Attribute("id", String, "Invitation UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("email", String, "Invitee email address.")
	Attribute("organization_id", String, "Organization UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("state", String, "Invitation lifecycle state.", func() {
		Enum("pending", "accepted", "revoked", "expired")
	})
	Attribute("token", String, "Opaque token used by /findByToken and the accept-flow URL.")
	Attribute("inviter_user_id", String, "User UUID of the inviter, when known.", func() {
		Format(FormatUUID)
	})
	Attribute("accepted_at", String, "Timestamp when the invitation was accepted; empty for pending.", func() {
		Format(FormatDateTime)
	})
	Attribute("revoked_at", String, "Timestamp when the invitation was revoked; empty for pending.", func() {
		Format(FormatDateTime)
	})
	Attribute("expires_at", String, "Timestamp when the invitation expires.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "email", "organization_id", "state", "token", "expires_at", "created_at", "updated_at")
})

// Membership mirrors the dev-idp `memberships` table (idp-design.md §5).
var Membership = Type("Membership", func() {
	Attribute("id", String, "Membership UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("user_id", String, "User UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "Organization UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("role", String, "Role within the organization (default `member`).")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "user_id", "organization_id", "role", "created_at", "updated_at")
})

// PaginationPayload is mixed into every list-method payload. See idp-design.md
// §6: cursor + limit, default 50, max 100.
func PaginationPayload() {
	Attribute("cursor", String, "Opaque cursor returned by a previous list call.")
	Attribute("limit", Int, "Maximum items to return.", func() {
		Default(50)
		Minimum(1)
		Maximum(100)
	})
}

// PaginationResult is mixed into every list-method result. `next_cursor` is
// empty when the page exhausts the result set.
func PaginationResult() {
	Attribute("next_cursor", String, "Cursor for the next page, empty when exhausted.")
	Required("next_cursor")
}
