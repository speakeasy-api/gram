package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// metaCodeTelemetryHost records each Code Mode discovery helper call as one
// meta_discovery row, as Progressive records its drill-down tools. tools.call
// passes through: the member dispatch it reaches writes the call's tool_call
// row.
type metaCodeTelemetryHost struct {
	codemode.Host

	service *Service
	gate    *metaGateContext
}

var _ codemode.Host = (*metaCodeTelemetryHost)(nil)

func (h *metaCodeTelemetryHost) Servers(ctx context.Context, args codemode.PageArgs) (*codemode.ServerPage, error) {
	start := time.Now()
	page, err := h.Host.Servers(ctx, args)
	h.service.logMetaDiscovery(ctx, h.gate, metamcp.ToolCodeServers, start, codeHelperTelemetryError(err))
	return page, err //nolint:wrapcheck // the program sees the host's own error
}

func (h *metaCodeTelemetryHost) Search(ctx context.Context, args codemode.SearchArgs) (*codemode.SearchPage, error) {
	start := time.Now()
	page, err := h.Host.Search(ctx, args)
	h.service.logMetaDiscovery(ctx, h.gate, metamcp.ToolCodeSearch, start, codeHelperTelemetryError(err))
	return page, err //nolint:wrapcheck // the program sees the host's own error
}

func (h *metaCodeTelemetryHost) Describe(ctx context.Context, path string) (*codemode.Description, error) {
	start := time.Now()
	description, err := h.Host.Describe(ctx, path)
	h.service.logMetaDiscovery(ctx, h.gate, metamcp.ToolCodeDescribe, start, codeHelperTelemetryError(err))
	return description, err //nolint:wrapcheck // the program sees the host's own error
}

// codeHelperTelemetryError gives a failed helper the status its Progressive
// counterpart records: a hidden or unknown tool is not found, and a member
// that needs consent or a revoked execution is forbidden.
func codeHelperTelemetryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, codemode.ErrToolUnavailable):
		return oops.E(oops.CodeNotFound, err, "tool unavailable")
	case errors.Is(err, codemode.ErrConsentRequired), errors.Is(err, codemode.ErrExecutionRevoked):
		return oops.E(oops.CodeForbidden, err, "tool not permitted")
	default:
		return err
	}
}
