package remotesessions

import (
	. "goa.design/goa/v3/dsl"
)

// Org-admin types — the read/aggregate shapes that power the organization
// administrator UI (AIS-119) for remote_sessions. They back the
// organizationRemoteSessions service in design.go.

var ListOrganizationRemoteSessionsResult = Type("ListOrganizationRemoteSessionsResult", func() {
	Description("Result type for the remote_sessions minted against a remote_session_client.")

	Attribute("items", ArrayOf(RemoteSession))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

// RevokeAllRemoteSessionsResult reports how many sessions a revoke-all cleared.
var RevokeAllRemoteSessionsResult = Type("RevokeAllRemoteSessionsResult", func() {
	Description("Result type for revoking all of a client's remote_sessions.")

	Attribute("revoked_count", Int, "Number of remote_sessions revoked.")

	Required("revoked_count")
})
