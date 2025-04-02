package sessions

import (
	. "goa.design/goa/v3/dsl"
)

// GramSession defines the security scheme for session-based authentication
var GramSession = APIKeySecurity("gram_session", func() {
	Description("Gram Session based auth. By cookie or header.")
})

var SessionPayload = func() {
	APIKey("gram_session", "gram_session_token", String)
}

var WriteSessionCookie = func() {
	Cookie("gram_session_cookie:gram_session", String, func() {
	})
	// TODO: We want to restrict cookie domain here
	CookieMaxAge(2592000) // 30 days in seconds
	CookieSecure()
	CookieHTTPOnly()
}

var DeleteSessionCookie = func() {
	Cookie("gram_session_cookie:gram_session", String, func() {
	})
	CookieMaxAge(0)
	CookieSecure()
	CookieHTTPOnly()
}

var SessionHeader = func() {
	Header("gram_session_token:Gram-Session", String, "Session header")
}
