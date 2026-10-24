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

// attachApprovals mounts the Approvals MCP server and the watch endpoint.
// Callers authenticate with the same agent keys and user sessions as gated
// MCP servers.
func attachApprovals(mux goahttp.Muxer, logger *slog.Logger, service *approval.Service, mcpService *mcp.Service, store ratelimit.Store) {
	runtime := approval.NewRuntime(service, func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		return mcpService.RequirePrivateIdentityAuth(r.Context(), w, r, false, uuid.Nil, "")
	}, store)
	o11y.AttachHandler(mux, http.MethodPost, approval.MCPPath, runtime.Handler().ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, approval.WatchPath, oops.ErrHandle(logger, runtime.HandleWatch).ServeHTTP)
}
