package telemetry

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// eventScope is the telemetry_logs slice a batch's fingerprints can collide
// with: one project, the URNs the batch writes, the fingerprints it carries,
// and its own event-time range.
type eventScope struct {
	urns    map[string]struct{}
	hashes  map[string]struct{}
	minNano int64
	maxNano int64
}

// LogBulkDeduped writes params to telemetry_logs after dropping every row whose
// event fingerprint is already there, and every repeat within the batch itself.
// It returns how many rows it wrote and how many it dropped as duplicates.
// Those two do not have to sum to len(params): rows belonging to an
// organization with telemetry logs disabled are discarded by the write.
//
// This is the enforcement half of the fingerprints usage importers stamp on
// rows they derive from a provider feed — hashKey being one of
// codex.compliance.event_hash, claude_chat.event_hash, cursor.event_hash.
// telemetry_logs is a plain MergeTree with no uniqueness constraint, so an
// importer that reads the same provider event twice — because the feed repeated
// it across log files, or because a failed sync left the watermark unadvanced —
// otherwise inserts it twice and doubles every downstream token and cost sum.
//
// The write is synchronous, unlike LogBulk: the next call's lookup has to see
// these rows, and the async insert buffer would hide them.
//
// The guarantee is against SEQUENTIAL re-reads, which is the failure that
// happens in practice. Two callers writing the same batch concurrently can
// still both insert, because each reads before the other has written; callers
// that can overlap themselves — a Temporal activity whose heartbeat times out
// while an attempt is still in flight, say — need their own serialization.
//
// Repeats are dropped rather than replaced, so a feed that restates a value
// under a fingerprint already ingested keeps the original. That is the right
// choice for pure re-emissions and the wrong one for restatements; only the
// former exists on the paths calling this today.
//
// Rows carrying no fingerprint under hashKey are written as-is, so a mixed
// batch is safe.
func (l *Logger) LogBulkDeduped(ctx context.Context, hashKey attr.Key, params []LogParams) (written int, dropped int, err error) {
	kept, err := l.dropIngestedDuplicates(ctx, hashKey, params)
	if err != nil {
		return 0, 0, err
	}

	dropped = len(params) - len(kept)
	if len(kept) == 0 {
		return 0, dropped, nil
	}
	written, err = l.logBulk(ctx, l.shutdownCtx(), kept, true)
	if err != nil {
		return written, dropped, err
	}
	return written, dropped, nil
}

// dropIngestedDuplicates returns params minus the rows already represented in
// telemetry_logs and minus in-batch repeats, preserving order and keeping the
// first occurrence of each fingerprint.
func (l *Logger) dropIngestedDuplicates(ctx context.Context, hashKey attr.Key, params []LogParams) ([]LogParams, error) {
	if l.chConn == nil || len(params) == 0 {
		return params, nil
	}

	hashes := make([]string, len(params))
	scopes := make(map[string]*eventScope)
	for i, param := range params {
		hash, _ := param.Attributes[hashKey].(string)
		hashes[i] = hash
		if hash == "" {
			continue
		}

		nano := param.Timestamp.UnixNano()
		scope, ok := scopes[param.ToolInfo.ProjectID]
		if !ok {
			scope = &eventScope{
				urns:    make(map[string]struct{}),
				hashes:  make(map[string]struct{}),
				minNano: nano,
				maxNano: nano,
			}
			scopes[param.ToolInfo.ProjectID] = scope
		}
		scope.urns[param.ToolInfo.URN] = struct{}{}
		scope.hashes[hash] = struct{}{}
		scope.minNano = min(scope.minNano, nano)
		scope.maxNano = max(scope.maxNano, nano)
	}
	if len(scopes) == 0 {
		return params, nil
	}

	queries := repo.New(l.chConn)
	ingested := make(map[string]map[string]struct{}, len(scopes))
	for projectID, scope := range scopes {
		seen, err := queries.ListIngestedEventHashes(ctx, repo.ListIngestedEventHashesParams{
			ProjectID:       projectID,
			URNs:            slices.Collect(maps.Keys(scope.urns)),
			HashAttribute:   string(hashKey),
			Hashes:          slices.Collect(maps.Keys(scope.hashes)),
			MinTimeUnixNano: scope.minNano,
			MaxTimeUnixNano: scope.maxNano,
		})
		if err != nil {
			return nil, fmt.Errorf("list ingested event hashes: %w", err)
		}
		ingested[projectID] = seen
	}

	type batchKey struct {
		projectID string
		hash      string
	}
	kept := make([]LogParams, 0, len(params))
	inBatch := make(map[batchKey]struct{}, len(params))
	for i, param := range params {
		hash := hashes[i]
		if hash == "" {
			kept = append(kept, param)
			continue
		}

		key := batchKey{projectID: param.ToolInfo.ProjectID, hash: hash}
		if _, dup := inBatch[key]; dup {
			continue
		}
		if _, dup := ingested[key.projectID][hash]; dup {
			continue
		}
		inBatch[key] = struct{}{}
		kept = append(kept, param)
	}
	return kept, nil
}
