package repo

// Usage importers stamp a stable fingerprint on every row they derive from a
// provider feed (codex.compliance.event_hash, claude_chat.event_hash,
// cursor.event_hash). telemetry_logs is a plain MergeTree with no uniqueness
// constraint, so the fingerprint only prevents double counting if a writer
// consults it before inserting — that is what ListIngestedEventHashes is for.

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// eventHashAttributePattern bounds the attribute names ListIngestedEventHashes
// will read. ClickHouse has no placeholder for a column path, so the name is
// concatenated into the query text; anything outside dotted lowercase
// identifiers is rejected rather than escaped.
var eventHashAttributePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ingestedEventHashChunk caps how many fingerprints go into one IN list.
// clickhouse-go renders bound arguments into the query text, and a 64-character
// hash per entry would otherwise push a large page past ClickHouse's 256KB
// max_query_size.
const ingestedEventHashChunk = 2000

// ListIngestedEventHashesParams scopes a fingerprint lookup to one project's
// rows from one set of importers over one time range, so the read stays on the
// telemetry_logs primary index instead of scanning the project's whole history.
type ListIngestedEventHashesParams struct {
	// ProjectID is the Gram project whose rows are searched. Fingerprints are
	// only unique within a project, since two projects can legitimately import
	// the same provider event.
	ProjectID string

	// URNs restricts the search to the gram_urn values the calling importer
	// writes. Required: it is what lets the gram_urn bloom filter skip the
	// granules belonging to every other telemetry source in the window, which
	// is where most of the pruning comes from.
	URNs []string

	// HashAttribute is the dotted attribute name carrying the fingerprint, as
	// stamped on the row (for example "codex.compliance.event_hash").
	HashAttribute string

	// Hashes are the candidate fingerprints to look for. Only these are
	// returned, which keeps the result proportional to the caller's batch
	// rather than to everything ingested in the window — the difference
	// between returning a few hundred rows and a few million on a high-volume
	// project.
	Hashes []string

	// MinTimeUnixNano bounds the search below, in event time.
	MinTimeUnixNano int64

	// MaxTimeUnixNano bounds the search above, in event time.
	MaxTimeUnixNano int64
}

// ListIngestedEventHashes returns the subset of params.Hashes already present
// in telemetry_logs for the given scope. Callers drop their matching rows.
//
// The read demands sequential consistency so that on replicated setups a retry
// cannot miss an insert acknowledged by another replica moments earlier
// (harmless no-op on a single plain-MergeTree node).
//
// Fingerprints age out with the rows carrying them: telemetry_logs has a
// 90-day TTL, so re-importing a window older than that will not be recognized
// as a duplicate.
func (q *Queries) ListIngestedEventHashes(ctx context.Context, params ListIngestedEventHashesParams) (map[string]struct{}, error) {
	if len(params.URNs) == 0 || len(params.Hashes) == 0 {
		return map[string]struct{}{}, nil
	}
	if !eventHashAttributePattern.MatchString(params.HashAttribute) {
		return nil, fmt.Errorf("unsupported event hash attribute: %q", params.HashAttribute)
	}
	hashPath := fmt.Sprintf("toString(attributes.%s)", params.HashAttribute)

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"select_sequential_consistency": 1,
	}))

	result := make(map[string]struct{})
	for chunk := range slices.Chunk(params.Hashes, ingestedEventHashChunk) {
		sb := sq.Select(hashPath+" AS event_hash").
			From("telemetry_logs").
			Where(squirrel.Eq{"gram_project_id": params.ProjectID}).
			Where("time_unix_nano >= ?", params.MinTimeUnixNano).
			Where("time_unix_nano <= ?", params.MaxTimeUnixNano).
			Where(squirrel.Eq{"gram_urn": params.URNs}).
			Where(squirrel.Eq{hashPath: chunk})

		query, args, err := sb.ToSql()
		if err != nil {
			return nil, fmt.Errorf("building ingested event hashes query: %w", err)
		}

		if err := q.collectIngestedEventHashes(ctx, query, args, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (q *Queries) collectIngestedEventHashes(ctx context.Context, query string, args []any, into map[string]struct{}) error {
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("querying ingested event hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return fmt.Errorf("scanning ingested event hash: %w", err)
		}
		into[hash] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating ingested event hashes: %w", err)
	}
	return nil
}
