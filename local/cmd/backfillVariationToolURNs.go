package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	// Backfill tool_variations
	if err := backfillToolVariations(ctx, conn); err != nil {
		slog.ErrorContext(ctx, "failed to backfill tool_variations", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "backfill completed successfully")
}

func backfillToolVariations(ctx context.Context, conn *pgx.Conn) error {
	// First, collect all the variations that need updating
	type toolVariation struct {
		ID          uuid.UUID
		SrcToolName string
	}

	query := `
		SELECT
			id,
			src_tool_name
		FROM tool_variations
		WHERE src_tool_urn IS NULL
		ORDER BY created_at
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("query tool variations: %w", err)
	}
	defer rows.Close()

	var variations []toolVariation
	for rows.Next() {
		var v toolVariation
		if err := rows.Scan(&v.ID, &v.SrcToolName); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		variations = append(variations, v)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	if len(variations) == 0 {
		slog.InfoContext(ctx, "no tool variations to update")
		return nil
	}

	slog.InfoContext(ctx, "found tool variations to backfill", slog.Int("count", len(variations)))

	// Use a single transaction for batch updates
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	updateQuery := `UPDATE tool_variations SET src_tool_urn = $1 WHERE id = $2`
	var updated, skipped int

	for i, v := range variations {
		// Look up the tool_urn from the latest deployment's HTTP tool
		lookupQuery := `
			SELECT htd.tool_urn
			FROM http_tool_definitions htd
			JOIN deployments d ON htd.deployment_id = d.id
			WHERE htd.name = $1
			  AND htd.deleted IS FALSE
			  AND d.deleted IS FALSE
			ORDER BY d.created_at DESC
			LIMIT 1
		`

		var toolURN string
		err := tx.QueryRow(ctx, lookupQuery, v.SrcToolName).Scan(&toolURN)
		if err != nil {
			if err == pgx.ErrNoRows {
				slog.WarnContext(ctx, "no HTTP tool found for variation",
					slog.String("variation_id", v.ID.String()),
					slog.String("src_tool_name", v.SrcToolName))
				skipped++
				continue
			}
			return fmt.Errorf("lookup tool urn for %s: %w", v.SrcToolName, err)
		}

		if _, err := tx.Exec(ctx, updateQuery, toolURN, v.ID); err != nil {
			return fmt.Errorf("update tool variation %s: %w", v.ID, err)
		}
		updated++

		// Progress report every 100 records
		if (i+1)%100 == 0 {
			slog.InfoContext(ctx, "tool variations progress",
				slog.Int("completed", i+1),
				slog.Int("total", len(variations)))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	slog.InfoContext(ctx, "tool variations backfill completed",
		slog.Int("updated", updated),
		slog.Int("skipped", skipped))
	return nil
}
