// Backfill script to update HTTP tool names and toolset references
// due to the sanitization logic change that no longer converts hyphens to underscores.
//
// This script:
// 1. Identifies HTTP tools with hyphens in their openapiv3_operation field
// 2. Calculates new names using the updated sanitization logic (preserving hyphens)
// 3. Updates the tool records with new names
// 4. Updates any toolsets that reference these tools by their old names
//
// To run this script, set DATABASE_URL environment variable and call main4()

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ettle/strcase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/tools"
)

func main1() {
	ctx := context.Background()

	// Get database connection string from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.ErrorContext(ctx, "DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// Connect to database
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		slog.ErrorContext(ctx, "failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer conn.Close(ctx)

	slog.InfoContext(ctx, "connected to database successfully")

	// Backfill tool names that would be affected by the sanitization change
	if err := backfillToolNameSanitization(ctx, conn); err != nil {
		slog.ErrorContext(ctx, "failed to backfill tool name sanitization", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "backfill completed successfully")
}

func backfillToolNameSanitization(ctx context.Context, conn *pgx.Conn) error {
	// First, identify tools whose names would be changed by the new sanitization logic
	// These are tools with hyphens in their openapiv3_operation field
	type httpTool struct {
		ID                  uuid.UUID
		Name                string
		NewName             string // Pre-calculated new name
		OpenAPIv3Operation  string
		ProjectID           uuid.UUID
		DeploymentID        uuid.UUID
		OpenAPIv3DocumentID *uuid.UUID
		AssetSlug           string
	}

	query := `
		SELECT 
			htd.id,
			htd.name,
			htd.openapiv3_operation,
			htd.project_id,
			htd.deployment_id,
			htd.openapiv3_document_id,
			doa.slug as asset_slug
		FROM gram.http_tool_definitions htd
		JOIN gram.deployments_openapiv3_assets doa ON htd.openapiv3_document_id = doa.id
		WHERE doa.slug LIKE '%-%'
		  AND htd.deleted IS FALSE
		ORDER BY htd.created_at
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query http tool definitions: %w", err)
	}
	defer rows.Close()

	var toolsToUpdate []httpTool
	for rows.Next() {
		var tool httpTool
		if err := rows.Scan(&tool.ID, &tool.Name, &tool.OpenAPIv3Operation, &tool.ProjectID, &tool.DeploymentID, &tool.OpenAPIv3DocumentID, &tool.AssetSlug); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}

		// Calculate what the new name would be with the current (new) sanitization logic
		snakeCasedOp := strcase.ToSnake(tool.OpenAPIv3Operation)
		newName := tools.SanitizeName(fmt.Sprintf("%s_%s", tool.AssetSlug, snakeCasedOp))
		newName = truncateWithHash(newName, 60)

		// Calculate what the old name would have been with the previous logic
		oldSlugWithUnderscores := strings.ReplaceAll(tool.AssetSlug, "-", "_")
		oldName := tools.SanitizeName(fmt.Sprintf("%s_%s", oldSlugWithUnderscores, snakeCasedOp))
		oldName = truncateWithHash(oldName, 60)

		// If the names are different, this tool needs updating
		if oldName != newName && tool.Name == oldName {
			tool.NewName = newName
			toolsToUpdate = append(toolsToUpdate, tool)
		}

		if len(toolsToUpdate) > 50 {
			fmt.Println("toolsToUpdate", len(toolsToUpdate))
			for _, tool := range toolsToUpdate {
				fmt.Println("tool", tool.OpenAPIv3Operation, tool.Name, tool.NewName)
			}
			os.Exit(1)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	if len(toolsToUpdate) == 0 {
		slog.InfoContext(ctx, "no HTTP tool definitions to update")
		return nil
	}

	slog.InfoContext(ctx, "found tools to update", slog.Int("count", len(toolsToUpdate)))

	// Use a transaction for atomic updates
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var updatedTools, updatedToolsets int

	for i, tool := range toolsToUpdate {
		// Use the pre-calculated new name
		newName := tool.NewName

		// Update the tool name
		updateToolQuery := `UPDATE gram.http_tool_definitions SET name = $1, updated_at = now() WHERE id = $2`
		if _, err := tx.Exec(ctx, updateToolQuery, newName, tool.ID); err != nil {
			return fmt.Errorf("update http tool definition %s: %w", tool.ID, err)
		}

		slog.InfoContext(ctx, "updated tool name",
			slog.String("id", tool.ID.String()),
			slog.String("old_name", tool.Name),
			slog.String("new_name", newName))

		// Now update any toolsets that reference this tool by the old name
		updateToolsetQuery := `
			UPDATE gram.toolsets 
			SET http_tool_names = array_replace(http_tool_names, $1, $2),
				updated_at = now()
			WHERE project_id = $3 
			  AND $1 = ANY(http_tool_names)
			  AND deleted IS FALSE
		`
		result, err := tx.Exec(ctx, updateToolsetQuery, tool.Name, newName, tool.ProjectID)
		if err != nil {
			return fmt.Errorf("update toolsets for tool %s: %w", tool.ID, err)
		}

		rowsAffected := result.RowsAffected()
		if rowsAffected > 0 {
			slog.InfoContext(ctx, "updated toolset references",
				slog.String("tool_id", tool.ID.String()),
				slog.String("old_name", tool.Name),
				slog.String("new_name", newName),
				slog.Int64("toolsets_affected", rowsAffected))
			updatedToolsets += int(rowsAffected)
		}

		updatedTools++

		// Progress report every 50 records
		if (i+1)%50 == 0 {
			slog.InfoContext(ctx, "tools progress", slog.Int("completed", i+1), slog.Int("total", len(toolsToUpdate)))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "tool name sanitization backfill completed",
		slog.Int("tools_updated", updatedTools),
		slog.Int("toolsets_updated", updatedToolsets))
	return nil
}

// generateNewToolName replicates the tool naming logic from parseToolDescriptor
// but without the hyphen-to-underscore conversion that was removed
func generateNewToolName(assetSlug, opID string) string {
	// Convert operationId to snake_case using the same library as the original code
	snakeCasedOp := strcase.ToSnake(opID)
	untruncatedName := tools.SanitizeName(fmt.Sprintf("%s_%s", assetSlug, snakeCasedOp))

	// Apply the same 60-character truncation logic as in the original code
	return truncateWithHash(untruncatedName, 60)
}

// truncateWithHash replicates the truncation logic from the original code
func truncateWithHash(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}

	hash := sha256.Sum256([]byte(s))
	hashStr := hex.EncodeToString(hash[:])[:8] // Use first 8 characters of hex hash
	truncateLength := maxLength - len(hashStr)
	if truncateLength < 0 {
		// If maxLength is too small to fit even the hash, just return the hash
		return hashStr
	}

	return s[:truncateLength] + hashStr
}
