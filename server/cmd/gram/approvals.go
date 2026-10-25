package gram

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
)

// attachApprovals mounts the request and watch endpoints, which take only the
// request token from the block message, and the Approvals MCP server, which
// also serves each project's approvals server through the gateway. At MCPPath
// callers authenticate with a key. Through the gateway they can also sign in
// with the approvals server's issuer.
func attachApprovals(mux goahttp.Muxer, logger *slog.Logger, service *approval.Service, mcpService *mcp.Service, store ratelimit.Store) {
	runtime := approval.NewRuntime(service, func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		return mcpService.RequirePrivateIdentityAuth(r.Context(), w, r, false, uuid.Nil, "")
	}, store)
	o11y.AttachHandler(mux, http.MethodPost, approval.MCPPath, runtime.Handler().ServeHTTP)
	// The same server answers at each project's approvals server endpoint.
	mcpService.SetBuiltinBackend(approval.BuiltinBackend, mcp.BuiltinBackend{Handler: runtime.GatewayHandler(), Tools: runtime.Tools()})
	o11y.AttachHandler(mux, http.MethodPost, approval.RequestPath, oops.ErrHandle(logger, runtime.HandleRequest).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, approval.WatchPath, oops.ErrHandle(logger, runtime.HandleWatch).ServeHTTP)
}
