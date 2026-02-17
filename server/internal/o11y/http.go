package o11y

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	goahttp "goa.design/goa/v3/http"
)

// AttachHandler attaches an HTTP handler to a goa mux and ensures an
// OpenTelemetry route tag is applied so that this route appears in HTTP
// metrics.
func AttachHandler(
	mux goahttp.Muxer,
	method string,
	route string,
	handler func(w http.ResponseWriter, r *http.Request),
) {
	mux.Handle(method, route, otelhttp.WithRouteTag(route, http.HandlerFunc(handler)).ServeHTTP)
}
