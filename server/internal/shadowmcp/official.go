package shadowmcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/endpointaddr"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// OfficialServer is the MCP server a project serves through Speakeasy for an
// upstream URL that people also reach directly. It is the approved way to
// use that upstream, so a blocked direct call can point at it.
type OfficialServer struct {
	MCPServerID uuid.UUID
	// Name is what the dashboard calls the server; never empty.
	Name string
	// Slug is the MCP server's slug; "" for a server created without one.
	Slug string
	// URL is where agents connect: the gateway's address when the server is
	// a member of one, otherwise the server's own primary endpoint. Empty
	// when the server has no addressable endpoint yet.
	URL string
	// GatewayName is set when URL is a gateway's address.
	GatewayName string
}

// OfficialServerDBTX is the database surface the official-server lookup
// needs: a pool or a transaction.
type OfficialServerDBTX interface {
	remotemcprepo.DBTX
	mcpendpointsrepo.DBTX
}

// officialServerKey is the form two URLs are compared in: the inventory's
// canonical URL without a trailing slash, so https://x/mcp and https://x/mcp/
// name one upstream.
func officialServerKey(rawURL string) (string, bool) {
	inventory, ok := CanonicalizeInventoryURL(rawURL)
	if !ok {
		return "", false
	}
	return strings.TrimRight(inventory.CanonicalURL, "/"), true
}

// OfficialServers maps every upstream URL the project serves through
// Speakeasy to its official server. Look a URL up with [OfficialServerFor].
//
// No link is stored between a Shadow MCP server and its official version:
// the two name the same upstream URL, and that is the link. Deleting or
// disabling the official server therefore ends it with nothing to clean up.
// When several servers proxy to one URL, the oldest stands for it.
func OfficialServers(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL) (map[string]OfficialServer, error) {
	rows, err := remotemcprepo.New(db).ListLiveRemoteBackedMcpServers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list remote-backed mcp servers: %w", err)
	}
	return resolveOfficialServers(ctx, db, organizationID, projectID, serverURL, rows)
}

func resolveOfficialServers(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL, rows []remotemcprepo.ListLiveRemoteBackedMcpServersRow) (map[string]OfficialServer, error) {
	if len(rows) == 0 {
		return map[string]OfficialServer{}, nil
	}

	baseURL := ""
	if serverURL != nil {
		baseURL = strings.TrimRight(serverURL.String(), "/")
	}

	serverIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		serverIDs = append(serverIDs, row.McpServerID)
	}

	own, err := endpointaddr.PrimaryEndpointsByServer(ctx, db, organizationID, []uuid.UUID{projectID}, serverIDs, baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve official server endpoints: %w", err)
	}
	gateways, err := officialServerGateways(ctx, db, organizationID, projectID, serverIDs, baseURL)
	if err != nil {
		return nil, err
	}

	out := make(map[string]OfficialServer, len(rows))
	for _, row := range rows {
		key, ok := officialServerKey(row.RemoteUrl)
		if !ok {
			continue
		}
		if _, taken := out[key]; taken {
			continue
		}
		server := OfficialServer{
			MCPServerID: row.McpServerID,
			Name:        officialServerName(row),
			Slug:        row.McpServerSlug.String,
			URL:         own[row.McpServerID].URL,
			GatewayName: "",
		}
		if gateway, ok := gateways[row.McpServerID]; ok {
			server.URL = gateway.url
			server.GatewayName = gateway.name
		}
		out[key] = server
	}
	return out, nil
}

// OfficialServerFor finds the official server for one observed server URL.
func OfficialServerFor(servers map[string]OfficialServer, rawURL string) (OfficialServer, bool) {
	key, ok := officialServerKey(rawURL)
	if !ok {
		return OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: ""}, false
	}
	server, found := servers[key]
	return server, found
}

