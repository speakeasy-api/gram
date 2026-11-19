package shadowmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/endpointaddr"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
)

// OfficialServer is the MCP server a project serves through Speakeasy in
// place of an upstream URL that people also reach directly. A project server
// that proxies to the upstream is linked automatically; an administrator can
// choose another one or keep the upstream in direct use. A linked server is
// the approved way to use that upstream, so a blocked direct call can point
// at it.
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
	// Automatic means no administrator chose the server: it is linked
	// because it proxies to the upstream URL.
	Automatic bool
}

// SuggestedOfficialServer is a project MCP server that proxies to the same
// upstream URL as a Shadow MCP server, the oldest when several do. Unless an
// administrator chose otherwise, it is linked automatically.
type SuggestedOfficialServer struct {
	MCPServerID uuid.UUID
	Name        string
	Slug        string
}

// OfficialServerDBTX is the database surface the official-server lookup
// needs: a pool or a transaction.
type OfficialServerDBTX interface {
	remotemcprepo.DBTX
	mcpendpointsrepo.DBTX
	shadowmcprepo.DBTX
}

// OfficialServerKey is the form a Shadow MCP server's upstream URL is
// compared and stored in: the inventory's canonical URL without a trailing
// slash, so https://x/mcp and https://x/mcp/ name one upstream.
func OfficialServerKey(rawURL string) (string, bool) {
	inventory, ok := CanonicalizeInventoryURL(rawURL)
	if !ok {
		return "", false
	}
	return strings.TrimRight(inventory.CanonicalURL, "/"), true
}

// officialLink is a link from an upstream URL to a live MCP server, before
// the server's address is resolved.
type officialLink struct {
	key       string
	id        uuid.UUID
	name      string
	slug      string
	automatic bool
}

// linkFromRow reads one stored link. A link whose server was deleted or
// disabled reads as no link: the Shadow MCP server is unmanaged again.
func linkFromRow(serverURL string, chosenID uuid.NullUUID, chosenName, chosenSlug string) (officialLink, bool) {
	if !chosenID.Valid {
		return officialLink{key: "", id: uuid.Nil, name: "", slug: "", automatic: false}, false
	}
	name := strings.TrimSpace(chosenName)
	if name == "" {
		name = strings.TrimSpace(chosenSlug)
	}
	if name == "" {
		name = "MCP server"
	}
	return officialLink{key: serverURL, id: chosenID.UUID, name: name, slug: chosenSlug, automatic: false}, true
}

// automaticLink links an upstream URL to the server that proxies to it.
func automaticLink(key string, suggestion SuggestedOfficialServer) officialLink {
	return officialLink{key: key, id: suggestion.MCPServerID, name: suggestion.Name, slug: suggestion.Slug, automatic: true}
}

// OfficialServerLinks is every official-server decision in one project,
// keyed by [OfficialServerKey].
type OfficialServerLinks struct {
	// Linked holds the URLs that have an official server, chosen by an
	// administrator or linked automatically.
	Linked map[string]OfficialServer
	// Declined holds the URLs an administrator keeps in direct use: nothing
	// is linked to them, not even automatically.
	Declined map[string]bool
	// Suggested holds, per URL, the project server that proxies to it.
	Suggested map[string]SuggestedOfficialServer
}

// chooseOfficialLinks applies the administrator's choices over the automatic
// links. Per URL: a choice with a live server links that server; a choice
// with no server declines the link; a choice whose server was deleted or
// disabled, or no choice at all, links the server that proxies to the URL.
func chooseOfficialLinks(rows []shadowmcprepo.ListShadowMCPOfficialServerChoicesRow, suggested map[string]SuggestedOfficialServer) ([]officialLink, map[string]bool) {
	chosen := make(map[string]officialLink, len(rows))
	declined := map[string]bool{}
	for _, row := range rows {
		if link, ok := linkFromRow(row.ServerUrl, row.ChosenMcpServerID, row.ChosenMcpServerName.String, row.ChosenMcpServerSlug.String); ok {
			chosen[row.ServerUrl] = link
			continue
		}
		if !row.LinkMcpServerID.Valid {
			declined[row.ServerUrl] = true
		}
	}
	links := make([]officialLink, 0, len(chosen)+len(suggested))
	for _, link := range chosen {
		links = append(links, link)
	}
	for key, suggestion := range suggested {
		if _, ok := chosen[key]; ok || declined[key] {
			continue
		}
		links = append(links, automaticLink(key, suggestion))
	}
	return links, declined
}

// LoadOfficialServerLinks reads every official-server link in a project.
// Look a URL up in Linked with [OfficialServerFor].
//
// A project server that proxies to an upstream URL is its official version
// by default, the oldest when several do. An administrator's choice, stored
// in shadow_mcp_official_servers, overrides that: it names another server,
// or keeps the upstream in direct use. Deleting or disabling a chosen server
// falls back to the automatic link.
func LoadOfficialServerLinks(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL) (OfficialServerLinks, error) {
	empty := OfficialServerLinks{Linked: nil, Declined: nil, Suggested: nil}
	rows, err := shadowmcprepo.New(db).ListShadowMCPOfficialServerChoices(ctx, projectID)
	if err != nil {
		return empty, fmt.Errorf("list shadow mcp official server links: %w", err)
	}
	suggested, err := SuggestedOfficialServers(ctx, db, projectID)
	if err != nil {
		return empty, err
	}
	links, declined := chooseOfficialLinks(rows, suggested)
	linked, err := resolveOfficialServers(ctx, db, organizationID, projectID, serverURL, links)
	if err != nil {
		return empty, err
	}
	return OfficialServerLinks{Linked: linked, Declined: declined, Suggested: suggested}, nil
}

