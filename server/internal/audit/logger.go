package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

type Logger struct{}

func NewLogger() *Logger {
	return &Logger{}
}

type auditEntry struct {
	Params      repo.InsertAuditLogParams
	OutboxEvent *outbox.EventDef[events.AuditLogCreatedPayload]
}

func (l *Logger) log(ctx context.Context, dbtx repo.DBTX, entry auditEntry) error {
	row, err := repo.New(dbtx).InsertAuditLog(ctx, entry.Params)
	if err != nil {
		return fmt.Errorf("log %s: %w", entry.Params.Action, err)
	}

	if err := appendToOutbox(ctx, dbtx, entry, row); err != nil {
		return err
	}

	return nil
}
