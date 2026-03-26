package audittest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type LogRecord struct {
	Action         string
	SubjectType    string
	SubjectDisplay string
	SubjectSlug    string
	Metadata       []byte
	BeforeSnapshot []byte
	AfterSnapshot  []byte
}

func LatestAuditLogByAction(ctx context.Context, dbtx repo.DBTX, action audit.Action) (LogRecord, error) {
	row, err := repo.New(dbtx).GetLatestAuditLogByAction(ctx, string(action))
	if err != nil {
		return LogRecord{}, fmt.Errorf("get latest audit log by action: %w", err)
	}

	return LogRecord{
		Action:         row.Action,
		SubjectType:    row.SubjectType,
		SubjectDisplay: conv.PtrValOr(conv.FromPGText[string](row.SubjectDisplayName), ""),
		SubjectSlug:    conv.PtrValOr(conv.FromPGText[string](row.SubjectSlug), ""),
		Metadata:       row.Metadata,
		BeforeSnapshot: row.BeforeSnapshot,
		AfterSnapshot:  row.AfterSnapshot,
	}, nil
}

func AuditLogCount(ctx context.Context, dbtx repo.DBTX) (int64, error) {
	count, err := repo.New(dbtx).CountAuditLogs(ctx)
	if err != nil {
		return 0, fmt.Errorf("count audit logs: %w", err)
	}

	return count, nil
}

func AuditLogCountByAction(ctx context.Context, dbtx repo.DBTX, action audit.Action) (int64, error) {
	count, err := repo.New(dbtx).CountAuditLogsByAction(ctx, string(action))
	if err != nil {
		return 0, fmt.Errorf("count audit logs by action: %w", err)
	}

	return count, nil
}

func DecodeAuditData(payload []byte) (map[string]any, error) {
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("decode audit snapshot: %w", err)
	}

	return snapshot, nil
}
