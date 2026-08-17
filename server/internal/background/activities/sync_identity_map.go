package activities

// SyncIdentityMap rebuilds the ClickHouse employee identity fold map
// (identity_map) from the Postgres directory. The source query encodes the
// fold rules — a directory email maps to its user only when exactly one
// connected user claims it, a linked-account email only when it has no
// directory row and exactly one owner — so ambiguous identities are absent
// and analytics readers fall back to literal email matching. Every pass is a
// full refresh into a staging table swapped live with EXCHANGE TABLES:
// deletions and unlinks propagate by omission, so a stalled sync means the
// map drifts stale (it keeps folding by old links) rather than failing open.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// identityMapReplaceLockKey serializes the whole staging rebuild + swap. The
// TRUNCATE/INSERT/EXCHANGE sequence is not atomic in ClickHouse, so without a
// single writer a retry can interleave with a timed-out predecessor's in-flight
// statements and publish mixed data — or a delayed EXCHANGE can republish the
// previous generation after a successor already swapped.
const identityMapReplaceLockKey = "identity-map:replace-lock"

// identityMapReplaceLockTTL outlives one activity attempt (2m StartToClose)
// plus the longest a lapsed holder's statement can still land afterwards:
// SELECT pipelines are bounded by the connection's 60s max_execution_time,
// while the TRUNCATE/EXCHANGE DDL is bounded by lock_acquire_timeout (120s
// server default) — revisit this TTL if that server setting is raised. The
// activity's 8m ScheduleToCloseTimeout leaves room for a retry after a lapsed
// lock; a fully exhausted budget just defers to the next tick.
const identityMapReplaceLockTTL = 5 * time.Minute

type identityMapStore interface {
	ListIdentityMapEntries(ctx context.Context) ([]activitiesrepo.ListIdentityMapEntriesRow, error)
}

type identityMapTelemetry interface {
	ReplaceIdentityMap(ctx context.Context, entries []telemetryrepo.IdentityMapEntry) error
}

type SyncIdentityMap struct {
	logger    *slog.Logger
	store     identityMapStore
	telemetry identityMapTelemetry
	cache     cache.Cache
}

func NewSyncIdentityMap(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn, cacheAdapter cache.Cache) *SyncIdentityMap {
	return &SyncIdentityMap{
		logger:    logger.With(attr.SlogComponent("sync_identity_map")),
		store:     activitiesrepo.New(db),
		telemetry: telemetryrepo.New(chConn),
		cache:     cacheAdapter,
	}
}

type SyncIdentityMapResult struct {
	Entries int
}

func (s *SyncIdentityMap) Do(ctx context.Context) (*SyncIdentityMapResult, error) {
	rows, err := s.store.ListIdentityMapEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list identity map entries: %w", err)
	}

	entries := make([]telemetryrepo.IdentityMapEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, telemetryrepo.IdentityMapEntry{
			OrgID:           row.OrganizationID,
			EmailLower:      row.EmailLower,
			CanonicalUserID: row.CanonicalUserID,
			CanonicalEmail:  row.CanonicalEmail,
		})
	}

	// Single-writer claim across the whole replacement. Losing the race defers
	// to the holder (the retry or the next tick picks up after the claim
	// clears); a claim leaked by a crashed attempt lapses at the TTL, after
	// which no statement from that attempt can still be in flight.
	claimed, err := s.cache.Add(ctx, identityMapReplaceLockKey, identityMapReplaceLockTTL)
	if err != nil {
		return nil, fmt.Errorf("claim identity map replacement lock: %w", err)
	}
	if !claimed {
		return nil, fmt.Errorf("identity map replacement already in progress")
	}

	if err := s.telemetry.ReplaceIdentityMap(ctx, entries); err != nil {
		// Deliberately keep the claim: this attempt may have statements in
		// flight, and the TTL is what guarantees they are dead before the
		// next writer starts.
		return nil, fmt.Errorf("replace identity map: %w", err)
	}

	// Release on success only, so the schedule and link-event triggers are
	// not blocked for the remainder of the TTL. Best effort: a leaked claim
	// merely delays the next refresh.
	if err := s.cache.Delete(context.WithoutCancel(ctx), identityMapReplaceLockKey); err != nil {
		s.logger.WarnContext(ctx, "failed to release identity map replacement lock", attr.SlogError(err))
	}

	// This success line is the staleness signal: a quiet component means the
	// map is drifting from Postgres until the next successful pass.
	s.logger.InfoContext(ctx, "identity map synced", attr.SlogIdentityMapEntryCount(len(entries)))

	return &SyncIdentityMapResult{Entries: len(entries)}, nil
}
