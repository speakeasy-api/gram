package plugins

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// DeviceMCPServer is a Speakeasy-hosted MCP server in a plugin, at the address
// the plugin's published package uses.
type DeviceMCPServer struct {
	// PluginSlug is the slug of the plugin the server belongs to.
	PluginSlug string

	// DisplayName is the server's display name within its plugin.
	DisplayName string

	// Name is the server's key in each tool's MCP configuration. The prefix keeps
	// it apart from servers people add by hand.
	Name string

	// URL is the server's streamable-HTTP address.
	URL string

	// SortOrder is the server's position within its plugin.
	SortOrder int32
}

// ListDeviceMCPServers returns the Speakeasy-hosted servers in the given plugins.
// Unproxied servers (a vendor's own address) and gateway members are left out,
// and an unresolvable or non-https address is skipped rather than failing the list.
func ListDeviceMCPServers(ctx context.Context, logger *slog.Logger, db repo.DBTX, serverURL string, projectID uuid.UUID, pluginIDs []uuid.UUID) ([]DeviceMCPServer, error) {
	if len(pluginIDs) == 0 {
		return nil, nil
	}

	remoteRows, err := repo.New(db).ListPluginsWithMcpServersForProject(ctx, repo.ListPluginsWithMcpServersForProjectParams{ProjectID: projectID, PluginIds: pluginIDs})
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

	servers := make([]DeviceMCPServer, 0, len(remoteRows))
	for _, r := range remoteRows {
		mcpURL, unproxied, err := remoteServerURL(serverURL, r)
		if err == nil && !unproxied {
			err = requireHTTPS(mcpURL)
		}
		switch {
		case err != nil:
			skip(r.PluginSlug, err)
		case !unproxied:
			servers = append(servers, DeviceMCPServer{
				PluginSlug:  r.PluginSlug,
				DisplayName: r.ServerDisplayName,
				Name:        deviceMCPServerName(r.McpServerSlug.String, r.ServerDisplayName),
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

// requireHTTPS refuses an address the device would send its credential to in
// cleartext.
func requireHTTPS(rawURL string) error {
	u, err := url.Parse(rawURL)
	switch {
	case err != nil:
		// A *url.Error quotes the raw URL, credentials included; keep only its cause.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}
		return fmt.Errorf("parse MCP server URL: %w", err)
	case u.Scheme != "https" || u.Host == "":
		return fmt.Errorf("MCP server URL is not an https URL: %q", u.Scheme+"://"+u.Host)
	case u.User != nil:
		// Report only the origin: Redacted masks a password but not a username.
		return fmt.Errorf("MCP server URL carries credentials: %q", u.Scheme+"://"+u.Host)
	default:
		return nil
	}
}

// deviceMCPServerNamePrefix marks the MCP server entries Speakeasy writes into
// a tool's configuration.
const deviceMCPServerNamePrefix = "speakeasy-"

// deviceMCPServerName returns the prefixed, slugified name (from slug, else
// displayName). The result is a valid key in Claude Code, Codex and Cursor configs.
func deviceMCPServerName(slug, displayName string) string {
	name := slugifyMCPServerName(slug)
	if name == "" {
		name = slugifyMCPServerName(displayName)
	}
	if name == "" {
		name = "mcp-server"
	}
	// A slug that already carries the prefix keeps a single copy of it.
	if rest, ok := strings.CutPrefix(name, deviceMCPServerNamePrefix); ok && rest != "" {
		name = rest
	}
	return deviceMCPServerNamePrefix + name
}

func slugifyMCPServerName(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	pendingDash := false
	for _, r := range strings.ToLower(value) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			pendingDash = true
			continue
		}
		if pendingDash && b.Len() > 0 {
			b.WriteByte('-')
		}
		pendingDash = false
		b.WriteRune(r)
	}
	return b.String()
}
