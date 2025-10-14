package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/auth"
	. "goa.design/goa/v3/dsl"
)

// Session defines the security scheme for session-based authentication
var Session = APIKeySecurity(auth.SessionSecurityScheme, func() {
	Description("Session based auth. By cookie or header.")
})

var SessionPayload = func() {
	APIKey(auth.SessionSecurityScheme, "session_token", String)
}

var WriteSessionCookie = func() {
	Cookie(fmt.Sprintf("session_cookie:%s", auth.SessionCookie), String, func() {
	})
	// TODO: We want to restrict cookie domain here
	CookieMaxAge(2592000) // 30 days in seconds
	CookieSecure()
	CookieHTTPOnly()
	CookiePath("/")
}

var DeleteSessionCookie = func() {
	Cookie(fmt.Sprintf("session_cookie:%s", auth.SessionCookie), String, func() {
	})
	CookieMaxAge(0)
	CookieSecure()
	CookieHTTPOnly()
	CookiePath("/")
}

// This can be safely removed after 11/13/25 after all `/rpc` scoped session cookies are sure to be expired
var DeleteLegacyScopedSessionCookie = func() {
	Cookie(fmt.Sprintf("session_cookie:%s", auth.SessionCookie), String, func() {
	})
	CookieMaxAge(0)
	CookieSecure()
	CookieHTTPOnly()
	CookiePath("/rpc")
}

var SessionHeader = func() {
	Header(fmt.Sprintf("session_token:%s", auth.SessionHeader), String, "Session header")
}
