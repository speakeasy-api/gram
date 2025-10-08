package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func main2() {
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

	// Backfill http_tool_definitions
	if err := backfillHTTPToolDefinitions(ctx, conn); err != nil {
		slog.ErrorContext(ctx, "failed to backfill http_tool_definitions", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Backfill prompt_templates
	if err := backfillPromptTemplates(ctx, conn); err != nil {
		slog.ErrorContext(ctx, "failed to backfill prompt_templates", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "backfill completed successfully")
}

func backfillHTTPToolDefinitions(ctx context.Context, conn *pgx.Conn) error {

	// First, collect all the records that need updating
	type httpTool struct {
		ID        uuid.UUID
		Name      string
		AssetSlug string
	}

	query := `
		SELECT 
			htd.id,
			htd.name,
			doa.slug as asset_slug
		FROM http_tool_definitions htd
		JOIN deployments_openapiv3_assets doa ON htd.openapiv3_document_id = doa.id
		WHERE htd.tool_urn IS NULL 
		  AND htd.deleted IS FALSE
		ORDER BY htd.created_at
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query http tool definitions: %w", err)
	}
	defer rows.Close()

	var tools []httpTool
	for rows.Next() {
		var tool httpTool
		if err := rows.Scan(&tool.ID, &tool.Name, &tool.AssetSlug); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		tools = append(tools, tool)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	if len(tools) == 0 {
		slog.InfoContext(ctx, "no HTTP tool definitions to update")
		return nil
	}

	// Use a single transaction for batch updates
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	updateQuery := `UPDATE http_tool_definitions SET tool_urn = $1 WHERE id = $2`
	var updated int
	for i, tool := range tools {
		// Generate the URN: tools:http:{asset_slug}:{tool_name}
		toolURN := urn.NewTool(urn.ToolKindHTTP, tool.AssetSlug, tool.Name)

		if _, err := tx.Exec(ctx, updateQuery, toolURN.String(), tool.ID); err != nil {
			return fmt.Errorf("update http tool definition %s: %w", tool.ID, err)
		}
		updated++

		// Progress report every 100 records
		if (i+1)%100 == 0 {
			slog.InfoContext(ctx, "HTTP tools progress", slog.Int("completed", i+1), slog.Int("total", len(tools)))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "HTTP tool definitions backfill completed", slog.Int("updated", updated))
	return nil
}

func backfillPromptTemplates(ctx context.Context, conn *pgx.Conn) error {

	// First, collect all the records that need updating
	type promptTemplate struct {
		ID   uuid.UUID
		Name string
		Kind sql.NullString
	}

	query := `
		SELECT 
			id,
			name,
			kind
		FROM prompt_templates
		WHERE tool_urn IS NULL 
		ORDER BY created_at
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query prompt templates: %w", err)
	}
	defer rows.Close()

	var templates []promptTemplate
	for rows.Next() {
		var tmpl promptTemplate
		if err := rows.Scan(&tmpl.ID, &tmpl.Name, &tmpl.Kind); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}

		// Skip if kind is null
		if !tmpl.Kind.Valid || tmpl.Kind.String == "" {
			slog.WarnContext(ctx, "skipping prompt template with null/empty kind",
				slog.String("id", tmpl.ID.String()),
				slog.String("name", tmpl.Name))
			continue
		}

		templates = append(templates, tmpl)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	if len(templates) == 0 {
		slog.InfoContext(ctx, "no prompt templates to update")
		return nil
	}

	// Use a single transaction for batch updates
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	updateQuery := `UPDATE prompt_templates SET tool_urn = $1 WHERE id = $2`
	var updated int
	for i, tmpl := range templates {
		// Generate the URN: tools:prompt:{kind}:{name}
		toolURN := urn.NewTool(urn.ToolKindPrompt, tmpl.Kind.String, tmpl.Name)

		if _, err := tx.Exec(ctx, updateQuery, toolURN.String(), tmpl.ID); err != nil {
			return fmt.Errorf("update prompt template %s: %w", tmpl.ID, err)
		}
		updated++

		// Progress report every 100 records
		if (i+1)%100 == 0 {
			slog.InfoContext(ctx, "Prompt templates progress", slog.Int("completed", i+1), slog.Int("total", len(templates)))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "prompt templates backfill completed", slog.Int("updated", updated))
	return nil
}