// SuggestedOfficialServers maps upstream URLs to the project's MCP server
// that proxies to the same URL, the oldest when several do. The dashboard
// offers it as the server to link when the URL has no link yet.
func SuggestedOfficialServers(ctx context.Context, db remotemcprepo.DBTX, projectID uuid.UUID) (map[string]SuggestedOfficialServer, error) {
	rows, err := remotemcprepo.New(db).ListLiveRemoteBackedMcpServers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list remote-backed mcp servers: %w", err)
	}
	out := make(map[string]SuggestedOfficialServer, len(rows))
	for _, row := range rows {
		key, ok := OfficialServerKey(row.RemoteUrl)
		if !ok {
			continue
		}
		if _, seen := out[key]; seen {
			continue
		}
		out[key] = SuggestedOfficialServer{MCPServerID: row.McpServerID, Name: officialServerName(row), Slug: row.McpServerSlug.String}
	}
	return out, nil
}

func resolveOfficialServers(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL, links []officialLink) (map[string]OfficialServer, error) {
	byKey := make(map[string]officialLink, len(links))
	for _, link := range links {
		byKey[link.key] = link
	}
	if len(byKey) == 0 {
		return map[string]OfficialServer{}, nil
	}

	baseURL := ""
	if serverURL != nil {
		baseURL = strings.TrimRight(serverURL.String(), "/")
	}

	serverIDs := make([]uuid.UUID, 0, len(byKey))
	for _, candidate := range byKey {
		serverIDs = append(serverIDs, candidate.id)
	}

	own, err := endpointaddr.PrimaryEndpointsByServer(ctx, db, organizationID, []uuid.UUID{projectID}, serverIDs, baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve official server endpoints: %w", err)
	}
	gateways, err := officialServerGateways(ctx, db, organizationID, projectID, serverIDs, baseURL)
	if err != nil {
		return nil, err
	}

	out := make(map[string]OfficialServer, len(byKey))
	for key, candidate := range byKey {
		server := OfficialServer{
			MCPServerID: candidate.id,
			Name:        candidate.name,
			Slug:        candidate.slug,
			URL:         own[candidate.id].URL,
			GatewayName: "",
			Automatic:   candidate.automatic,
		}
		if gateway, ok := gateways[candidate.id]; ok {
			server.URL = gateway.url
			server.GatewayName = gateway.name
		}
		out[key] = server
	}
	return out, nil
}

// OfficialServerFor finds the official server for one observed server URL.
func OfficialServerFor(servers map[string]OfficialServer, rawURL string) (OfficialServer, bool) {
	key, ok := OfficialServerKey(rawURL)
	if !ok {
		return noOfficialServer(), false
	}
	server, found := servers[key]
	return server, found
}

func noOfficialServer() OfficialServer {
	return OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: "", Automatic: false}
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

// OfficialServerForURL finds the official server linked to one observed
// server URL, read the same way as [LoadOfficialServerLinks]. The realtime
// hook guard calls it for calls that are about to be judged, so it reports
// failure rather than guessing: a caller that cannot tell must not treat the
// URL as having an official version.
//
// It costs one indexed read for the administrator's choice and, without a
// choice, one read of the project's remote-backed servers. Endpoints are
// resolved only for a linked server.
func (c *Client) OfficialServerForURL(ctx context.Context, organizationID string, projectID uuid.UUID, rawURL string) (OfficialServer, bool, error) {
	key, ok := OfficialServerKey(rawURL)
	if !ok {
		return noOfficialServer(), false, nil
	}

	link, found, err := c.officialLinkForKey(ctx, projectID, key)
	if err != nil || !found {
		return noOfficialServer(), false, err
	}

	servers, err := resolveOfficialServers(ctx, c.db, organizationID, projectID, c.serverURL, []officialLink{link})
	if err != nil {
		return noOfficialServer(), false, err
	}
	server, found := servers[key]
	return server, found, nil
}

// officialLinkForKey applies the rules of [chooseOfficialLinks] to one URL.
func (c *Client) officialLinkForKey(ctx context.Context, projectID uuid.UUID, key string) (officialLink, bool, error) {
	none := officialLink{key: "", id: uuid.Nil, name: "", slug: "", automatic: false}
	row, err := shadowmcprepo.New(c.db).GetShadowMCPOfficialServerChoice(ctx, shadowmcprepo.GetShadowMCPOfficialServerChoiceParams{
		ProjectID: projectID,
		ServerUrl: key,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, false, fmt.Errorf("get shadow mcp official server link: %w", err)
	}
	if err == nil {
		if link, ok := linkFromRow(row.ServerUrl, row.ChosenMcpServerID, row.ChosenMcpServerName.String, row.ChosenMcpServerSlug.String); ok {
			return link, true, nil
		}
		if !row.LinkMcpServerID.Valid {
			// The administrator keeps this URL in direct use.
			return none, false, nil
		}
	}

	suggested, err := SuggestedOfficialServers(ctx, c.db, projectID)
	if err != nil {
		return none, false, err
	}
	suggestion, ok := suggested[key]
	if !ok {
		return none, false, nil
	}
	return automaticLink(key, suggestion), true, nil
}
