package audittest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	// The feed returns this alongside the display name, and the staff mask
	// clears it only for an actor id that resolves to a Gram user.
	ActorSlug      string
	SubjectID      string
	SubjectType    string
	SubjectDisplay string
	SubjectSlug    string
	Metadata       []byte
	BeforeSnapshot []byte
	AfterSnapshot  []byte

	// ActingSurface and ActingClientID are read straight from the row, so both
	// keep the absent-vs-blank distinction: a row written before attribution
	// existed has no surface at all, and a call with no OAuth client records
	// NULL rather than an empty string.
	ActingSurface  *string
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
		ActorSlug:        conv.PtrValOr(conv.FromPGText[string](row.ActorSlug), ""),
		SubjectID:        row.SubjectID,
		SubjectType:      row.SubjectType,
		SubjectDisplay:   conv.PtrValOr(conv.FromPGText[string](row.SubjectDisplayName), ""),
		SubjectSlug:      conv.PtrValOr(conv.FromPGText[string](row.SubjectSlug), ""),
		Metadata:         row.Metadata,
		BeforeSnapshot:   row.BeforeSnapshot,
		AfterSnapshot:    row.AfterSnapshot,
		ActingSurface:    conv.FromPGText[string](row.ActingSurface),
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

// RejectAction makes every audit insert for one action fail, so a caller can
// prove that its mutation and its entry commit together.
//
// It alters the schema, so it is only sound in a test that owns its database,
// and it can be called once per database. There is no seam inside the Logger to
// fail instead: it holds no state.
func RejectAction(ctx context.Context, dbtx repo.DBTX, action audit.Action) error {
	literal := strings.ReplaceAll(string(action), "'", "''")
	stmt := fmt.Sprintf(
		"ALTER TABLE audit_logs ADD CONSTRAINT audittest_reject_action CHECK (action <> '%s')",
		literal,
	)

	if _, err := dbtx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("reject audit action %s: %w", action, err)
	}

	return nil
}
