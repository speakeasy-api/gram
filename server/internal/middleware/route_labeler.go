package middleware

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// RouteLabelerMiddleware enriches HTTP metric attributes recorded by otelhttp.
//
// This middleware must be registered after otelhttp so that the Labeler is
// present in the request context.
func RouteLabelerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			// otelhttp (v0.67.0) reads r.Pattern for trace span attributes
			// but does not populate it for metric attributes. Add http.route
			// via the Labeler so metrics like http.server.request.duration
			// can be segmented by route. Remove this if a future otelhttp
			// version populates MetricAttributes.Route from r.Pattern.
			if idx := strings.IndexByte(r.Pattern, '/'); idx >= 0 {
				labeler.Add(attr.HTTPRoute(r.Pattern[idx:]))
			}
		}
		next.ServeHTTP(w, r)
	})
}
