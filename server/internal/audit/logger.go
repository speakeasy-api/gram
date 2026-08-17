package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

type Logger struct{}

func NewLogger() *Logger {
	return &Logger{}
}

type auditEntry struct {
	Params      repo.InsertAuditLogParams
	OutboxEvent *outbox.EventDef[events.AuditLogCreatedPayloadV1]
}

// log writes one audit record.
//
// Acting surface and client are stamped here rather than by each event, so
// every audited mutation carries them and no call site can forget or disagree.
// They describe the request, not the event, which is why they are derived from
// the context at the moment of the write.
func (l *Logger) log(ctx context.Context, dbtx repo.DBTX, entry auditEntry) error {
	identity := actingIdentityFromContext(ctx)
	entry.Params.ActingSurface = string(identity.Surface)
	// A call with no OAuth client records NULL, not an empty string: absent and
	// blank are different answers to "which client acted".
	entry.Params.ActingClientID = conv.ToPGTextEmpty(identity.ClientID)

	row, err := repo.New(dbtx).InsertAuditLog(ctx, entry.Params)
	if err != nil {
		return fmt.Errorf("log %s: %w", entry.Params.Action, err)
	}

	if err := appendToOutbox(ctx, dbtx, entry, row); err != nil {
		return err
	}

	return nil
}
