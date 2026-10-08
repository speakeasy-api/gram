package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

// SpeakeasyAIHeaderAliases maps each documented X-Speakeasy-AI-* request
// header to the Gram-* header the handlers and the Goa security schemes read.
// The X-Speakeasy-AI-* names are canonical; the Gram-* names stay accepted as
// deprecated aliases.
var SpeakeasyAIHeaderAliases = map[string]string{
	"X-Speakeasy-AI-Key":                constants.APIKeyHeader,
	"X-Speakeasy-AI-Project":            constants.ProjectHeader,
	"X-Speakeasy-AI-Session":            constants.SessionHeader,
	"X-Speakeasy-AI-Chat-Session":       constants.ChatSessionsTokenHeader,
	"X-Speakeasy-AI-Environment":        "Gram-Environment",
	"X-Speakeasy-AI-Mode":               "Gram-Mode",
	"X-Speakeasy-AI-User-Email":         "Gram-User-Email",
	"X-Speakeasy-AI-Device-Serial":      deviceidentity.HeaderSerial,
	"X-Speakeasy-AI-Device-Hostname":    deviceidentity.HeaderHostname,
	"X-Speakeasy-AI-Device-Environment": deviceidentity.HeaderEnvironment,
}

// SpeakeasyAIHeaders copies each X-Speakeasy-AI-* request header onto the
// Gram-* header it replaces, so everything downstream reads one name. When a
// request sends both, a non-empty X-Speakeasy-AI-* value wins.
func SpeakeasyAIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, legacy := range SpeakeasyAIHeaderAliases {
			values := slices.DeleteFunc(slices.Clone(r.Header.Values(name)), func(v string) bool {
				return strings.TrimSpace(v) == ""
			})
			r.Header.Del(name)
			if len(values) > 0 {
				r.Header[http.CanonicalHeaderKey(legacy)] = values
			}
		}
		next.ServeHTTP(w, r)
	})
}
