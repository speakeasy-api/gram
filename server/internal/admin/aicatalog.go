package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// maxListedAIProducts bounds one catalog list response. The catalog caps live
// products at 500; the headroom leaves room for deleted rows, and a search
// narrows anything beyond it.
const maxListedAIProducts = 1000

func (s *Service) ListAIProducts(ctx context.Context, p *gen.ListAIProductsPayload) (*gen.ListAIProductsResult, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	query := pgtype.Text{String: "", Valid: false}
	if p.Query != nil {
		if trimmed := strings.TrimSpace(*p.Query); trimmed != "" {
			query = conv.ToPGText(trimmed)
		}
	}

	limit := maxListedAIProducts
	if s.aiProductListLimit > 0 {
		limit = s.aiProductListLimit
	}

	rows, err := repo.New(s.db).AdminListAIProducts(ctx, repo.AdminListAIProductsParams{
		IncludeDeleted: p.IncludeDeleted,
		Query:          query,
		RowLimit:       conv.SafeInt32(limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ai products").LogError(ctx, s.logger)
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	products := make([]*gen.AdminAIProductSummary, 0, len(rows))
	for _, row := range rows {
		products = append(products, &gen.AdminAIProductSummary{
			ID:            row.ID,
			DisplayName:   row.DisplayName,
			Category:      conv.FromPGText[string](row.Category),
			VendorKey:     row.VendorKey,
			DocumentCount: int(row.DocumentCount),
			Deleted:       row.Deleted,
			UpdatedAt:     formatAICatalogTime(row.UpdatedAt),
		})
	}
	return &gen.ListAIProductsResult{Products: products, Truncated: truncated}, nil
}

func (s *Service) GetAIProduct(ctx context.Context, p *gen.GetAIProductPayload) (*gen.AdminAIProduct, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	q := repo.New(s.db)
	product, err := q.AdminGetAIProduct(ctx, p.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "product not found").LogInfo(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get ai product").LogError(ctx, s.logger)
	}

	documents, err := q.AdminListAIProductDocuments(ctx, product.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ai product documents").LogError(ctx, s.logger)
	}

	result := &gen.AdminAIProduct{
		ID:                       product.ID,
		DisplayName:              product.DisplayName,
		Category:                 conv.FromPGText[string](product.Category),
		VendorKey:                product.VendorKey,
		BundleIds:                product.BundleIds,
		Binaries:                 product.Binaries,
		ConfigDirs:               product.ConfigDirs,
		ProcessNames:             product.ProcessNames,
		VersionPlistKey:          conv.FromPGText[string](product.VersionPlistKey),
		ClientInfoNames:          product.ClientInfoNames,
		CreatedBy:                conv.FromPGText[string](product.CreatedBy),
		UpdatedBy:                conv.FromPGText[string](product.UpdatedBy),
		Deleted:                  product.Deleted,
		CreatedAt:                formatAICatalogTime(product.CreatedAt),
		UpdatedAt:                formatAICatalogTime(product.UpdatedAt),
		BlockedOrganizationCount: int(product.BlockedOrganizationCount),
		Documents:                make([]*gen.AdminAIProductDocument, 0, len(documents)),
	}
	for _, d := range documents {
		var verifiedAt *string
		if d.VerifiedAt.Valid {
			verifiedAt = new(formatAICatalogTime(d.VerifiedAt))
		}
		result.Documents = append(result.Documents, &gen.AdminAIProductDocument{
			ID:                d.ID.String(),
			URL:               d.Url,
			DisplayName:       d.DisplayName,
			Enabled:           d.Enabled,
			IsPattern:         d.IsPattern,
			VerifiedAt:        verifiedAt,
			VerificationError: conv.FromPGText[string](d.VerificationError),
			CreatedBy:         conv.FromPGText[string](d.CreatedBy),
			UpdatedBy:         conv.FromPGText[string](d.UpdatedBy),
			Deleted:           d.Deleted,
			CreatedAt:         formatAICatalogTime(d.CreatedAt),
			UpdatedAt:         formatAICatalogTime(d.UpdatedAt),
		})
	}
	return result, nil
}

func formatAICatalogTime(t pgtype.Timestamptz) string {
	return t.Time.UTC().Format(time.RFC3339)
}
