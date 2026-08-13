package repo

import (
	"context"
	"fmt"
	"strings"
)

// identityMapInsertChunk bounds one INSERT's bound-parameter count. The map is
// org-directory scale, so most syncs fit in a single chunk.
const identityMapInsertChunk = 5000

// IdentityMapEntry is one row of the employee identity fold map: a normalized
// email and the directory user it unambiguously resolves to within an org.
type IdentityMapEntry struct {
	OrgID           string
	EmailLower      string
	CanonicalUserID string
	CanonicalEmail  string
}

// ReplaceIdentityMap rebuilds identity_map_staging from entries and swaps it
// live with EXCHANGE TABLES, so readers switch between complete map
// generations and never observe a partial rebuild. A full replace rather than
// an incremental update: deletions and unlinks in Postgres propagate by simply
// not being in the next generation.
//
// Entries must be unique per (org, email) key: the ANY Join engine silently
// keeps the first row per key, which would make the fold target insertion-order
// dependent. The rejection below turns a caller violating that contract into a
// failed sync — the previous complete generation stays live — instead of an
// arbitrary owner.
func (q *Queries) ReplaceIdentityMap(ctx context.Context, entries []IdentityMapEntry) error {
	seen := make(map[[2]string]struct{}, len(entries))
	for _, entry := range entries {
		key := [2]string{entry.OrgID, entry.EmailLower}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate identity map key for email %q", entry.EmailLower)
		}
		seen[key] = struct{}{}
	}

	if err := q.conn.Exec(ctx, "TRUNCATE TABLE identity_map_staging"); err != nil {
		return fmt.Errorf("truncate identity_map_staging: %w", err)
	}

	for start := 0; start < len(entries); start += identityMapInsertChunk {
		chunk := entries[start:min(start+identityMapInsertChunk, len(entries))]
		values := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk)*4)
		for _, entry := range chunk {
			values = append(values, "(?, ?, ?, ?)")
			args = append(args, entry.OrgID, entry.EmailLower, entry.CanonicalUserID, entry.CanonicalEmail)
		}
		query := "INSERT INTO identity_map_staging (org_id, email_lower, canonical_user_id, canonical_email) VALUES " + strings.Join(values, ", ")
		if err := q.conn.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("insert identity_map_staging chunk: %w", err)
		}
	}

	if err := q.conn.Exec(ctx, "EXCHANGE TABLES identity_map_staging AND identity_map"); err != nil {
		return fmt.Errorf("exchange identity_map tables: %w", err)
	}

	return nil
}
