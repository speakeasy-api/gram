package gram

import (
	"log/slog"
	"net/http"

	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
)

// attachApprovals mounts the request and watch endpoints. Both take only the
// request token from the block message, as the bearer.
func attachApprovals(mux goahttp.Muxer, logger *slog.Logger, service *approval.Service, store ratelimit.Store) {
	runtime := approval.NewRuntime(service, store)
	o11y.AttachHandler(mux, http.MethodPost, approval.RequestPath, oops.ErrHandle(logger, runtime.HandleRequest).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, approval.WatchPath, oops.ErrHandle(logger, runtime.HandleWatch).ServeHTTP)
}
