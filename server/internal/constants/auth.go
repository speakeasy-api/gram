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

	// TransferInNonceCookiePrefix starts the name of the cookie that
	// auth.transferIn's start mode sets to bind a cross-domain session
	// transfer to this browser, and its callback mode checks. Each transfer
	// has its own cookie, named from its nonce hash, so two transfers in one
	// browser do not overwrite each other. It outlives the 60 second transfer
	// code by a small margin.
	TransferInNonceCookiePrefix        = "__Host-gram_transfer_in_"
	TransferInNonceCookieMaxAgeSeconds = 90

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
