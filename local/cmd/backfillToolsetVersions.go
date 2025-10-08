package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func main() {
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

	// Backfill toolset versions
	if err := backfillToolsetVersions(ctx, conn); err != nil {
		slog.ErrorContext(ctx, "failed to backfill toolset versions", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "toolset versions backfill completed successfully")
}

type toolsetData struct {
	ID              uuid.UUID
	Slug            string
	HTTPToolNames   []string
	PromptTemplates []string
	ProjectID       uuid.UUID
}

func backfillToolsetVersions(ctx context.Context, conn *pgx.Conn) error {
	// First, get all toolsets that don't have any versions yet

	// Query to get toolsets without versions and their associated prompt templates
	query := `
		SELECT
			t.id,
			t.slug,
			t.http_tool_names,
			t.project_id,
			COALESCE(array_agg(DISTINCT tp.name) FILTER (WHERE tp.name IS NOT NULL), ARRAY[]::text[]) as prompt_template_names
		FROM gram.toolsets t
		LEFT JOIN gram.toolset_prompts tpt ON t.id = tpt.toolset_id
		LEFT JOIN gram.prompt_templates tp ON tpt.prompt_history_id = tp.history_id
			AND tp.kind = 'higher_order_tool'
		WHERE NOT EXISTS (
			SELECT 1 FROM gram.toolset_versions tv
			WHERE tv.toolset_id = t.id
		  )
		GROUP BY t.id, t.slug, t.http_tool_names, t.project_id
		ORDER BY t.id
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query toolsets: %w", err)
	}
	defer rows.Close()

	var toolsets []toolsetData
	for rows.Next() {
		var toolset toolsetData
		if err := rows.Scan(&toolset.ID, &toolset.Slug, &toolset.HTTPToolNames, &toolset.ProjectID, &toolset.PromptTemplates); err != nil {
			return fmt.Errorf("scan toolset row: %w", err)
		}
		toolsets = append(toolsets, toolset)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate toolset rows: %w", err)
	}

	if len(toolsets) == 0 {
		slog.InfoContext(ctx, "no toolsets without versions found")
		return nil
	}

	slog.InfoContext(ctx, "found toolsets to create versions for", slog.Int("count", len(toolsets)))

	// Use a single transaction for all operations
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	created := 0
	for i, toolset := range toolsets {
		if err := createToolsetVersion(ctx, tx, toolset); err != nil {
			slog.WarnContext(ctx, "failed to create toolset version",
				slog.String("toolset_id", toolset.ID.String()),
				slog.String("toolset_slug", toolset.Slug),
				slog.String("error", err.Error()))
			continue
		}
		created++

		// Progress report every 10 records
		if (i+1)%10 == 0 {
			slog.InfoContext(ctx, "toolset versions progress",
				slog.Int("completed", i+1),
				slog.Int("total", len(toolsets)))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "toolset versions backfill completed", slog.Int("created", created))
	return nil
}

func createToolsetVersion(ctx context.Context, tx pgx.Tx, toolset toolsetData) error {
	allToolURNs := []urn.Tool{}

	// Get HTTP tool URNs if there are HTTP tool names
	if len(toolset.HTTPToolNames) > 0 {
		httpToolURNQuery := `
			WITH latest_deployment AS (
				SELECT d.id
				FROM gram.deployments d
				JOIN gram.deployment_statuses ds ON d.id = ds.deployment_id
				WHERE d.project_id = $2
				  AND ds.status = 'completed'
				ORDER BY d.seq DESC
				LIMIT 1
			)
			SELECT DISTINCT
				htd.tool_urn
			FROM gram.http_tool_definitions htd
			WHERE htd.name = ANY($1)
			  AND htd.project_id = $2
			  AND htd.deployment_id = (SELECT id FROM latest_deployment)
			  AND htd.deleted IS FALSE
			  AND htd.tool_urn IS NOT NULL
		`

		rows, err := tx.Query(ctx, httpToolURNQuery, toolset.HTTPToolNames, toolset.ProjectID)
		if err != nil {
			return fmt.Errorf("query HTTP tool URNs: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var toolURN urn.Tool
			if err := rows.Scan(&toolURN); err != nil {
				return fmt.Errorf("scan HTTP tool URN: %w", err)
			}

			allToolURNs = append(allToolURNs, toolURN)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate HTTP tool URN rows: %w", err)
		}
	}

	// Get prompt template URNs if there are prompt templates with kind=higher-order-tool
	if len(toolset.PromptTemplates) > 0 {
		promptTemplateURNQuery := `
			SELECT DISTINCT
				pt.tool_urn
			FROM gram.prompt_templates pt
			WHERE pt.name = ANY($1)
			  AND pt.project_id = $2
			  AND pt.kind = 'higher_order_tool'
			  AND pt.deleted IS FALSE
			  AND pt.tool_urn IS NOT NULL
		`

		rows, err := tx.Query(ctx, promptTemplateURNQuery, toolset.PromptTemplates, toolset.ProjectID)
		if err != nil {
			return fmt.Errorf("query prompt template URNs: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var toolURN urn.Tool
			if err := rows.Scan(&toolURN); err != nil {
				return fmt.Errorf("scan prompt template URN: %w", err)
			}

			allToolURNs = append(allToolURNs, toolURN)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate prompt template URN rows: %w", err)
		}
	}

	// Convert URNs to strings for database storage
	urnStrings := make([]string, len(allToolURNs))
	for i, u := range allToolURNs {
		urnStrings[i] = u.String()
	}

	// Create the toolset version
	insertQuery := `
		INSERT INTO gram.toolset_versions (
			toolset_id,
			version,
			tool_urns,
			predecessor_id
		) VALUES ($1, $2, $3, $4)
		RETURNING id
	`

	var versionID uuid.UUID
	err := tx.QueryRow(ctx, insertQuery,
		toolset.ID,
		int64(1), // First version
		urnStrings,
		uuid.NullUUID{Valid: false}, // No predecessor for first version
	).Scan(&versionID)

	if err != nil {
		return fmt.Errorf("create toolset version: %w", err)
	}

	slog.InfoContext(ctx, "created toolset version",
		slog.String("toolset_id", toolset.ID.String()),
		slog.String("toolset_slug", toolset.Slug),
		slog.String("version_id", versionID.String()),
		slog.Int("tool_count", len(allToolURNs)))

	return nil
}
