package constants

const (
	KeySecurityScheme = "apikey"
	APIKeyHeader      = "Gram-Key"

	FunctionTokenSecurityScheme = "function_token"
	FunctionTokenHeader         = "Authorization"

	SessionSecurityScheme = "session"
	SessionHeader         = "Gram-Session"
	SessionCookie         = "gram_session"

	ChatSessionsTokenSecurityScheme = "chat_sessions_token"
	ChatSessionsTokenHeader         = "Gram-Chat-Session" //nolint:gosec // this is a valid header name

	ProjectSlugSecuritySchema = "project_slug"
	ProjectHeader             = "Gram-Project"
)
