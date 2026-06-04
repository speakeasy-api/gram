// Package toolfilter is the shared home for MCP tool-filtering logic: deriving a
// tool's effective filter tags, applying the runtime ?tags= filter, resolving
// the effective variation group, and building the read-only scopes view shown on
// the dashboard.
//
// Both the runtime /mcp handler and the management listToolFilters endpoints
// depend on EffectiveToolTags so the dashboard view cannot drift from what
// ?tags= actually returns.
package toolfilter

import (
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ResolveGroupID resolves the effective tool variations group using the same
// chain as the runtime: the mcp_servers value takes precedence, then the
// toolset's own column. A nil result means no explicit group is configured
// (the project default applies and filtering is considered disabled).
func ResolveGroupID(mcpServerGroupID, toolsetGroupID *uuid.UUID) *uuid.UUID {
	if mcpServerGroupID != nil {
		return mcpServerGroupID
	}
	return toolsetGroupID
}

// EffectiveToolTags returns the tags used for ?tags= filtering. A variation that
// defines tags replaces the source tags — including an explicit empty set
// (non-nil, length 0), which removes the tool from every tag filter. A nil
// variation tag set means "no tag modification", so the source tool's own tags
// remain authoritative. This lets source-defined tags drive filtering without
// forcing a variation row to exist for every tool.
//
// Tools that carry neither variation tags nor source tags — proxy/external MCP
// tools, prompts, platform tools — yield an empty set and are excluded whenever
// a filter is active. Variations must already be applied to the tool (e.g. via
// mv.DescribeToolset / mv.ApplyVariations) for the variation branch to apply.
func EffectiveToolTags(tool *types.Tool) []string {
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

// FilterToolsByTags returns the subset of tools whose effective tag set carries
// at least one of the requested tags (OR/union). Callers should only invoke this
// when tags is non-empty; an empty tags slice yields an empty result.
//
// Matching is exact and case-sensitive.
func FilterToolsByTags(tools []*types.Tool, tags []string) []*types.Tool {
	want := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		want[tag] = struct{}{}
	}

	filtered := make([]*types.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}

		for _, tag := range EffectiveToolTags(tool) {
			if _, ok := want[tag]; ok {
				filtered = append(filtered, tool)
				break
			}
		}
	}

	return filtered
}

// BuildView builds the read-only tool-filter view from tools that already have
// the resolved variation group's overrides applied.
//
// groupID is the explicit group resolved from the mcp_servers/toolsets chain
// (see ResolveGroupID); callers pass nil for the project-default group, which is
// reported as filtering disabled. When groupID is nil it returns an empty,
// filtering_enabled=false result. Otherwise it derives each tool's effective
// tags — identically to the runtime ?tags= filter for that group — and groups
// them into scopes plus the set of tools excluded from every filter.
func BuildView(tools []*types.Tool, groupID *uuid.UUID, groupName *string) *types.ListToolFiltersResult {
	if groupID == nil {
		return &types.ListToolFiltersResult{
			FilteringEnabled:        false,
			ToolVariationsGroupID:   nil,
			ToolVariationsGroupName: nil,
			Scopes:                  []*types.ToolFilterScope{},
			Excluded:                []*types.ToolFilterTool{},
		}
	}

	scopes, excluded := groupByEffectiveTags(tools)
	id := groupID.String()

	return &types.ListToolFiltersResult{
		FilteringEnabled:        true,
		ToolVariationsGroupID:   &id,
		ToolVariationsGroupName: groupName,
		Scopes:                  scopes,
		Excluded:                excluded,
	}
}

// groupByEffectiveTags partitions tools into per-tag scopes (a tool with N
// effective tags appears under each, mirroring the OR/union ?tags= semantics)
// and an excluded set for tools whose effective tag set is empty. Scopes are
// sorted by tag and tools by display name for stable output.
func groupByEffectiveTags(tools []*types.Tool) ([]*types.ToolFilterScope, []*types.ToolFilterTool) {
	scopeTools := make(map[string][]*types.ToolFilterTool)
	excluded := make([]*types.ToolFilterTool, 0)

	for _, tool := range tools {
		if tool == nil {
			continue
		}
		ft := toFilterTool(tool)
		if ft == nil {
			continue
		}

		tags := EffectiveToolTags(tool)
		if len(tags) == 0 {
			excluded = append(excluded, ft)
			continue
		}

		seen := make(map[string]struct{}, len(tags))
		for _, tag := range tags {
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			scopeTools[tag] = append(scopeTools[tag], ft)
		}
	}

	scopes := make([]*types.ToolFilterScope, 0, len(scopeTools))
	for _, tag := range slices.Sorted(maps.Keys(scopeTools)) {
		members := scopeTools[tag]
		sortFilterTools(members)
		scopes = append(scopes, &types.ToolFilterScope{
			Tag:       tag,
			ToolCount: len(members),
			Tools:     members,
		})
	}

	sortFilterTools(excluded)

	return scopes, excluded
}

func toFilterTool(tool *types.Tool) *types.ToolFilterTool {
	toolURN, err := conv.GetToolURN(*tool)
	if err != nil || toolURN == nil {
		return nil
	}

	name := ""
	if base, err := conv.ToBaseTool(tool); err == nil {
		name = base.Name
	}

	return &types.ToolFilterTool{
		ToolUrn: toolURN.String(),
		Name:    name,
	}
}

func sortFilterTools(tools []*types.ToolFilterTool) {
	slices.SortFunc(tools, func(a, b *types.ToolFilterTool) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return strings.Compare(a.ToolUrn, b.ToolUrn)
	})
}
