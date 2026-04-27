package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpslugs/repo"
)

// BuildMcpSlugView converts a repo mcp_slugs row into the API response type.
func BuildMcpSlugView(slug repo.McpSlug) *types.McpSlug {
	return &types.McpSlug{
		ID:             slug.ID.String(),
		ProjectID:      slug.ProjectID.String(),
		CustomDomainID: conv.FromNullableUUID(slug.CustomDomainID),
		McpFrontendID:  slug.McpFrontendID.String(),
		Slug:           types.McpSlugString(slug.Slug),
		CreatedAt:      slug.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      slug.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildMcpSlugListView converts a slice of repo rows into API types.
func BuildMcpSlugListView(slugs []repo.McpSlug) []*types.McpSlug {
	result := make([]*types.McpSlug, len(slugs))
	for i, s := range slugs {
		result[i] = BuildMcpSlugView(s)
	}
	return result
}
