package constants

import "time"

const (
	KeySecurityScheme = "apikey"
	APIKeyHeader      = "Gram-Key"

	FunctionTokenSecurityScheme = "function_token"
	FunctionTokenHeader         = "Authorization"

	SessionSecurityScheme      = "session"
	SessionHeader              = "Gram-Session"
	SessionCookie              = "gram_session"
	SessionIdleTimeout         = 72 * time.Hour
	SessionCookieMaxAgeSeconds = int(SessionIdleTimeout / time.Second)

	// SessionTransferNonceCookie binds a cross-domain session transfer to the
	// browser that started it on the target host. It outlives the 60 second
	// transfer code by a small margin.
	SessionTransferNonceCookie              = "__Host-gram_transfer_nonce"
	SessionTransferNonceCookieMaxAgeSeconds = 90

	ChatSessionsTokenSecurityScheme = "chat_sessions_token"
	ChatSessionsTokenHeader         = "Gram-Chat-Session" //nolint:gosec // this is a valid header name

	ProjectSlugSecuritySchema = "project_slug"
	ProjectHeader             = "Gram-Project"

	AdminAuthSecurityScheme = "admin_auth"
	AdminSessionCookie      = "gram_admin"
	AdminLoginStateCookie   = "gram_admin_login_state"

	WorkOSSignatureSecurityScheme = "workos_signature"
	WorkOSSignatureHeader         = "WorkOS-Signature"
)
