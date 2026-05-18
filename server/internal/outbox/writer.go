// Package outbox provides the producer-side helpers for writing events to
// the outbox table and waking the per-org Svix relay workflow.
//
// Typical usage from a service handler:
//
//	id, err := outbox.NewWriter(db).Insert(ctx, tx, outbox.InsertParams{
//	    OrganizationID: orgID,
//	    EventType:      "audit_log:created",
//	    Payload:        payloadJSON,
//	})
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

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type EventType string

const (
	EventTypeAuditLogCreated    EventType = "audit_log.created"
	EventTypeRiskFindingCreated EventType = "risk_finding.created"
)

// AppendParams is the producer-facing payload for queuing an event.
type AppendParams struct {
	OrganizationID string
	EventType      EventType
	Payload        any
}

// AppendResult identifies the inserted row.
type AppendResult struct {
	ID             int64
	OrganizationID string
}

// Append inserts a new outbox event for an organization.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func Append(ctx context.Context, dbtx DBTX, p AppendParams) (AppendResult, error) {
	jsonPayload, err := json.Marshal(p.Payload)
	if err != nil {
		return AppendResult{}, fmt.Errorf("marshal outbox payload: %w", err)
	}

	row, err := repo.New(noopCopyFromDBTX{DBTX: dbtx}).InsertOutboxEntry(ctx, repo.InsertOutboxEntryParams{
		OrganizationID: p.OrganizationID,
		EventType:      string(p.EventType),
		Payload:        jsonPayload,
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("insert outbox entry: %w", err)
	}
	return AppendResult{
		ID:             row.ID,
		OrganizationID: p.OrganizationID,
	}, nil
}

type AppendBatchResult struct {
	Count int64
}

// AppendBatch inserts multiple outbox events for an organization and returns
// the count of inserted rows. This is a much more efficient alternative to
// multiple calls to Append when queuing many events at once.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func AppendBatch(ctx context.Context, dbtx repo.DBTX, ps []AppendParams) (AppendBatchResult, error) {
	var empty AppendBatchResult
	entries := make([]repo.BulkInsertOutboxEntriesParams, 0, len(ps))
	for _, p := range ps {
		jsonPayload, err := json.Marshal(p.Payload)
		if err != nil {
			return empty, fmt.Errorf("marshal outbox payload: %w", err)
		}
		entries = append(entries, repo.BulkInsertOutboxEntriesParams{
			OrganizationID: p.OrganizationID,
			EventType:      string(p.EventType),
			Payload:        jsonPayload,
		})
	}

	n, err := repo.New(dbtx).BulkInsertOutboxEntries(ctx, entries)
	if err != nil {
		return empty, fmt.Errorf("bulk insert outbox entries: %w", err)
	}

	return AppendBatchResult{Count: n}, nil
}

type noopCopyFromDBTX struct {
	DBTX
}

func (n noopCopyFromDBTX) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, oops.Permanent(errors.New("not implemented"))
}
