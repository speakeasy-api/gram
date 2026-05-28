package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// BuildAgentPluginsView regroups the flat (plugin, server) rows returned by
// agent.GetAssignedPluginsWithServers into one AgentPlugin per plugin id and
// computes a stable ETag over the resulting set.
//
// Rows are assumed to be already grouped by plugin (contiguous), which the
// query's `ORDER BY p.slug, ps.sort_order` guarantees.
//
// mcpURL constructs the public MCP URL from a toolset's mcp_slug. The caller
// owns the URL shape so this builder stays free of server-side config.
func BuildAgentPluginsView(rows []repo.GetAssignedPluginsWithServersRow, mcpURL func(slug string) string) *gen.GetPluginsResult {
	var plugins []*gen.AgentPlugin

	for _, row := range rows {
		// Defensive: the join already filters to mcp_enabled, but the column
		// itself is nullable so a missing slug must not produce a malformed URL.
		if !row.ToolsetMcpSlug.Valid || row.ToolsetMcpSlug.String == "" {
			continue
		}

		pluginID := row.PluginID.String()
		if len(plugins) == 0 || plugins[len(plugins)-1].ID != pluginID {
			plugins = append(plugins, &gen.AgentPlugin{
				ID:          pluginID,
				Slug:        row.PluginSlug,
				Name:        row.PluginName,
				Description: conv.FromPGText[string](row.PluginDescription),
				Servers:     nil,
			})
		}

		last := plugins[len(plugins)-1]
		last.Servers = append(last.Servers, &gen.AgentPluginServer{
			DisplayName: row.ServerDisplayName,
			Policy:      row.ServerPolicy,
			McpURL:      mcpURL(row.ToolsetMcpSlug.String),
			IsPublic:    row.ToolsetMcpIsPublic,
		})
	}

	return &gen.GetPluginsResult{
		Etag:    computeAgentPluginsETag(rows),
		Plugins: plugins,
	}
}

// computeAgentPluginsETag hashes the dimensions that determine the rendered
// plugin set so a stable revision string can be returned to the agent.
// Excluded inputs (e.g. the server URL) are deployment-config, not policy —
// changing them should not bust the agent's cache.
func computeAgentPluginsETag(rows []repo.GetAssignedPluginsWithServersRow) string {
	h := sha256.New()
	for _, row := range rows {
		// sha256.Hash never errors from Write; assign to _ to satisfy errcheck.
		_, _ = fmt.Fprintf(
			h,
			"%s\x00%d\x00%s\x00%d\x00%s\x00%t\n",
			row.PluginID.String(),
			row.PluginUpdatedAt.Time.UnixNano(),
			row.ServerID.String(),
			row.ServerUpdatedAt.Time.UnixNano(),
			row.ToolsetMcpSlug.String,
			row.ToolsetMcpIsPublic,
		)
	}
	return hex.EncodeToString(h.Sum(nil))
}
