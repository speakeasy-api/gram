package middleware

import (
	"net/http"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

// SpeakeasyAIHeaderAliases maps each documented Speakeasy-AI-* request
// header to the Gram-* header the handlers and the Goa security schemes read.
// The Speakeasy-AI-* names are canonical; the Gram-* names stay accepted as
// deprecated aliases.
var SpeakeasyAIHeaderAliases = map[string]string{
	"Speakeasy-AI-Key":                constants.APIKeyHeader,
	"Speakeasy-AI-Project":            constants.ProjectHeader,
	"Speakeasy-AI-Session":            constants.SessionHeader,
	"Speakeasy-AI-Chat-Session":       constants.ChatSessionsTokenHeader,
	"Speakeasy-AI-Environment":        "Gram-Environment",
	"Speakeasy-AI-Mode":               "Gram-Mode",
	"Speakeasy-AI-User-Email":         "Gram-User-Email",
	"Speakeasy-AI-Device-Serial":      deviceidentity.HeaderSerial,
	"Speakeasy-AI-Device-Hostname":    deviceidentity.HeaderHostname,
	"Speakeasy-AI-Device-Environment": deviceidentity.HeaderEnvironment,
}

// meterHeaderAlias counts requests that send an aliased header, by canonical
// header name and by which form arrived. Once the "gram" form stops showing
// up, the Gram-* names and this middleware can be removed.
const meterHeaderAlias = "gram.http.header_alias.requests"

// SpeakeasyAIHeaders copies each Speakeasy-AI-* request header onto the
// Gram-* header it replaces, so everything downstream reads one name. When a
// request sends both, a non-empty Speakeasy-AI-* value wins.
func SpeakeasyAIHeaders(meterProvider metric.MeterProvider) func(next http.Handler) http.Handler {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/middleware")
	// A failed instrument creation only loses the metric; the aliasing still
	// runs, so the error is dropped rather than failing startup.
	counter, _ := meter.Int64Counter(
		meterHeaderAlias,
		metric.WithDescription("Requests that send an Speakeasy-AI-* header or the deprecated Gram-* header it replaces."),
		metric.WithUnit("{request}"),
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for name, legacy := range SpeakeasyAIHeaderAliases {
				values := slices.DeleteFunc(slices.Clone(r.Header.Values(name)), func(v string) bool {
					return strings.TrimSpace(v) == ""
				})
				r.Header.Del(name)
				form := ""
				switch {
				case len(values) > 0:
					r.Header[http.CanonicalHeaderKey(legacy)] = values
					form = "speakeasy_ai"
				case r.Header.Get(legacy) != "":
					form = "gram"
				}
				if form != "" && counter != nil {
					counter.Add(r.Context(), 1, metric.WithAttributes(
						attr.HTTPHeaderAliasName(name),
						attr.HTTPHeaderAliasForm(form),
					))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
