package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	mainFixToolsets()
}

func mainFixToolsets() {
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

	// Parse the fixed tool log file to extract name changes
	nameChanges, err := parseFixedToolLog("fixed_tool.txt")
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse fixed tool log", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "parsed tool name changes", slog.Int("projects", len(nameChanges)))

	// Update toolsets for each project
	var totalUpdates int
	for project, changes := range nameChanges {
		updates := updateToolsetsWithNewNamesFixed(ctx, pool, project, changes)
		totalUpdates += updates
		slog.InfoContext(ctx, "updated toolsets for project",
			slog.String("project", project),
			slog.Int("toolsets_updated", updates),
		)
	}

	slog.InfoContext(ctx, "toolset reference fix completed",
		slog.Int("total_toolsets_updated", totalUpdates),
	)
}

func parseFixedToolLog(filename string) (map[string]map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Regex to parse log lines like:
	// 2025/09/25 11:48:25 INFO fixed invalid tool tool_id=xxx project=default old_name="trello_get_members=id" new_name=trello_get_members_id old_urn=xxx new_urn=xxx
	logRegex := regexp.MustCompile(`project=(\S+)\s+old_name="?([^"\s]+)"?\s+new_name=(\S+)`)

	nameChanges := make(map[string]map[string]string) // project -> old_name -> new_name

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		
		// Skip non-log lines
		if !strings.Contains(line, "fixed invalid tool") {
			continue
		}

		matches := logRegex.FindStringSubmatch(line)
		if len(matches) != 4 {
			continue
		}

		project := matches[1]
		oldName := matches[2]
		newName := matches[3]

		// Initialize project map if needed
		if nameChanges[project] == nil {
			nameChanges[project] = make(map[string]string)
		}

		// Only add if names are actually different
		if oldName != newName {
			nameChanges[project][oldName] = newName
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}

	return nameChanges, nil
}

func updateToolsetsWithNewNamesFixed(ctx context.Context, pool *pgxpool.Pool, project string, nameChanges map[string]string) int {
	// Find toolsets in this project that reference any of the old names
	oldNames := make([]string, 0, len(nameChanges))
	for oldName := range nameChanges {
		oldNames = append(oldNames, oldName)
	}

	if len(oldNames) == 0 {
		return 0
	}

	query := `
		SELECT ts.id, ts.name, ts.slug, ts.http_tool_names
		FROM gram.toolsets ts
		JOIN gram.projects p ON ts.project_id = p.id
		WHERE p.slug = $1 
		  AND ts.deleted IS FALSE
		  AND ts.http_tool_names && $2
	`

	rows, err := pool.Query(ctx, query, project, oldNames)
	if err != nil {
		slog.ErrorContext(ctx, "failed to query toolsets for name updates",
			slog.String("project", project),
			slog.String("error", err.Error()),
		)
		return 0
	}
	defer rows.Close()

	var updatedCount int

	for rows.Next() {
		var toolsetID uuid.UUID
		var toolsetName, toolsetSlug string
		var httpToolNames []string

		if err := rows.Scan(&toolsetID, &toolsetName, &toolsetSlug, &httpToolNames); err != nil {
			slog.ErrorContext(ctx, "failed to scan toolset row",
				slog.String("error", err.Error()),
			)
			continue
		}

		// Update tool names in the array
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
			// Update the toolset with new tool names
			updateQuery := `
				UPDATE gram.toolsets 
				SET http_tool_names = $1, updated_at = now()
				WHERE id = $2
			`

			if _, err := pool.Exec(ctx, updateQuery, updatedNames, toolsetID); err != nil {
				slog.ErrorContext(ctx, "failed to update toolset",
					slog.String("toolset_id", toolsetID.String()),
					slog.String("error", err.Error()),
				)
				continue
			}

			updatedCount++
			slog.InfoContext(ctx, "updated toolset with new tool names",
				slog.String("toolset_id", toolsetID.String()),
				slog.String("toolset_name", toolsetName),
				slog.String("toolset_slug", toolsetSlug),
				slog.String("project", project),
				slog.Any("old_names", httpToolNames),
				slog.Any("new_names", updatedNames),
			)
		}
	}

	return updatedCount
}