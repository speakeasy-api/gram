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

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

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
}

func NewSyncIdentityMap(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn) *SyncIdentityMap {
	return &SyncIdentityMap{
		logger:    logger.With(attr.SlogComponent("sync_identity_map")),
		store:     activitiesrepo.New(db),
		telemetry: telemetryrepo.New(chConn),
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

	if err := s.telemetry.ReplaceIdentityMap(ctx, entries); err != nil {
		return nil, fmt.Errorf("replace identity map: %w", err)
	}

	// This success line is the staleness signal: a quiet component means the
	// map is drifting from Postgres until the next successful pass.
	s.logger.InfoContext(ctx, "identity map synced", attr.SlogIdentityMapEntryCount(len(entries)))

	return &SyncIdentityMapResult{Entries: len(entries)}, nil
}
