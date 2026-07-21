package hooks

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// shadowMCPInventoryUpsertTimeout bounds the detached inventory capture. The
// work is best-effort telemetry, so the bound is not about latency — it stops
// a saturated Postgres pool (a deadline-less acquire can wait unboundedly) or
// a hung write from retaining one goroutine per capture.
const shadowMCPInventoryUpsertTimeout = 10 * time.Second

// upsertShadowMCPInventoryURLs records the session's external MCP servers in
// the shadow-MCP inventory. The upsert is pure telemetry — nothing in the hook
// response depends on it — so the whole unit (custom-domain lookup,
// canonicalization, ClickHouse write) runs detached from the request:
// synchronous ClickHouse writes here held hook responses for multiple seconds
// (DNO-521/DNO-606). WithoutCancel keeps the work alive after the hook
// response is sent; the re-bound timeout keeps it from living forever.
func (s *Service) upsertShadowMCPInventoryURLs(ctx context.Context, orgID string, projectID string, sessionID string, entries []MCPServerEntry) {
	if s.telemetryLogger == nil || projectID == "" || len(entries) == 0 {
		return
	}

	seenAt := time.Now()
	detachedCtx := context.WithoutCancel(ctx)
	go func() {
		asyncCtx, cancel := context.WithTimeout(detachedCtx, shadowMCPInventoryUpsertTimeout)
		defer cancel()

		inventoryURLs := make([]telemetry.ShadowMCPInventoryURL, 0, len(entries))
		for _, entry := range entries {
			if entry.URL == "" {
				continue
			}
			if s.shadowMCPClient.IsGramHostedMCPURLForOrg(asyncCtx, entry.URL, orgID) {
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

		if err := s.telemetryLogger.UpsertShadowMCPInventoryURLs(asyncCtx, inventoryURLs); err != nil {
			s.logger.WarnContext(asyncCtx, "shadow MCP inventory URL upsert failed",
				attr.SlogEvent("shadow_mcp_inventory_url_upsert_failed"),
				attr.SlogError(err),
				attr.SlogProjectID(projectID),
				attr.SlogGenAIConversationID(sessionID),
			)
		}
	}()
}
