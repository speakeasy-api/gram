package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// BuildAgentPluginsView regroups the flat (plugin, server) rows returned by
// agent.GetAssignedPluginsWithServers into one AgentPlugin per plugin id and
// computes a stable ETag over the resulting set.
//
// serverURL is the base URL (e.g. https://app.getgram.ai) used to construct
// each server's mcp_url from its toolset mcp_slug.
func BuildAgentPluginsView(serverURL string, rows []repo.GetAssignedPluginsWithServersRow) *gen.GetPluginsResult {
	byPlugin := make(map[string]*gen.AgentPlugin)
	order := make([]string, 0)

	for _, row := range rows {
		// Skip rows whose toolset has no MCP slug — the join already filters
		// to mcp_enabled, but the column itself is nullable so a defensive
		// guard keeps a missing slug from producing a malformed URL.
		if !row.ToolsetMcpSlug.Valid || row.ToolsetMcpSlug.String == "" {
			continue
		}

		id := row.PluginID.String()
		plugin, ok := byPlugin[id]
		if !ok {
			plugin = &gen.AgentPlugin{
				ID:          id,
				Slug:        row.PluginSlug,
				Name:        row.PluginName,
				Description: conv.FromPGText[string](row.PluginDescription),
				Servers:     nil,
			}
			byPlugin[id] = plugin
			order = append(order, id)
		}

		plugin.Servers = append(plugin.Servers, &gen.AgentPluginServer{
			DisplayName: row.ServerDisplayName,
			Policy:      row.ServerPolicy,
			McpURL:      fmt.Sprintf("%s/mcp/%s", strings.TrimRight(serverURL, "/"), row.ToolsetMcpSlug.String),
			IsPublic:    row.ToolsetMcpIsPublic,
		})
	}

	plugins := make([]*gen.AgentPlugin, 0, len(order))
	for _, id := range order {
		plugins = append(plugins, byPlugin[id])
	}

	return &gen.GetPluginsResult{
		Etag:    computeAgentPluginsETag(rows),
		Plugins: plugins,
	}
}

// computeAgentPluginsETag hashes the dimensions that determine the rendered
// plugin set so a stable revision string can be returned to the agent. Rows
// come back from the query already sorted (plugin.slug, server.sort_order),
// so the hash is order-stable as long as the inputs are.
func computeAgentPluginsETag(rows []repo.GetAssignedPluginsWithServersRow) string {
	if len(rows) == 0 {
		return "empty"
	}
	h := sha256.New()
	for _, row := range rows {
		// sha256.Hash never returns an error from Write; the discard is for errcheck.
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
