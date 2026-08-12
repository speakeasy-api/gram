package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

const selectedUseToolCategory = "remote_mcp_tool"

// SelectedUseRecorder persists only the first successful normal Remote MCP tool
// use for an active Platform distribution version. It deliberately accepts no
// request or response content.
type SelectedUseRecorder struct {
	db  *pgxpool.Pool
	now func() time.Time
}

var _ toolcallobserver.SuccessRecorder = (*SelectedUseRecorder)(nil)

func NewSelectedUseRecorder(db *pgxpool.Pool) *SelectedUseRecorder {
	return &SelectedUseRecorder{db: db, now: time.Now}
}

func (r *SelectedUseRecorder) RecordSuccessfulToolCall(ctx context.Context, observation toolcallobserver.SuccessObservation) {
	if r == nil || r.db == nil || observation.OrganizationID == "" || observation.UserID == "" || observation.ProjectID == uuid.Nil || observation.MCPServerID == uuid.Nil || !validSelectedUseToolName(observation.ToolName) {
		return
	}

	_ = r.record(ctx, observation)
}

func (r *SelectedUseRecorder) record(ctx context.Context, observation toolcallobserver.SuccessObservation) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin platform mcp selected-use record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	target, err := q.GetPlatformMCPSelectedUseTarget(ctx, repo.GetPlatformMCPSelectedUseTargetParams{
		InitiatingSubjectUrn: userSubjectURN(observation.UserID),
		OrganizationID:       observation.OrganizationID,
		ProjectID:            observation.ProjectID,
		McpServerID:          uuid.NullUUID{UUID: observation.MCPServerID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve platform mcp selected-use target: %w", err)
	}

	succeededAt := observation.SucceededAt
	if succeededAt.IsZero() {
		succeededAt = r.now().UTC()
	}
	if err := q.CreatePlatformMCPSelectedUseEvidence(ctx, repo.CreatePlatformMCPSelectedUseEvidenceParams{
		OrganizationID:      observation.OrganizationID,
		ProjectID:           observation.ProjectID,
		RegistrationID:      target.RegistrationID,
		DistributionID:      target.DistributionID,
		DistributionVersion: target.DistributionVersion,
		WorkflowID:          target.WorkflowID,
		ToolName:            observation.ToolName,
		ToolCategory:        selectedUseToolCategory,
		SucceededAt:         pgtype.Timestamptz{Time: succeededAt, Valid: true},
	}); err != nil {
		return fmt.Errorf("create platform mcp selected-use evidence: %w", err)
	}
	if err := q.RecordPlatformMCPFirstValueAchieved(ctx, repo.RecordPlatformMCPFirstValueAchievedParams{
		OrganizationID:       observation.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: target.ConnectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: target.ConnectionGeneration, Valid: true},
		ProjectID:            uuid.NullUUID{UUID: observation.ProjectID, Valid: true},
		McpKey:               target.McpKey,
	}); err != nil {
		return fmt.Errorf("record platform mcp first value: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit platform mcp selected-use record: %w", err)
	}
	return nil
}

func validSelectedUseToolName(name string) bool {
	return name != "" && len(name) <= 128 && strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) == -1
}
