package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// BuildToolMetadataView converts a repo mcp_server_tool_metadata row into the
// API response type.
func BuildToolMetadataView(row repo.McpServerToolMetadatum) *types.ToolMetadata {
	var deletedAt *string
	if row.DeletedAt.Valid {
		formatted := row.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &formatted
	}

	return &types.ToolMetadata{
		McpServerID:     row.McpServerID.String(),
		ToolName:        row.ToolName,
		Title:           conv.FromPGText[string](row.Title),
		ReadOnlyHint:    conv.FromPGBool[bool](row.ReadOnlyHint),
		DestructiveHint: conv.FromPGBool[bool](row.DestructiveHint),
		IdempotentHint:  conv.FromPGBool[bool](row.IdempotentHint),
		OpenWorldHint:   conv.FromPGBool[bool](row.OpenWorldHint),
		CreatedAt:       row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
	}
}

// BuildToolMetadataListView converts a slice of repo rows into API types.
func BuildToolMetadataListView(rows []repo.McpServerToolMetadatum) []*types.ToolMetadata {
	result := make([]*types.ToolMetadata, len(rows))
	for i, row := range rows {
		result[i] = BuildToolMetadataView(row)
	}
	return result
}

// BuildToolMetadataSetView converts a row returned by the authoritative
// SetMCPServerToolMetadata write into the API response type. The row carries the
// table's columns plus the was_deleted discriminator, which is not part of the
// view.
func BuildToolMetadataSetView(row repo.SetMCPServerToolMetadataRow) *types.ToolMetadata {
	return BuildToolMetadataView(repo.McpServerToolMetadatum{
		ID:              row.ID,
		ProjectID:       row.ProjectID,
		McpServerID:     row.McpServerID,
		ToolName:        row.ToolName,
		Title:           row.Title,
		ReadOnlyHint:    row.ReadOnlyHint,
		DestructiveHint: row.DestructiveHint,
		IdempotentHint:  row.IdempotentHint,
		OpenWorldHint:   row.OpenWorldHint,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		DeletedAt:       row.DeletedAt,
		Deleted:         row.Deleted,
	})
}
