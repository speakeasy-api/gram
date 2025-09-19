package api

// CredentialGetter provides common methods for getting API credentials
type CredentialGetter interface {
	// GetApiKey returns the API key to use for authentication.
	GetApiKey() string
	// GetProjectSlug returns the project slug to use for the request.
	GetProjectSlug() string
}
