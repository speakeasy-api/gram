package categories

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BootstrapConnection creates a session-scoped TEMP TABLE
// `risk_category_lookup` on the connection and populates it from the
// canonical Definitions slice.
//
// Wire this into pgxpool.Config.AfterConnect so every connection in the pool
// owns one classifier table for its lifetime. Subsequent SQL queries that
// need to classify risk_results join against the temp table by name; the Go
// classifier (Definitions) is the single source of truth, the SQL CASE
// expressions that used to live in queries.sql are gone.
//
// Schema:
//
//	priority    INT      — evaluation order; first match in ORDER BY priority ASC wins
//	category    TEXT     — bucket the finding rolls up to (e.g. 'secrets')
//	source      TEXT     — exact source match (e.g. 'shadow_mcp', 'gitleaks')
//	rule_id     TEXT     — exact rule_id match (e.g. 'pii.credit_card')
//	rule_prefix TEXT     — LIKE-prefix match (e.g. 'secret.')
//
// Each row populates one of source / rule_id / rule_prefix; the others are
// NULL. Findings that match none of the rows are treated as `custom` via a
// COALESCE in the consuming queries.
func BootstrapConnection(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS risk_category_lookup (
			priority    INTEGER NOT NULL,
			category    TEXT    NOT NULL,
			source      TEXT,
			rule_id     TEXT,
			rule_prefix TEXT
		) ON COMMIT PRESERVE ROWS;
		TRUNCATE risk_category_lookup;
	`); err != nil {
		return fmt.Errorf("create risk_category_lookup temp table: %w", err)
	}

	// Build a VALUES insert from the canonical classifier so SQL never has
	// the mapping baked in.
	var (
		args []any
		rows []string
		idx  = 1
		prio = 0
		emit = func(category, source, ruleID, prefix string) {
			placeholders := fmt.Sprintf("($%d::int, $%d::text, NULLIF($%d::text, ''), NULLIF($%d::text, ''), NULLIF($%d::text, ''))",
				idx, idx+1, idx+2, idx+3, idx+4)
			rows = append(rows, placeholders)
			args = append(args, prio, category, source, ruleID, prefix)
			idx += 5
			prio++
		}
	)

	for _, def := range Definitions {
		switch {
		case def.Source != "":
			emit(string(def.Category), def.Source, "", "")
		case len(def.RuleIDs) > 0:
			for _, ruleID := range def.RuleIDs {
				emit(string(def.Category), "", ruleID, "")
			}
		case def.RulePrefix != "":
			emit(string(def.Category), "", "", def.RulePrefix)
		}
	}

	if len(rows) == 0 {
		return nil
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO risk_category_lookup (priority, category, source, rule_id, rule_prefix) VALUES %s",
		strings.Join(rows, ", "),
	)
	if _, err := conn.Exec(ctx, insertSQL, args...); err != nil {
		return fmt.Errorf("populate risk_category_lookup: %w", err)
	}
	return nil
}

// ClassifySubquery returns the SQL fragment that resolves a risk_results row
// to its category by joining against risk_category_lookup. Embed inside a
// SELECT or use as a LATERAL join source.
//
// Returns 'custom' for unmatched rows so callers don't have to repeat that.
const ClassifySubquery = `(
	SELECT COALESCE(
		(
			SELECT rcl.category
			FROM risk_category_lookup rcl
			WHERE (rcl.source IS NOT NULL AND rcl.source = rr.source)
			   OR (rcl.rule_id IS NOT NULL AND rcl.rule_id = rr.rule_id)
			   OR (rcl.rule_prefix IS NOT NULL AND rr.rule_id LIKE rcl.rule_prefix || '%')
			ORDER BY rcl.priority ASC
			LIMIT 1
		),
		'custom'
	)
)`
