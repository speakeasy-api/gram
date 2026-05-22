package security

import (
	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

// AdminAuth defines the security scheme for admin staff authenticated via a
// Google Workspace session. The credential is an opaque server-side session ID
// carried in the gram_admin cookie, issued by /admin/auth.callback after
// Google login and re-validated against Google on every request.
var AdminAuth = APIKeySecurity(constants.AdminAuthSecurityScheme, func() {
	Description("Admin session auth for admin endpoints. Cookie-only credential; session is validated against Google on every request.")
})

var AdminAuthPayload = func() {
	APIKey(constants.AdminAuthSecurityScheme, "admin_session_token", String)
}
