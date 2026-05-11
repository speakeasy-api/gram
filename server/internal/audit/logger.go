package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
)

type Logger struct{}

func NewLogger() *Logger {
	return &Logger{}
}

func (l *Logger) log(ctx context.Context, dbtx repo.DBTX, entry repo.InsertAuditLogParams) error {
	row, err := repo.New(dbtx).InsertAuditLog(ctx, entry)
	if err != nil {
		return fmt.Errorf("log %s: %w", entry.Action, err)
	}

	if err := appendToOutbox(ctx, dbtx, entry, row); err != nil {
		return err
	}

	return nil
}
