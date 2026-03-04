package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

// Session defines the security scheme for session-based authentication
var Session = APIKeySecurity(constants.SessionSecurityScheme, func() {
	Description("Session based auth. By cookie or header.")
})

var SessionPayload = func() {
	APIKey(constants.SessionSecurityScheme, "session_token", String)
}

var WriteSessionCookie = func() {
	Cookie(fmt.Sprintf("session_cookie:%s", constants.SessionCookie), String, func() {
	})
	// TODO: We want to restrict cookie domain here
	CookieMaxAge(2592000) // 30 days in seconds
	CookieSecure()
	CookieHTTPOnly()
	CookiePath("/")
}

var DeleteSessionCookie = func() {
	Cookie(fmt.Sprintf("session_cookie:%s", constants.SessionCookie), String, func() {
	})
	// NOTE: We set max age to -1 to rather than 0 because go's Set-Cookie treats 0 as unset
	CookieMaxAge(-1)
	CookieSecure()
	CookieHTTPOnly()
	CookiePath("/")
}

var SessionHeader = func() {
	Header(fmt.Sprintf("session_token:%s", constants.SessionHeader), String, "Session header")
}
