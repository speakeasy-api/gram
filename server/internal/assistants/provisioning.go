package assistants

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// managedAssistantInstructions is the system prompt for the platform-managed
// assistant that powers the AI Insights sidebar. Ported from the inline prompt
// that previously lived in the dashboard (insights-sidebar.tsx); the dynamic
// "current date" line was dropped because the runtime composes time context
// separately.
//
//go:embed managed_assistant_instructions.txt
var managedAssistantInstructions string

const (
	// managedAssistantModel mirrors the model the sidebar used client-side.
	managedAssistantModel = "anthropic/claude-sonnet-4.6"

	// Schema defaults for the assistants table, applied explicitly so the
	// managed assistant's intent is visible at the call site.
	managedAssistantWarmTTLSeconds int64 = 60
	managedAssistantMaxConcurrency int64 = 10
)

// ErrManagedAssistantNameTaken is returned by EnableManagedAssistant when a
// non-managed assistant already occupies the managed assistant's name in the
// project. The caller should ask the user to rename or remove it.
var ErrManagedAssistantNameTaken = errors.New("an assistant with the managed assistant's name already exists in this project")

// managedAssistantName composes a project's managed-assistant display name. The
// project name is embedded so the per-project assistants stay distinguishable
// across an organization. project.name is capped at 40 chars and assistants.name
// at 120, so the composed name always fits.
func managedAssistantName(projectName string) string {
	return fmt.Sprintf("Speakeasy Assistant for %s", projectName)
}

// EnableManagedAssistant turns on the managed assistant for a project: it
// creates the assistant (attaching every MCP-reachable project toolset) and
// records it in project_managed_assistants. The managed assistant exists only
// while this mapping row does — disabling or removing the project tears it down.
//
// Idempotent and safe under concurrency: the fast path returns the existing
// managed assistant, and an enable that loses a race is recovered by re-reading
// the winner. A unique violation has two causes — a concurrent enable (the
// mapping now exists, so the re-read returns it) or a non-managed assistant
// already holding the name (no mapping, so we surface ErrManagedAssistantNameTaken).
//
// createdByUserID may be empty for system-initiated enablement; it is recorded
// as NULL in that case.
func (s *ServiceCore) EnableManagedAssistant(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	createdByUserID string,
) (assistantRecord, error) {
	existing, err := s.GetManagedAssistant(ctx, projectID)
	switch {
	case err == nil:
		return existing, nil
	case errors.Is(err, pgx.ErrNoRows):
		// fall through to create
	default:
		return assistantRecord{}, err
	}

	projectName, err := assistantrepo.New(s.db).GetProjectName(ctx, projectID)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("load project for managed assistant: %w", err)
	}
	name := managedAssistantName(projectName)

	record, err := s.createManagedAssistant(ctx, organizationID, projectID, name, createdByUserID)
	if err == nil {
		return record, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		recovered, reErr := s.GetManagedAssistant(ctx, projectID)
		if reErr == nil {
			return recovered, nil
		}
		if errors.Is(reErr, pgx.ErrNoRows) {
			return assistantRecord{}, fmt.Errorf("%w: %q — rename or remove it to enable the managed assistant for this project", ErrManagedAssistantNameTaken, name)
		}
		return assistantRecord{}, reErr
	}
	return assistantRecord{}, err
}

// GetManagedAssistant resolves a project's managed assistant and hydrates its
// toolsets. Returns pgx.ErrNoRows when the feature isn't enabled for the project.
func (s *ServiceCore) GetManagedAssistant(ctx context.Context, projectID uuid.UUID) (assistantRecord, error) {
	row, err := assistantrepo.New(s.db).GetManagedAssistantByProject(ctx, projectID)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("get managed assistant: %w", err)
	}
	record := assistantRecordFromManagedRow(row)
	refs, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

