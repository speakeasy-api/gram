package mcp

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// filterToolsByTags returns the subset of tools whose effective tag set carries
// at least one of the requested tags (OR/union). See effectiveToolTags for how a
// tool's effective tags are derived from its variation and source tags. Callers
// should only invoke this when tags is non-empty; an empty tags slice yields an
// empty result.
//
// Matching is exact and case-sensitive.
func filterToolsByTags(tools []*types.Tool, tags []string) []*types.Tool {
	want := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		want[tag] = struct{}{}
	}

	filtered := make([]*types.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}

		for _, tag := range effectiveToolTags(tool) {
			if _, ok := want[tag]; ok {
				filtered = append(filtered, tool)
				break
			}
		}
	}

	return filtered
}

// effectiveToolTags returns the tags used for ?tags= filtering. A variation that
// defines tags replaces the source tags — including an explicit empty set
// (non-nil, length 0), which removes the tool from every tag filter. A nil
// variation tag set means "no tag modification", so the source tool's own tags
// remain authoritative. This lets source-defined tags drive filtering without
// forcing a variation row to exist for every tool.
//
// Tools that carry neither variation tags nor source tags — proxy/external MCP
// tools, prompts, platform tools — yield an empty set and are excluded whenever a
// filter is active.
func effectiveToolTags(tool *types.Tool) []string {
	if base, err := conv.ToBaseTool(tool); err == nil && base.Variation != nil && base.Variation.Tags != nil {
		return base.Variation.Tags
	}

	switch {
	case tool.HTTPToolDefinition != nil:
		return tool.HTTPToolDefinition.Tags
	case tool.FunctionToolDefinition != nil:
		return tool.FunctionToolDefinition.Tags
	default:
		return nil
	}
}

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
