package auth

const (
	KeySecurityScheme = "apikey"
	APIKeyHeader      = "Gram-Key"

	FunctionTokenSecurityScheme = "function_token"
	FunctionTokenHeader         = "Authorization"

	SessionSecurityScheme = "session"
	SessionHeader         = "Gram-Session"
	SessionCookie         = "gram_session"

	ProjectSlugSecuritySchema = "project_slug"
	ProjectHeader             = "Gram-Project"
)
