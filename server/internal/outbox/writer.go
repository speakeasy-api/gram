// Package outbox provides the producer-side helpers for writing events to
// the outbox table and waking the per-org Svix relay workflow.
//
// Typical usage from a service handler:
//
//	id, err := outbox.AppendEvent(ctx, tx, orgID, events.AuditLogCreated, payload)
//	if err != nil { ... }
//	// commit the caller's tx, then:
//	signaler.SignalRelay(ctx, orgID)
//
// The signal MUST be issued after the caller's transaction commits.
// Issuing it earlier risks waking the workflow before the row is visible to
// other connections; if the commit later fails the workflow will see no work
// and exit, with the next producer signal eventually doing the relay.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/repo"
)

// DBTX is the minimal database interface required by AppendEvent. Callers can
// pass a transaction or a pool; bulk operations require repo.DBTX (see
// AppendBatchEvents).
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// AppendResult identifies the inserted outbox row.
type AppendResult struct {
	ID             int64
	OrganizationID string
}

// AppendBatchResult is returned by AppendBatchEvents.
type AppendBatchResult struct {
	Count int64
}

// Append inserts a single typed outbox event within a transaction.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func Append[T any](ctx context.Context, dbtx DBTX, orgID string, def *EventDef[T], payload T) (AppendResult, error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return AppendResult{}, fmt.Errorf("marshal outbox payload: %w", err)
	}
	row, err := repo.New(noopCopyFromDBTX{DBTX: dbtx}).InsertOutboxEntry(ctx, repo.InsertOutboxEntryParams{
		OrganizationID: orgID,
		EventType:      string(def.EventType()),
		Payload:        jsonPayload,
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("insert outbox entry: %w", err)
	}
	return AppendResult{
		ID:             row.ID,
		OrganizationID: orgID,
	}, nil
}

// AppendBatch inserts multiple events of the same type within a transaction.
// This is much more efficient than repeated AppendEvent calls for large batches.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func AppendBatch[T any](ctx context.Context, dbtx repo.DBTX, orgID string, def *EventDef[T], payloads []T) (AppendBatchResult, error) {
	entries := make([]repo.BulkInsertOutboxEntriesParams, 0, len(payloads))
	for _, p := range payloads {
		jsonPayload, err := json.Marshal(p)
		if err != nil {
			return AppendBatchResult{}, fmt.Errorf("marshal outbox payload: %w", err)
		}
		entries = append(entries, repo.BulkInsertOutboxEntriesParams{
			OrganizationID: orgID,
			EventType:      string(def.EventType()),
			Payload:        jsonPayload,
		})
	}
	n, err := repo.New(dbtx).BulkInsertOutboxEntries(ctx, entries)
	if err != nil {
		return AppendBatchResult{}, fmt.Errorf("bulk insert outbox entries: %w", err)
	}
	return AppendBatchResult{Count: n}, nil
}

type noopCopyFromDBTX struct {
	DBTX
}

func (n noopCopyFromDBTX) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, oops.Permanent(errors.New("not implemented"))
}