func officialServerName(row remotemcprepo.ListLiveRemoteBackedMcpServersRow) string {
	if name := strings.TrimSpace(row.McpServerName.String); name != "" {
		return name
	}
	if name := strings.TrimSpace(row.RemoteName.String); name != "" {
		return name
	}
	if inventory, ok := CanonicalizeInventoryURL(row.RemoteUrl); ok && inventory.URLHost != "" {
		return inventory.URLHost
	}
	return "MCP server"
}

type officialServerGateway struct {
	name string
	url  string
}

// officialServerGateways resolves, for each member server, the address of
// the first gateway that serves it. A gateway without an addressable
// endpoint cannot be pointed at, so it is skipped.
func officialServerGateways(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverIDs []uuid.UUID, baseURL string) (map[uuid.UUID]officialServerGateway, error) {
	rows, err := mcpendpointsrepo.New(db).ListAddressableGatewayEndpointsByMemberMCPServerIDs(ctx, mcpendpointsrepo.ListAddressableGatewayEndpointsByMemberMCPServerIDsParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		McpServerIds:   serverIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list gateway endpoints for official servers: %w", err)
	}

	type candidate struct {
		gatewayID uuid.UUID
		name      string
		endpoints []mcpendpointsrepo.McpEndpoint
		domains   map[uuid.UUID]string
	}
	// Rows arrive ordered by gateway age, so the first gateway seen for a
	// member is its oldest; later gateways for the same member are ignored.
	byMember := map[uuid.UUID]*candidate{}
	for _, row := range rows {
		gatewayID := row.McpEndpoint.MetaMcpServerID.UUID
		current, ok := byMember[row.MemberMcpServerID]
		if !ok {
			current = &candidate{gatewayID: gatewayID, name: row.GatewayName, endpoints: nil, domains: map[uuid.UUID]string{}}
			byMember[row.MemberMcpServerID] = current
		}
		if current.gatewayID != gatewayID {
			continue
		}
		current.endpoints = append(current.endpoints, row.McpEndpoint)
		if row.CustomDomain.Valid {
			current.domains[row.McpEndpoint.ID] = row.CustomDomain.String
		}
	}

	out := make(map[uuid.UUID]officialServerGateway, len(byMember))
	for memberID, current := range byMember {
		primary := endpointaddr.PrimaryEndpoint(current.endpoints)
		if primary == nil {
			continue
		}
		domain := current.domains[primary.ID]
		if domain == "" && baseURL == "" {
			continue
		}
		endpointURL, err := endpointaddr.EndpointURL(primary, domain, baseURL)
		if err != nil {
			return nil, fmt.Errorf("build gateway endpoint url: %w", err)
		}
		out[memberID] = officialServerGateway{name: current.name, url: endpointURL}
	}
	return out, nil
}

// OfficialServerForURL finds the official server the project serves for one
// observed server URL. The realtime hook guard calls it for calls that are
// about to be judged, so it reports failure rather than guessing: a caller
// that cannot tell must not treat the URL as having an official version.
//
// The common answer is "none", and that costs one indexed read: endpoints
// are resolved only for the server that matched.
func (c *Client) OfficialServerForURL(ctx context.Context, organizationID string, projectID uuid.UUID, rawURL string) (OfficialServer, bool, error) {
	none := OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: ""}
	key, ok := officialServerKey(rawURL)
	if !ok {
		return none, false, nil
	}
	rows, err := remotemcprepo.New(c.db).ListLiveRemoteBackedMcpServers(ctx, projectID)
	if err != nil {
		return none, false, fmt.Errorf("list remote-backed mcp servers: %w", err)
	}
	for _, row := range rows {
		rowKey, ok := officialServerKey(row.RemoteUrl)
		if !ok || rowKey != key {
			continue
		}
		// Rows are oldest first, so the first match is the one that stands
		// for the URL, the same choice OfficialServers makes.
		servers, err := resolveOfficialServers(ctx, c.db, organizationID, projectID, c.serverURL, []remotemcprepo.ListLiveRemoteBackedMcpServersRow{row})
		if err != nil {
			return none, false, err
		}
		server, found := servers[key]
		return server, found, nil
	}
	return none, false, nil
}
