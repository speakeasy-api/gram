package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const batchSize = 10000

type InvalidTool struct {
	ID       uuid.UUID
	Name     string
	Project  string
	ToolURN  string
	Error    string
}

func main6() {
	ctx := context.Background()

	// Get database connection string from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.ErrorContext(ctx, "DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// Connect to database with connection pool
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.ErrorContext(ctx, "failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	slog.InfoContext(ctx, "connected to database successfully")

	// Scan HTTP tool definitions and collect invalid tools
	invalidTools, err := scanHTTPToolDefinitions(ctx, pool)
	if err != nil {
		slog.ErrorContext(ctx, "failed to scan http_tool_definitions", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "invalid tools", slog.Any("invalid_tools", len(invalidTools)))

	// if len(invalidTools) > 0 {
	// 	slog.InfoContext(ctx, "found invalid tools, fixing them", slog.Int("invalid_count", len(invalidTools)))

	// 	// Fix invalid tools by sanitizing names and updating URNs
	// 	if err := fixInvalidTools(ctx, pool, invalidTools); err != nil {
	// 		slog.ErrorContext(ctx, "failed to fix invalid tools", slog.String("error", err.Error()))
	// 		os.Exit(1)
	// 	}
	// }

	// // Scan prompt templates
	// if err := scanPromptTemplates(ctx, conn); err != nil {
	// 	slog.ErrorContext(ctx, "failed to scan prompt_templates", slog.String("error", err.Error()))
	// 	os.Exit(1)
	// }

	slog.InfoContext(ctx, "scan completed successfully")
}

func scanHTTPToolDefinitions(ctx context.Context, pool *pgxpool.Pool) ([]InvalidTool, error) {
	slog.InfoContext(ctx, "scanning http_tool_definitions for invalid URNs")

	// Get total count first
	var totalCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM gram.http_tool_definitions 
		WHERE tool_urn IS NOT NULL AND deleted IS FALSE
	`).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("get total count: %w", err)
	}

	slog.InfoContext(ctx, "starting HTTP tool definitions scan", slog.Int("total_tools", totalCount))

	var offset int
	var totalInvalid int
	var processedCount int
	var invalidTools []InvalidTool

	for {
		query := `
			SELECT htd.id, htd.tool_urn, htd.name, p.slug
			FROM gram.http_tool_definitions htd
			JOIN gram.projects p ON htd.project_id = p.id
			WHERE htd.tool_urn IS NOT NULL AND htd.deleted IS FALSE
			ORDER BY htd.created_at
			LIMIT $1 OFFSET $2
		`

		rows, err := pool.Query(ctx, query, batchSize, offset)
		if err != nil {
			return nil, fmt.Errorf("query http tool definitions: %w", err)
		}

		var batchCount int
		var batchInvalid int

		for rows.Next() {
			var id uuid.UUID
			var toolURNStr string
			var name string
			var projectSlug string

			if err := rows.Scan(&id, &toolURNStr, &name, &projectSlug); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan row: %w", err)
			}

			batchCount++
			processedCount++


			sanitizedName := tools.SanitizeName(name)

			// Try to parse the URN - this will trigger validation
			var toolURN urn.Tool
			if err := toolURN.Scan(toolURNStr); err != nil || sanitizedName != name {

				if sanitizedName != name {
					err = fmt.Errorf("name has not been sanitized: %s -> %s", name, sanitizedName)
				}

				batchInvalid++
				totalInvalid++
				invalidTools = append(invalidTools, InvalidTool{
					ID:      id,
					Name:    name,
					Project: projectSlug,
					ToolURN: toolURNStr,
					Error:   err.Error(),
				})
				slog.ErrorContext(ctx, "invalid HTTP tool URN found",
					slog.String("id", id.String()),
					slog.String("name", name),
					slog.String("project", projectSlug),
					slog.String("tool_urn", toolURNStr),
					slog.String("validation_error", err.Error()),
				)
			}
		}

		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate rows: %w", err)
		}

		// Progress logging
		if processedCount > 0 && processedCount%10000 == 0 {
			slog.InfoContext(ctx, "HTTP tools progress",
				slog.Int("processed", processedCount),
				slog.Int("total", totalCount),
				slog.Int("invalid_found", totalInvalid),
			)
		}

		// If we got fewer rows than batch size, we're done
		if batchCount < batchSize {
			break
		}

		offset += batchSize
	}

	slog.InfoContext(ctx, "HTTP tool definitions scan completed",
		slog.Int("total_processed", processedCount),
		slog.Int("invalid_urns", totalInvalid),
	)

	return invalidTools, nil
}

func fixInvalidTools(ctx context.Context, pool *pgxpool.Pool, invalidTools []InvalidTool) error {
	slog.InfoContext(ctx, "fixing invalid tools by sanitizing names and updating URNs")

	// Begin transaction for atomicity
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var fixedCount int
	toolNameUpdates := make(map[string]map[string]string) // project -> old_name -> new_name

	for _, tool := range invalidTools {
		// Sanitize the tool name
		sanitizedName := tools.SanitizeName(tool.Name)
		if sanitizedName == tool.Name {
			// Name didn't change after sanitization - this shouldn't happen with invalid URNs
			slog.WarnContext(ctx, "tool name didn't change after sanitization despite invalid URN",
				slog.String("tool_id", tool.ID.String()),
				slog.String("name", tool.Name),
				slog.String("project", tool.Project),
				slog.String("original_urn", tool.ToolURN),
				slog.String("error", tool.Error),
			)
			continue
		}

		// Create new URN with sanitized name and project
		newURN := urn.NewTool(urn.ToolKindHTTP, tool.Project, sanitizedName)

		// Update the HTTP tool definition
		updateQuery := `
			UPDATE gram.http_tool_definitions 
			SET name = $1, tool_urn = $2, updated_at = now()
			WHERE id = $3
		`

		if _, err := tx.Exec(ctx, updateQuery, sanitizedName, newURN.String(), tool.ID); err != nil {
			return fmt.Errorf("update http_tool_definition %s: %w", tool.ID.String(), err)
		}

		// Track name changes for toolset updates
		if toolNameUpdates[tool.Project] == nil {
			toolNameUpdates[tool.Project] = make(map[string]string)
		}
		toolNameUpdates[tool.Project][tool.Name] = sanitizedName

		fixedCount++
		slog.InfoContext(ctx, "fixed invalid tool",
			slog.String("tool_id", tool.ID.String()),
			slog.String("project", tool.Project),
			slog.String("old_name", tool.Name),
			slog.String("new_name", sanitizedName),
			slog.String("old_urn", tool.ToolURN),
			slog.String("new_urn", newURN.String()),
		)
	}

	// Update toolsets that reference the old tool names (within the same transaction)
	var toolsetUpdates int
	for project, nameChanges := range toolNameUpdates {
		updated, err := updateToolsetsWithNewNames(ctx, tx, project, nameChanges)
		if err != nil {
			return fmt.Errorf("update toolsets for project %s: %w", project, err)
		}
		toolsetUpdates += updated
	}

	// Commit transaction only after both tool and toolset updates succeed
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "invalid tools fix completed",
		slog.Int("tools_fixed", fixedCount),
		slog.Int("toolsets_updated", toolsetUpdates),
	)

	return nil
}

func updateToolsetsWithNewNames(ctx context.Context, tx pgx.Tx, project string, nameChanges map[string]string) (int, error) {
	// Get all toolsets for this project first, then filter in Go to avoid complex array queries
	query := `
		SELECT ts.id, ts.name, ts.slug, ts.http_tool_names
		FROM gram.toolsets ts
		JOIN gram.projects p ON ts.project_id = p.id
		WHERE p.slug = $1 AND ts.deleted IS FALSE
	`

	rows, err := tx.Query(ctx, query, project)
	if err != nil {
		return 0, fmt.Errorf("query toolsets for project %s: %w", project, err)
	}
	defer rows.Close()

	// Collect toolsets that need updates
	type toolsetUpdate struct {
		id           uuid.UUID
		name         string
		slug         string
		updatedNames []string
		originalNames []string
	}
	
	var toolsetsToUpdate []toolsetUpdate

	for rows.Next() {
		var toolsetID uuid.UUID
		var toolsetName, toolsetSlug string
		var httpToolNames []string

		if err := rows.Scan(&toolsetID, &toolsetName, &toolsetSlug, &httpToolNames); err != nil {
			return 0, fmt.Errorf("scan toolset row: %w", err)
		}

		// Check if this toolset references any of the old names
		var updatedNames []string
		var hasChanges bool
		for _, toolName := range httpToolNames {
			if newName, exists := nameChanges[toolName]; exists {
				updatedNames = append(updatedNames, newName)
				hasChanges = true
			} else {
				updatedNames = append(updatedNames, toolName)
			}
		}

		if hasChanges {
			toolsetsToUpdate = append(toolsetsToUpdate, toolsetUpdate{
				id:           toolsetID,
				name:         toolsetName,
				slug:         toolsetSlug,
				updatedNames: updatedNames,
				originalNames: httpToolNames,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate toolset rows: %w", err)
	}

	// Now update all the toolsets after closing the result set
	for _, update := range toolsetsToUpdate {
		updateQuery := `
			UPDATE gram.toolsets 
			SET http_tool_names = $1, updated_at = now()
			WHERE id = $2
		`

		if _, err := tx.Exec(ctx, updateQuery, update.updatedNames, update.id); err != nil {
			return 0, fmt.Errorf("update toolset %s: %w", update.id.String(), err)
		}

		slog.InfoContext(ctx, "updated toolset with new tool names",
			slog.String("toolset_id", update.id.String()),
			slog.String("toolset_name", update.name),
			slog.String("toolset_slug", update.slug),
			slog.String("project", project),
			slog.Any("old_names", update.originalNames),
			slog.Any("new_names", update.updatedNames),
		)
	}

	return len(toolsetsToUpdate), nil
}
