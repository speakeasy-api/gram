package deviceintegrations

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// syncErrorMaxLen truncates stored provider errors: last_poll_error and the
// testConnection message are dashboard surfaces, not log sinks.
const syncErrorMaxLen = 500

// boundedProviderClient is the SSRF-hardened guardian client with a
// redirect cap layered on: a hostile vendor must not chase arbitrary
// redirect chains (mirrors remotemcp's probe bounds; response-size limits
// are each provider implementation's job).
func boundedProviderClient(policy *guardian.Policy) *guardian.HTTPClient {
	client := policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("stopped after 3 redirects")
		}
		return nil
	}
	return client
}

// sanitizeSyncError scrubs credential values out of a provider error and
// truncates it rune-safely: vendor transport errors can echo request URLs,
// including URL-escaped forms of a secret. The scrub threshold is guaranteed
// to cover every stored secret because upsert validation enforces
// minSecretLength.
func sanitizeSyncError(message string, creds providers.Credentials) string {
	for _, value := range creds {
		if len(value) < 4 {
			continue
		}
		for _, form := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
			message = strings.ReplaceAll(message, form, "[redacted]")
		}
	}
	if len(message) > syncErrorMaxLen {
		message = strings.ToValidUTF8(message[:syncErrorMaxLen], "")
	}
	return message
}
