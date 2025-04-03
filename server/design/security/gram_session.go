package security

import (
	. "goa.design/goa/v3/dsl"
)

const SessionSecurityScheme = "session"

// Session defines the security scheme for session-based authentication
var Session = APIKeySecurity(SessionSecurityScheme, func() {
	Description("Session based auth. By cookie or header.")
})

var SessionPayload = func() {
	APIKey(SessionSecurityScheme, "session_token", String)
}

var WriteSessionCookie = func() {
	Cookie("session_cookie:session", String, func() {
	})
	// TODO: We want to restrict cookie domain here
	CookieMaxAge(2592000) // 30 days in seconds
	CookieSecure()
	CookieHTTPOnly()
}

var DeleteSessionCookie = func() {
	Cookie("session_cookie:session", String, func() {
	})
	CookieMaxAge(0)
	CookieSecure()
	CookieHTTPOnly()
}

var SessionHeader = func() {
	Header("session_token:Gram-Session", String, "Session header")
}
