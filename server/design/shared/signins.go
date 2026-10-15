package shared

import (
	. "goa.design/goa/v3/dsl"
)

var RevokeSignInsResult = Type("RevokeSignInsResult", func() {
	Meta("struct:pkg:path", "types")

	Description("What a sign-in revocation revoked.")

	Attribute("revoked_sign_ins", Int, "Number of sign-ins revoked.")
	Attribute("revoked_sessions", Int, "Number of MCP grants revoked.")
	Required("revoked_sign_ins", "revoked_sessions")
})
