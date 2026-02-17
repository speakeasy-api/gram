package o11y

import (
	"net/http"

	goahttp "goa.design/goa/v3/http"
)

// This function was previously used to attach handlers with OpenTelemetry
// instrumentation, but is no longer necessary as goahttp.Muxer
// handles this now. It will be removed in a future release.
func AttachHandler(
	mux goahttp.Muxer,
	method string,
	route string,
	handler func(w http.ResponseWriter, r *http.Request),
) {
	mux.Handle(method, route, handler)
}
