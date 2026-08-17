package audittest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type LogRecord struct {
	Action         string
	OrganizationID string
	ProjectID      uuid.NullUUID

	// The display name is denormalized and masked for staff; assert on these
	// instead. ActorDisplayName keeps the presence-vs-empty distinction the
	// masking tests assert on; ActorDisplay is its collapsed convenience form.
	ActorID          string
	ActorType        string
	ActorDisplayName *string
	ActorDisplay     string
	SubjectID        string
	SubjectType      string
	SubjectDisplay   string
	SubjectSlug      string
	Metadata         []byte
	BeforeSnapshot   []byte
	AfterSnapshot    []byte

	// ActingSurface is how the change was made; ActingClientID keeps the
	// absent-vs-blank distinction, since a call with no OAuth client records
	// NULL rather than an empty string.
	ActingSurface  string
	ActingClientID *string
}

func LatestAuditLogByAction(ctx context.Context, dbtx repo.DBTX, action audit.Action) (LogRecord, error) {
	row, err := repo.New(dbtx).GetLatestAuditLogByAction(ctx, string(action))
	if err != nil {
		return LogRecord{}, fmt.Errorf("get latest audit log by action: %w", err)
	}

	return LogRecord{
		Action:           row.Action,
		OrganizationID:   row.OrganizationID,
		ProjectID:        row.ProjectID,
		ActorID:          row.ActorID,
		ActorType:        row.ActorType,
		ActorDisplayName: conv.FromPGText[string](row.ActorDisplayName),
		ActorDisplay:     conv.PtrValOr(conv.FromPGText[string](row.ActorDisplayName), ""),
		SubjectID:        row.SubjectID,
		SubjectType:      row.SubjectType,
		SubjectDisplay:   conv.PtrValOr(conv.FromPGText[string](row.SubjectDisplayName), ""),
		SubjectSlug:      conv.PtrValOr(conv.FromPGText[string](row.SubjectSlug), ""),
		Metadata:         row.Metadata,
		BeforeSnapshot:   row.BeforeSnapshot,
		AfterSnapshot:    row.AfterSnapshot,
		ActingSurface:    row.ActingSurface,
		ActingClientID:   conv.FromPGText[string](row.ActingClientID),
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
