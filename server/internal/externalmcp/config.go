package externalmcp

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// BuildHeaders constructs HTTP headers from system environment variables and user configuration.
//
// Logic:
// 1. ALL system env values become headers using the appropriate header names.
// 2. For keys with header definitions, use the definition's HeaderName.
// 3. For keys without definitions, derive the header name using ToHTTPHeader.
// 4. User config can override values (only for keys with header definitions).
// 5. Empty values are skipped.
// 6. If oauthToken is provided, sets Authorization: Bearer <token>.
func BuildHeaders(
	systemEnv *toolconfig.CaseInsensitiveEnv,
	userConfig *toolconfig.CaseInsensitiveEnv,
	headerDefs []HeaderDefinition,
	oauthToken string,
) map[string]string {
	// Build lookup map: lowercased env key -> header definition
	defsByLowercaseKey := make(map[string]HeaderDefinition, len(headerDefs))
	for _, def := range headerDefs {
		lowercaseKey := strings.ToLower(def.Name)
		defsByLowercaseKey[lowercaseKey] = def
	}

	// Build headers from all system env values
	headers := make(map[string]string)

	// Get all system env variables
	allSystemEnv := systemEnv.All()
	for envKey, envValue := range allSystemEnv {
		// Skip empty values
		if envValue == "" {
			continue
		}

		// Determine the header name
		var headerName string
		if def, exists := defsByLowercaseKey[envKey]; exists {
			// Use the header name from the definition
			headerName = def.HeaderName
		} else {
			// Derive the header name from the environment variable key
			headerName = toolconfig.ToHTTPHeader(envKey)
		}

		headers[headerName] = envValue
	}

	// User config can override values (only for keys with header definitions)
	allUserConfig := userConfig.All()
	for envKey, userValue := range allUserConfig {
		// Skip empty values
		if userValue == "" {
			continue
		}

		// Only allow overrides for keys with header definitions
		if def, exists := defsByLowercaseKey[envKey]; exists {
			headers[def.HeaderName] = userValue
		}
	}

	// Set Authorization header from OAuth token if provided
	if oauthToken != "" {
		headers["Authorization"] = "Bearer " + oauthToken
	}

	return headers
}
