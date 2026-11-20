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
	// Routes are every way agents can reach the server: its own endpoint
	// first, then each gateway that serves it, oldest first. Empty when the
	// server has no addressable endpoint and no gateway serves it.
	Routes []OfficialRoute
}

// OfficialRoute is one way to reach an official server: the server's own
// endpoint, or a gateway that serves it as a member.
type OfficialRoute struct {
	// GatewayName is the gateway's name; "" for the server's own endpoint.
	GatewayName string
	// URL is the route's primary address, the one to give out.
	URL string
	// keys are every address of the route, primary included, in the form
	// OfficialServerKey compares URLs in. A connected MCP server at any of
	// them is reached through this route.
	keys []string
}

// NewOfficialRoute builds a route resolved outside this package, such as
// the derived project gateway. aliases are other addresses that reach the
// same route; a connected MCP server at any of them counts as this route.
func NewOfficialRoute(gatewayName, routeURL string, aliases ...string) OfficialRoute {
	route := OfficialRoute{GatewayName: gatewayName, URL: routeURL, keys: nil}
	for _, raw := range append([]string{routeURL}, aliases...) {
		if key, ok := OfficialServerKey(raw); ok {
			route.keys = append(route.keys, key)
		}
	}
	return route
}

// ConnectedRoute finds the route an agent already has connected, given the
// URLs of the MCP servers its session lists. Speakeasy cannot tell which
// route a person uses from anything else, so without a match the caller
// offers every route.
func (s OfficialServer) ConnectedRoute(connectedURLs []string) (OfficialRoute, bool) {
	connected := make(map[string]struct{}, len(connectedURLs))
	for _, raw := range connectedURLs {
		if key, ok := OfficialServerKey(raw); ok {
			connected[key] = struct{}{}
		}
	}
	for _, route := range s.Routes {
		for _, key := range route.keys {
			if _, ok := connected[key]; ok {
				return route, true
			}
		}
	}
	return OfficialRoute{GatewayName: "", URL: "", keys: nil}, false
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
	// Automatic reports whether automatic links apply in the project
	// ([feature.FlagShadowMCPAutoLink]). Without them, only links an
	// administrator made are linked, and Declined has no effect.
	Automatic bool
}

// chooseOfficialLinks applies the administrator's choices over the automatic
// links. Per URL: a choice with a live server links that server; a choice
// with no server declines the link; a choice whose server was deleted or
// disabled, or no choice at all, links the server that proxies to the URL.
// Without automatic links, only the first rule applies.
func chooseOfficialLinks(rows []shadowmcprepo.ListShadowMCPOfficialServerChoicesRow, suggested map[string]SuggestedOfficialServer, automatic bool) ([]officialLink, map[string]bool) {
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
	if !automatic {
		return links, declined
	}
	for key, suggestion := range suggested {
		if _, ok := chosen[key]; ok || declined[key] {
			continue
		}
		links = append(links, automaticLink(key, suggestion))
	}
	return links, declined
}

