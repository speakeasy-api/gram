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
// place of an upstream URL that people also reach directly. It is the
// approved way to use that upstream, so a blocked direct call can point at it.
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
	// Chosen is true when an administrator named this server, rather than
	// it being the server that proxies to the same URL.
	Chosen bool
	// DirectAccessAllowed keeps direct connections to the upstream under the
	// ordinary Shadow MCP rules instead of blocking them, for example while
	// the official server is tested.
	DirectAccessAllowed bool
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

// officialCandidate is a server that may stand for an upstream URL, before
// its address is resolved.
type officialCandidate struct {
	key    string
	id     uuid.UUID
	name   string
	slug   string
	chosen bool
}

type officialChoice struct {
	key               string
	allowDirectAccess bool
	chosen            *officialCandidate
}

func choiceFromRow(serverURL string, allowDirectAccess bool, chosenID uuid.NullUUID, chosenName, chosenSlug string) officialChoice {
	choice := officialChoice{key: serverURL, allowDirectAccess: allowDirectAccess, chosen: nil}
	if !chosenID.Valid {
		return choice
	}
	name := strings.TrimSpace(chosenName)
	if name == "" {
		name = strings.TrimSpace(chosenSlug)
	}
	if name == "" {
		name = "MCP server"
	}
	choice.chosen = &officialCandidate{key: serverURL, id: chosenID.UUID, name: name, slug: chosenSlug, chosen: true}
	return choice
}

// OfficialServers maps every upstream URL the project has an official
// server for to that server. Look a URL up with [OfficialServerFor].
//
// By default the official server for a URL is the project's MCP server that
// proxies to it: the shared URL is the link, so deleting or disabling that
// server ends it with nothing to clean up. An administrator can name another
// server instead, or keep direct connections working; those choices live in
// shadow_mcp_official_servers. When several servers proxy to one URL, the
// oldest stands for it.
func OfficialServers(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL) (map[string]OfficialServer, error) {
	rows, err := remotemcprepo.New(db).ListLiveRemoteBackedMcpServers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list remote-backed mcp servers: %w", err)
	}
	choiceRows, err := shadowmcprepo.New(db).ListShadowMCPOfficialServerChoices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list shadow mcp official server choices: %w", err)
	}
	choices := make([]officialChoice, 0, len(choiceRows))
	for _, row := range choiceRows {
		choices = append(choices, choiceFromRow(row.ServerUrl, row.AllowDirectAccess, row.ChosenMcpServerID, row.ChosenMcpServerName.String, row.ChosenMcpServerSlug.String))
	}
	return resolveOfficialServers(ctx, db, organizationID, projectID, serverURL, urlMatchCandidates(rows, ""), choices)
}

// urlMatchCandidates turns the project's remote-backed servers into one
// candidate per upstream URL, the oldest first. A non-empty onlyKey keeps
// just that URL.
func urlMatchCandidates(rows []remotemcprepo.ListLiveRemoteBackedMcpServersRow, onlyKey string) []officialCandidate {
	seen := map[string]bool{}
	out := make([]officialCandidate, 0, len(rows))
	for _, row := range rows {
		key, ok := OfficialServerKey(row.RemoteUrl)
		if !ok || seen[key] || (onlyKey != "" && key != onlyKey) {
			continue
		}
		seen[key] = true
		out = append(out, officialCandidate{key: key, id: row.McpServerID, name: officialServerName(row), slug: row.McpServerSlug.String, chosen: false})
	}
	return out
}

func resolveOfficialServers(ctx context.Context, db OfficialServerDBTX, organizationID string, projectID uuid.UUID, serverURL *url.URL, urlMatches []officialCandidate, choices []officialChoice) (map[string]OfficialServer, error) {
	byKey := make(map[string]officialCandidate, len(urlMatches)+len(choices))
	for _, candidate := range urlMatches {
		byKey[candidate.key] = candidate
	}
	directAccess := map[string]bool{}
	for _, choice := range choices {
		directAccess[choice.key] = choice.allowDirectAccess
		if choice.chosen != nil {
			byKey[choice.key] = *choice.chosen
		}
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
			MCPServerID:         candidate.id,
			Name:                candidate.name,
			Slug:                candidate.slug,
			URL:                 own[candidate.id].URL,
			GatewayName:         "",
			Chosen:              candidate.chosen,
			DirectAccessAllowed: directAccess[key],
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
	return OfficialServer{MCPServerID: uuid.Nil, Name: "", Slug: "", URL: "", GatewayName: "", Chosen: false, DirectAccessAllowed: false}
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
// The common answer is "none", and that costs two indexed reads: endpoints
// are resolved only for the server that stands for the URL.
func (c *Client) OfficialServerForURL(ctx context.Context, organizationID string, projectID uuid.UUID, rawURL string) (OfficialServer, bool, error) {
	key, ok := OfficialServerKey(rawURL)
	if !ok {
		return noOfficialServer(), false, nil
	}

	var choices []officialChoice
	choice, err := shadowmcprepo.New(c.db).GetShadowMCPOfficialServerChoice(ctx, shadowmcprepo.GetShadowMCPOfficialServerChoiceParams{
		ProjectID: projectID,
		ServerUrl: key,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return noOfficialServer(), false, fmt.Errorf("get shadow mcp official server choice: %w", err)
	default:
		choices = append(choices, choiceFromRow(choice.ServerUrl, choice.AllowDirectAccess, choice.ChosenMcpServerID, choice.ChosenMcpServerName.String, choice.ChosenMcpServerSlug.String))
	}

	var urlMatches []officialCandidate
	if len(choices) == 0 || choices[0].chosen == nil {
		rows, err := remotemcprepo.New(c.db).ListLiveRemoteBackedMcpServers(ctx, projectID)
		if err != nil {
			return noOfficialServer(), false, fmt.Errorf("list remote-backed mcp servers: %w", err)
		}
		urlMatches = urlMatchCandidates(rows, key)
	}
	if len(urlMatches) == 0 && (len(choices) == 0 || choices[0].chosen == nil) {
		return noOfficialServer(), false, nil
	}

	servers, err := resolveOfficialServers(ctx, c.db, organizationID, projectID, c.serverURL, urlMatches, choices)
	if err != nil {
		return noOfficialServer(), false, err
	}
	server, found := servers[key]
	return server, found, nil
}
