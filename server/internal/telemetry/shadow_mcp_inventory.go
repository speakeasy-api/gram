package telemetry

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type ShadowMCPInventoryURL struct {
	GramProjectID string
	ServerURL     shadowmcp.InventoryURL
	ServerName    string
	SeenAt        time.Time
}

type BackfillShadowMCPInventoryURLsParams struct {
	GramProjectID string
	Limit         int
}

type BackfillShadowMCPInventoryURLsResult struct {
	InventoryURLCount int
}

func (l *Logger) UpsertShadowMCPInventoryURLs(ctx context.Context, inventoryURLs []ShadowMCPInventoryURL) error {
	if len(inventoryURLs) == 0 || l.chConn == nil {
		return nil
	}

	params := make([]repo.UpsertShadowMCPInventoryURLParams, 0, len(inventoryURLs))
	for _, inventoryURL := range inventoryURLs {
		if inventoryURL.GramProjectID == "" || inventoryURL.ServerURL.CanonicalURL == "" {
			continue
		}

		seenAt := inventoryURL.SeenAt
		if seenAt.IsZero() {
			seenAt = time.Now()
		}

		params = append(params, repo.UpsertShadowMCPInventoryURLParams{
			GramProjectID:      inventoryURL.GramProjectID,
			CanonicalServerURL: inventoryURL.ServerURL.CanonicalURL,
			URLHost:            inventoryURL.ServerURL.URLHost,
			ServerName:         inventoryURL.ServerName,
			SeenAt:             seenAt,
			FirstSeen:          time.Time{},
			LastSeen:           time.Time{},
			UpdatedAt:          time.Now(),
		})
	}

	if len(params) == 0 {
		return nil
	}

	chRepo := repo.New(l.chConn)
	if err := chRepo.UpsertShadowMCPInventoryURLs(l.shutdownCtx(), params); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert shadow mcp inventory urls")
	}

	return nil
}

func (s *Service) BackfillShadowMCPInventoryURLs(ctx context.Context, params BackfillShadowMCPInventoryURLsParams) (BackfillShadowMCPInventoryURLsResult, error) {
	if params.GramProjectID == "" {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, nil
	}

	usageRows, err := s.chRepo.ListShadowMCPInventoryUsage(ctx, repo.ListShadowMCPInventoryUsageParams{
		GramProjectID:       params.GramProjectID,
		CanonicalServerURLs: nil,
		Limit:               params.Limit,
	})
	if err != nil {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage for backfill")
	}

	upserts := make([]repo.UpsertShadowMCPInventoryURLParams, 0, len(usageRows))
	now := time.Now()
	for _, usageRow := range usageRows {
		invURL, ok := shadowmcp.CanonicalizeInventoryURL(usageRow.CanonicalServerURL)
		if !ok || usageRow.FirstCalled == nil || usageRow.LastCalled == nil {
			continue
		}

		upserts = append(upserts, repo.UpsertShadowMCPInventoryURLParams{
			GramProjectID:      params.GramProjectID,
			CanonicalServerURL: invURL.CanonicalURL,
			URLHost:            invURL.URLHost,
			ServerName:         usageRow.ServerName,
			SeenAt:             time.Time{},
			FirstSeen:          *usageRow.FirstCalled,
			LastSeen:           *usageRow.LastCalled,
			UpdatedAt:          now,
		})
	}

	if len(upserts) == 0 {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, nil
	}

	if err := s.chRepo.UpsertShadowMCPInventoryURLs(ctx, upserts); err != nil {
		return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: 0}, oops.E(oops.CodeUnexpected, err, "upsert shadow mcp inventory urls")
	}

	return BackfillShadowMCPInventoryURLsResult{InventoryURLCount: len(usageRows)}, nil
}
