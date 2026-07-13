package hooks

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func (s *Service) upsertShadowMCPInventoryURLs(ctx context.Context, orgID string, projectID string, sessionID string, entries []MCPServerEntry) {
	if s.telemetryLogger == nil || projectID == "" || len(entries) == 0 {
		return
	}

	seenAt := time.Now()
	inventoryURLs := make([]telemetry.ShadowMCPInventoryURL, 0, len(entries))
	for _, entry := range entries {
		if entry.URL == "" {
			continue
		}
		if s.isGramHostedMCPURLForOrg(ctx, entry.URL, orgID) {
			continue
		}
		invURL, ok := shadowmcp.CanonicalizeInventoryURL(entry.URL)
		if !ok {
			continue
		}
		inventoryURLs = append(inventoryURLs, telemetry.ShadowMCPInventoryURL{
			GramProjectID: projectID,
			ServerURL:     invURL,
			ServerName:    entry.Name,
			SeenAt:        seenAt,
		})
	}
	if len(inventoryURLs) == 0 {
		return
	}

	if err := s.telemetryLogger.UpsertShadowMCPInventoryURLs(ctx, inventoryURLs); err != nil {
		s.logger.WarnContext(ctx, "shadow MCP inventory URL upsert failed",
			attr.SlogEvent("shadow_mcp_inventory_url_upsert_failed"),
			attr.SlogError(err),
			attr.SlogProjectID(projectID),
			attr.SlogGenAIConversationID(sessionID),
		)
	}
}
