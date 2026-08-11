package plugins

import (
	"context"
	"fmt"
	"io"

	genplugins "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListPlugins struct {
	plugins PluginsService
}

type listPluginsInput struct{}

// pluginSummary is a deliberately narrow projection of gen.Plugin. The
// management result also carries every plugin server and role assignment,
// which is a lot of context for what callers actually need here: the plugin ID
// to distribute a skill to, and enough naming to pick the right one.
type pluginSummary struct {
	ID          string
	Name        string
	Slug        string
	Description *string
	IsDefault   bool
	ServerCount int64
	SkillCount  int64
}

type listPluginsResult struct {
	Plugins []pluginSummary
}

func NewListPluginsTool(svc PluginsService) *ListPlugins {
	return &ListPlugins{plugins: svc}
}

func (t *ListPlugins) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "plugins",
		HandlerName: "list_plugins",
		Name:        "platform_list_plugins",
		Description: "List the plugins in the current project, including each plugin's ID, name, slug, and how many servers and skills it carries. Use this to resolve a plugin by name before distributing a skill to it with platform_distribute_skill.",
		InputSchema: core.BuildInputSchema[listPluginsInput](),
		Variables:   nil,
		Annotations: pluginCatalogAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

// pluginCatalogAnnotations stops short of read-only on purpose. Listing
// plugins lazily heals a project that predates the Default plugin: for a
// caller holding org admin, the management service provisions it and writes a
// plugin-create audit event. That heal is additive and converges — every
// subsequent call is a pure read — so the tool is idempotent and
// non-destructive, but claiming readOnlyHint would misreport a write that a
// client could reasonably want to know about.
func pluginCatalogAnnotations() *types.ToolAnnotations {
	readOnly := false
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func (t *ListPlugins) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.plugins == nil {
		return fmt.Errorf("plugins service not configured")
	}

	input := listPluginsInput{}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	result, err := t.plugins.ListPlugins(ctx, &genplugins.ListPluginsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list plugins: %w", err)
	}

	summaries := make([]pluginSummary, 0, len(result.Plugins))
	for _, plugin := range result.Plugins {
		if plugin == nil {
			continue
		}
		summaries = append(summaries, pluginSummary{
			ID:          plugin.ID,
			Name:        plugin.Name,
			Slug:        plugin.Slug,
			Description: plugin.Description,
			IsDefault:   conv.PtrValOr(plugin.IsDefault, false),
			ServerCount: conv.PtrValOr(plugin.ServerCount, 0),
			SkillCount:  conv.PtrValOr(plugin.SkillCount, 0),
		})
	}

	return core.EncodeResult(wr, listPluginsResult{Plugins: summaries})
}