// DisableManagedAssistant turns the managed assistant off for a project: it
// removes the mapping and soft-deletes the assistant. No-op when the project
// has no managed assistant.
func (s *ServiceCore) DisableManagedAssistant(ctx context.Context, projectID uuid.UUID) error {
	row, err := assistantrepo.New(s.db).GetManagedAssistantByProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get managed assistant: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin disable managed assistant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := assistantrepo.New(tx)
	if err := queries.DeleteProjectManagedAssistant(ctx, projectID); err != nil {
		return fmt.Errorf("delete managed assistant mapping: %w", err)
	}
	if err := queries.DeleteAssistant(ctx, assistantrepo.DeleteAssistantParams{
		AssistantID: row.ID,
		ProjectID:   projectID,
	}); err != nil {
		return fmt.Errorf("soft-delete managed assistant: %w", err)
	}

	triggerQueries := triggerrepo.New(tx)
	instances, err := triggerQueries.ListActiveTriggerInstancesByTarget(ctx, triggerrepo.ListActiveTriggerInstancesByTargetParams{
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      row.ID.String(),
	})
	if err != nil {
		return fmt.Errorf("list dashboard trigger instances: %w", err)
	}
	for _, inst := range instances {
		if _, err := triggerQueries.DeleteTriggerInstance(ctx, triggerrepo.DeleteTriggerInstanceParams{
			ID:        inst.ID,
			ProjectID: projectID,
		}); err != nil {
			return fmt.Errorf("delete dashboard trigger instance: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit disable managed assistant tx: %w", err)
	}
	return nil
}

// createManagedAssistant inserts the assistant, records the managed mapping, and
// attaches all of the project's MCP-reachable toolsets in a single transaction.
func (s *ServiceCore) createManagedAssistant(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	name string,
	createdByUserID string,
) (assistantRecord, error) {
	slugs, err := assistantrepo.New(s.db).ListProjectMCPToolsetSlugs(ctx, projectID)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("list project toolsets for managed assistant: %w", err)
	}
	refs := make([]*types.AssistantToolsetRef, 0, len(slugs))
	for _, slug := range slugs {
		refs = append(refs, &types.AssistantToolsetRef{ToolsetSlug: slug, EnvironmentSlug: nil})
	}

	resolved, err := s.resolveToolsetRefsForWrite(ctx, projectID, refs)
	if err != nil {
		return assistantRecord{}, err
	}

	var createdBy pgtype.Text
	if createdByUserID != "" {
		createdBy = conv.ToPGText(createdByUserID)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin managed assistant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := assistantrepo.New(tx)
	created, err := queries.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       projectID,
		OrganizationID:  organizationID,
		CreatedByUserID: createdBy,
		Name:            name,
		Model:           managedAssistantModel,
		Instructions:    managedAssistantInstructions,
		WarmTtlSeconds:  managedAssistantWarmTTLSeconds,
		MaxConcurrency:  managedAssistantMaxConcurrency,
		Status:          StatusActive,
	})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("insert managed assistant: %w", err)
	}
	record := assistantRecordFromCreateRow(created)

	if err := queries.CreateProjectManagedAssistant(ctx, assistantrepo.CreateProjectManagedAssistantParams{
		ProjectID:   projectID,
		AssistantID: record.ID,
	}); err != nil {
		return assistantRecord{}, fmt.Errorf("insert managed assistant mapping: %w", err)
	}

	// Dashboard sidebar messages reach the assistant through the trigger
	// dispatch path, so the managed assistant gets a direct-ingress trigger
	// instance. It's hidden from the user's trigger list (KindDirect).
	if _, err := triggerrepo.New(tx).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		Name:           name,
		EnvironmentID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      record.ID.String(),
		TargetDisplay:  name,
		ConfigJson:     []byte("{}"),
		Status:         StatusActive,
	}); err != nil {
		return assistantRecord{}, fmt.Errorf("create dashboard trigger instance: %w", err)
	}

	if err := writeAssistantToolsets(ctx, tx, record.ID, projectID, resolved); err != nil {
		return assistantRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit managed assistant tx: %w", err)
	}

	refs2, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs2[record.ID]
	return record, nil
}

func assistantRecordFromManagedRow(row assistantrepo.GetManagedAssistantByProjectRow) assistantRecord {
	return assistantRecord{
		ID:              row.ID,
		ProjectID:       row.ProjectID,
		OrganizationID:  row.OrganizationID,
		CreatedByUserID: conv.FromPGTextOrEmpty[string](row.CreatedByUserID),
		Name:            row.Name,
		Model:           row.Model,
		Instructions:    row.Instructions,
		Toolsets:        nil,
		WarmTTLSeconds:  conv.SafeInt(row.WarmTtlSeconds),
		MaxConcurrency:  conv.SafeInt(row.MaxConcurrency),
		Status:          row.Status,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
		DeletedAt:       row.DeletedAt,
	}
}
