package plugins

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// DeviceMCPServer is a Speakeasy-hosted MCP server inside a plugin, at the
// address the plugin's published package uses. The device agent writes these
// into an agent identity's tool configurations with its own mcp:connect
// credential.
type DeviceMCPServer struct {
	// PluginSlug is the slug of the plugin the server belongs to.
	PluginSlug string

	// DisplayName is the server's display name within its plugin.
	DisplayName string

	// Key is DisplayName reduced to the characters every managed tool accepts
	// as an MCP server name.
	Key string

	// URL is the server's streamable-HTTP address.
	URL string

	// SortOrder is the server's position within its plugin.
	SortOrder int32
}

// ListDeviceMCPServers returns the Speakeasy-hosted MCP servers in the given
// plugins of one project, ordered by plugin slug, then position, then display
// name. It returns nothing when pluginIDs is empty.
//
// Unproxied servers are left out: their address is the vendor's own server,
// which must never receive a Speakeasy credential. Gateway members are left
// out too: publishing re-checks their distribution admission under the
// project's admission lock, which a device poll must not take.
//
// A server whose address cannot be resolved is logged and skipped, so one
// misconfigured server does not keep the rest from reaching devices.
func ListDeviceMCPServers(ctx context.Context, logger *slog.Logger, db repo.DBTX, serverURL string, projectID uuid.UUID, pluginIDs []uuid.UUID) ([]DeviceMCPServer, error) {
	if len(pluginIDs) == 0 {
		return nil, nil
	}

	queries := repo.New(db)
	toolsetRows, err := queries.ListPluginsWithServersForProject(ctx, repo.ListPluginsWithServersForProjectParams{ProjectID: projectID, PluginIds: pluginIDs})
	if err != nil {
		return nil, fmt.Errorf("list toolset plugin servers: %w", err)
	}
	remoteRows, err := queries.ListPluginsWithMcpServersForProject(ctx, repo.ListPluginsWithMcpServersForProjectParams{ProjectID: projectID, PluginIds: pluginIDs})
	if err != nil {
		return nil, fmt.Errorf("list remote plugin servers: %w", err)
	}

	skip := func(pluginSlug string, err error) {
		logger.WarnContext(ctx, "plugin MCP server has no usable address; leaving it out of the device poll",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
			attr.SlogPluginSlug(pluginSlug),
		)
	}

	servers := make([]DeviceMCPServer, 0, len(toolsetRows)+len(remoteRows))
	for _, r := range toolsetRows {
		mcpURL, ok, err := toolsetServerURL(serverURL, r)
		switch {
		case err != nil:
			skip(r.PluginSlug, err)
		case ok:
			servers = append(servers, DeviceMCPServer{
				PluginSlug:  r.PluginSlug,
				DisplayName: r.ServerDisplayName,
				Key:         codexMCPServerName(r.ServerDisplayName),
				URL:         mcpURL,
				SortOrder:   r.ServerSortOrder,
			})
		}
	}
	for _, r := range remoteRows {
		mcpURL, unproxied, err := remoteServerURL(serverURL, r)
		switch {
		case err != nil:
			skip(r.PluginSlug, err)
		case !unproxied:
			servers = append(servers, DeviceMCPServer{
				PluginSlug:  r.PluginSlug,
				DisplayName: r.ServerDisplayName,
				Key:         codexMCPServerName(r.ServerDisplayName),
				URL:         mcpURL,
				SortOrder:   r.ServerSortOrder,
			})
		}
	}

	slices.SortStableFunc(servers, func(a, b DeviceMCPServer) int {
		return cmp.Or(
			cmp.Compare(a.PluginSlug, b.PluginSlug),
			cmp.Compare(a.SortOrder, b.SortOrder),
			cmp.Compare(a.DisplayName, b.DisplayName),
		)
	})
	return servers, nil
}
