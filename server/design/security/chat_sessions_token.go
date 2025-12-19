package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/auth"
	. "goa.design/goa/v3/dsl"
)

// ChatSessionsToken defines the security scheme for chat sessions token-based authentication
var ChatSessionsToken = JWTSecurity(auth.ChatSessionsTokenSecurityScheme, func() {
	Description("Gram Chat Sessions token based auth.")
})

var ChatSessionsTokenPayload = func() {
	Token("chat_sessions_token", String)
}

var ChatSessionsTokenHeader = func() {
	Header(fmt.Sprintf("chat_sessions_token:%s", auth.ChatSessionsTokenHeader), String, "Chat Sessions token header")
}

var ChatSessionsTokenNamedHeader = func(name string) {
	Header(fmt.Sprintf("chat_sessions_token:%s", name), String, "Chat Sessions Token header")
}
