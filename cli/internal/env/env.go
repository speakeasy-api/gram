package env

import (
	"fmt"
	"os"
	"strings"
)

const (
	// VarNameProducerKey is the environment variable name which points to the
	// user's API key.
	VarNameProducerKey = "GRAM_API_KEY"

	// VarNameProjectSlug is the environment variable name which points to the
	// user's intended project.
	VarNameProjectSlug = "GRAM_PROJECT_SLUG"

	// VarNameAPIScheme is the environment variable name which points to the API
	// URL scheme, e.g. "https"
	VarNameAPIScheme = "GRAM_SCHEME"

	// VarNameAPIHost is the environment variable name which points to the API
	// hostname, e.g. "example.com"
	VarNameAPIHost = "GRAM_HOST"
)

func APIKey() string {
	return validateApiKey(Must(VarNameProducerKey))
}

func APIKeyMissing() bool {
	return Missing(VarNameProducerKey)
}

func ProjectSlug() string {
	return Must(VarNameProjectSlug)
}

const defaultHostGramAPI = "app.getgram.ai"

func APIHost() string {
	return Fallback(VarNameAPIHost, defaultHostGramAPI)
}

func APIScheme() string {
	return Fallback(VarNameAPIScheme, "https")
}

const apiKeyPrefix = "gram"

func validateApiKey(key string) string {
	ok := strings.HasPrefix(key, apiKeyPrefix)

	if ok {
		return key
	} else {
		panic(fmt.Errorf("key is malformed: expected prefix '%s'", apiKeyPrefix))
	}
}

func Must(key string) string {
	val := os.Getenv(key)
	if len(val) == 0 {
		panic(fmt.Errorf("missing env: %s", key))
	}

	return val
}

func Fallback(key string, fallback string) string {
	val := os.Getenv(key)
	if len(val) == 0 {
		return fallback
	} else {
		return val
	}
}

func Missing(key string) bool {
	return len(os.Getenv(key)) == 0
}
