package access

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// FindShadowMCPInventoryServers finds the Shadow MCP servers in use that
// share an upstream with one of the given URLs. The catalog asks before it
// adds a server, so it can offer to replace the Shadow MCP version with the
// new one. URLs are compared by their official-server key, the same form a
// link is stored in, so a match is exactly what a link would cover.
func (s *Service) FindShadowMCPInventoryServers(ctx context.Context, payload *gen.FindShadowMCPInventoryServersPayload) (*gen.FindShadowMCPInventoryServersResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, err
	}

	// The inventory stores each URL in its canonical form, which keeps a
	// trailing slash when one was observed. A key matches either form.
	candidates := make([]string, 0, 2*len(payload.ServerUrls))
	seen := make(map[string]struct{}, 2*len(payload.ServerUrls))
	for _, rawURL := range payload.ServerUrls {
		key, ok := shadowmcp.OfficialServerKey(rawURL)
		if !ok {
			continue
		}
		for _, candidate := range []string{key, key + "/"} {
			if _, dup := seen[candidate]; dup {
				continue
			}
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	result := &gen.FindShadowMCPInventoryServersResult{Matches: []*gen.ShadowMCPInventoryServerMatch{}}
	if len(candidates) == 0 {
		return result, nil
	}

	chRepo := telemetryrepo.New(s.chConn)
	rows, err := chRepo.ListShadowMCPInventoryURLsByCanonicalURLs(ctx, telemetryrepo.ListShadowMCPInventoryURLsByCanonicalURLsParams{
		GramProjectID:       projectID.String(),
		CanonicalServerURLs: candidates,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "find shadow mcp inventory urls").LogError(ctx, s.logger)
	}
	if len(rows) == 0 {
		return result, nil
	}

	// When both forms of one upstream were observed, the most recently seen
	// row speaks for it: one link covers both.
	byKey := make(map[string]telemetryrepo.ShadowMCPInventoryURLRow, len(rows))
	for _, row := range rows {
		key, ok := shadowmcp.OfficialServerKey(row.CanonicalServerURL)
		if !ok {
			continue
		}
		if current, found := byKey[key]; found && !row.LastSeen.After(current.LastSeen) {
			continue
		}
		byKey[key] = row
	}

	canonicalURLs := make([]string, 0, len(byKey))
	for _, row := range byKey {
		canonicalURLs = append(canonicalURLs, row.CanonicalServerURL)
	}
	usageRows, err := chRepo.ListShadowMCPInventoryUsage(ctx, telemetryrepo.ListShadowMCPInventoryUsageParams{
		GramProjectID:       projectID.String(),
		CanonicalServerURLs: canonicalURLs,
		Limit:               shadowMCPInventoryUsageTraceLimit,
		OrganizationID:      "",
		UserKeys:            nil,
		From:                nil,
		To:                  nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage").LogError(ctx, s.logger)
	}
	usageByURL := shadowMCPInventoryUsageByURL(usageRows)
	policyState, err := s.shadowMCPInventoryPolicyState(ctx, ac.ActiveOrganizationID, projectID, canonicalURLs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
	}

	serversByKey := make(map[string]*gen.ShadowMCPInventoryServer, len(byKey))
	servers := make([]*gen.ShadowMCPInventoryServer, 0, len(byKey))
	for key, row := range byKey {
		server := buildShadowMCPInventoryServer(row, usageByURL[row.CanonicalServerURL], policyState.forURL(row.CanonicalServerURL), shadowMCPTargetKindServerURL)
		serversByKey[key] = server
		servers = append(servers, server)
	}
	s.attachShadowMCPOfficialServers(ctx, ac.ActiveOrganizationID, projectID, servers)

	for _, rawURL := range payload.ServerUrls {
		key, ok := shadowmcp.OfficialServerKey(rawURL)
		if !ok {
			continue
		}
		server, found := serversByKey[key]
		if !found {
			continue
		}
		result.Matches = append(result.Matches, &gen.ShadowMCPInventoryServerMatch{ServerURL: rawURL, Server: server})
	}
	return result, nil
}