// LoadOfficialServerLinks reads the official-server links of a project.
// Look a URL up in Linked with [OfficialServerFor]. With keys, only those
// URLs (in [OfficialServerKey] form) are resolved, so a page of the
// inventory pays only for the servers it shows; nil resolves every URL.
//
// With automatic links ([feature.FlagShadowMCPAutoLink], read by the caller
// and passed as automatic), a project server that proxies to an upstream URL
// is its official version by default, the oldest when several do. An administrator's choice, stored
// in shadow_mcp_official_servers, overrides that: it names another server,
// or keeps the upstream in direct use. Deleting or disabling a chosen server
// falls back to the automatic link.
func LoadOfficialServerLinks(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL, keys map[string]bool, automatic bool) (OfficialServerLinks, error) {
	empty := OfficialServerLinks{Linked: nil, Declined: nil, Suggested: nil, Automatic: automatic}
	rows, err := shadowmcprepo.New(db).ListShadowMCPOfficialServerChoices(ctx, projectID)
	if err != nil {
		return empty, fmt.Errorf("list shadow mcp official server links: %w", err)
	}
	suggested, err := SuggestedOfficialServers(ctx, db, projectID)
	if err != nil {
		return empty, err
	}
	links, declined := chooseOfficialLinks(rows, suggested, automatic)
	if keys != nil {
		wanted := make([]officialLink, 0, len(links))
		for _, link := range links {
			if keys[link.key] {
				wanted = append(wanted, link)
			}
		}
		links = wanted
	}
	linked, err := resolveOfficialServers(ctx, db, organizationID, projectID, serverURL, links)
	if err != nil {
		return empty, err
	}
	return OfficialServerLinks{Linked: linked, Declined: declined, Suggested: suggested, Automatic: automatic}, nil
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

	own, err := officialServerOwnRoutes(ctx, db, organizationID, projectID, serverIDs, baseURL)
	if err != nil {
		return nil, err
	}
	gateways, err := officialServerGatewayRoutes(ctx, db, organizationID, projectID, serverIDs, baseURL)
	if err != nil {
		return nil, err
	}

	out := make(map[string]OfficialServer, len(byKey))
	for key, candidate := range byKey {
		server := OfficialServer{
			MCPServerID: candidate.id,
			Name:        candidate.name,
			Slug:        candidate.slug,
			URL:         "",
			GatewayName: "",
			Automatic:   candidate.automatic,
			Routes:      nil,
		}
		if route, ok := own[candidate.id]; ok {
			server.URL = route.URL
			server.Routes = append(server.Routes, route)
		}
		// A gateway is preferred as the one address to give out: it is
		// where the organization gathers its servers.
		if routes := gateways[candidate.id]; len(routes) > 0 {
			server.URL = routes[0].URL
			server.GatewayName = routes[0].GatewayName
			server.Routes = append(server.Routes, routes...)
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
	return OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: "", Automatic: false, Routes: nil}
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

// endpointRoute builds a route from one server's or gateway's addressable
// endpoints. It reports false when none can be pointed at: no endpoint, or
// only platform endpoints while the platform address is unknown.
func endpointRoute(gatewayName string, endpoints []mcpendpointsrepo.McpEndpoint, domains map[uuid.UUID]string, baseURL string) (OfficialRoute, bool, error) {
	route := OfficialRoute{GatewayName: gatewayName, URL: "", keys: nil}
	primary := endpointaddr.PrimaryEndpoint(endpoints)
	if primary == nil {
		return route, false, nil
	}
	for _, endpoint := range endpoints {
		domain := domains[endpoint.ID]
		if domain == "" && baseURL == "" {
			continue
		}
		endpointURL, err := endpointaddr.EndpointURL(&endpoint, domain, baseURL)
		if err != nil {
			return route, false, fmt.Errorf("build official server endpoint url: %w", err)
		}
		if endpoint.ID == primary.ID {
			route.URL = endpointURL
		}
		if key, ok := OfficialServerKey(endpointURL); ok {
			route.keys = append(route.keys, key)
		}
	}
	return route, route.URL != "", nil
}

// officialServerOwnRoutes resolves each server's own route: its primary
// addressable endpoint, plus every other address it answers at.
func officialServerOwnRoutes(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverIDs []uuid.UUID, baseURL string) (map[uuid.UUID]OfficialRoute, error) {
	rows, err := mcpendpointsrepo.New(db).ListAddressableMCPEndpointsByMCPServerIDs(ctx, mcpendpointsrepo.ListAddressableMCPEndpointsByMCPServerIDsParams{
		OrganizationID: organizationID,
		ProjectIds:     []uuid.UUID{projectID},
		McpServerIds:   serverIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve official server endpoints: %w", err)
	}
	endpoints := map[uuid.UUID][]mcpendpointsrepo.McpEndpoint{}
	domains := map[uuid.UUID]string{}
	for _, row := range rows {
		serverID := row.McpEndpoint.McpServerID.UUID
		endpoints[serverID] = append(endpoints[serverID], row.McpEndpoint)
		if row.CustomDomain.Valid {
			domains[row.McpEndpoint.ID] = row.CustomDomain.String
		}
	}

	out := make(map[uuid.UUID]OfficialRoute, len(endpoints))
	for serverID, serverEndpoints := range endpoints {
		route, ok, err := endpointRoute("", serverEndpoints, domains, baseURL)
		if err != nil {
			return nil, err
		}
		if ok {
			out[serverID] = route
		}
	}
	return out, nil
}

// officialServerGatewayRoutes resolves, for each member server, a route
// through every gateway that serves it, oldest gateway first. A gateway
// without an addressable endpoint cannot be pointed at, so it is skipped.
func officialServerGatewayRoutes(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverIDs []uuid.UUID, baseURL string) (map[uuid.UUID][]OfficialRoute, error) {
	rows, err := mcpendpointsrepo.New(db).ListAddressableGatewayEndpointsByMemberMCPServerIDs(ctx, mcpendpointsrepo.ListAddressableGatewayEndpointsByMemberMCPServerIDsParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		McpServerIds:   serverIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list gateway endpoints for official servers: %w", err)
	}

	type gateway struct {
		id        uuid.UUID
		name      string
		endpoints []mcpendpointsrepo.McpEndpoint
	}
	// Rows arrive ordered by gateway age, so each member's gateways keep
	// that order.
	byMember := map[uuid.UUID][]*gateway{}
	domains := map[uuid.UUID]string{}
	for _, row := range rows {
		gatewayID := row.McpEndpoint.MetaMcpServerID.UUID
		gateways := byMember[row.MemberMcpServerID]
		var current *gateway
		for _, candidate := range gateways {
			if candidate.id == gatewayID {
				current = candidate
				break
			}
		}
		if current == nil {
			current = &gateway{id: gatewayID, name: row.GatewayName, endpoints: nil}
			byMember[row.MemberMcpServerID] = append(gateways, current)
		}
		current.endpoints = append(current.endpoints, row.McpEndpoint)
		if row.CustomDomain.Valid {
			domains[row.McpEndpoint.ID] = row.CustomDomain.String
		}
	}

	out := make(map[uuid.UUID][]OfficialRoute, len(byMember))
	for memberID, gateways := range byMember {
		for _, current := range gateways {
			route, ok, err := endpointRoute(current.name, current.endpoints, domains, baseURL)
			if err != nil {
				return nil, err
			}
			if ok {
				out[memberID] = append(out[memberID], route)
			}
		}
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

	link, found, err := c.officialLinkForKey(ctx, organizationID, projectID, key)
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
func (c *Client) officialLinkForKey(ctx context.Context, organizationID string, projectID uuid.UUID, key string) (officialLink, bool, error) {
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

	if !c.AutoLinkEnabled(ctx, organizationID, projectID) {
		return none, false, nil
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
