package mcp

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Tag-based filtering of the tool set lives in the shared toolfilter package so
// the runtime ?tags= path and the management listToolFilters endpoints derive
// effective tags identically. See toolfilter.FilterToolsByTags and
// toolfilter.EffectiveToolTags.

// recordToolFilterSpan annotates the active span with how many tools survived
// the ?tags= filter and how many were dropped, to aid debugging of missing
// tools.
func recordToolFilterSpan(ctx context.Context, returned, filtered int) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attr.MCPToolsReturned(returned), attr.MCPToolsFiltered(filtered))
}
