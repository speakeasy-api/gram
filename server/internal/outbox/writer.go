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
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox/repo"
)

type EventType string

const (
	EventTypeAuditLogCreated EventType = "audit_log.created"
)

// InsertParams is the producer-facing payload for queuing an event.
type InsertParams struct {
	OrganizationID string
	EventType      EventType
	Payload        any
}

// InsertResult identifies the inserted row.
type InsertResult struct {
	ID             int64
	OrganizationID string
	PublicID       uuid.UUID
}

// Append inserts a new outbox event for an organization and returns identifiers
// needed for downstream relay/signal coordination.
func Append(ctx context.Context, dbtx repo.DBTX, p InsertParams) (InsertResult, error) {
	jsonPayload, err := json.Marshal(p.Payload)
	if err != nil {
		return InsertResult{}, fmt.Errorf("marshal outbox payload: %w", err)
	}

	row, err := repo.New(dbtx).InsertOutboxEntry(ctx, repo.InsertOutboxEntryParams{
		OrganizationID: p.OrganizationID,
		EventType:      string(p.EventType),
		Payload:        jsonPayload,
	})
	if err != nil {
		return InsertResult{}, fmt.Errorf("insert outbox entry: %w", err)
	}
	return InsertResult{
		ID:             row.ID,
		PublicID:       row.PublicID,
		OrganizationID: p.OrganizationID,
	}, nil
}
