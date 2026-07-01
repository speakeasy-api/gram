package assistants

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

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
	// managedAssistantModel is the default model for the platform-managed
	// assistant. Defaulted to Sonnet 5, matching the in-app default chat model.
	managedAssistantModel = "anthropic/claude-sonnet-5"

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
// at 120, so the composed name always fits. The user-facing dashboard surfaces
// (card, sidebar) render the plain "Project Assistant" label; this fuller name
// is what shows in org-wide / admin assistant listings.
func managedAssistantName(projectName string) string {
	return fmt.Sprintf("Project Assistant for %s", projectName)
}

// EnableManagedAssistant turns on the managed assistant for a project: it
// creates the assistant (with no toolsets attached) and records it in
// project_managed_assistants. The managed assistant exists only while this
// mapping row does — disabling or removing the project tears it down.
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
		// Repair managed assistants provisioned before dashboard ingress existed:
		// the trigger is provisioned lazily here so re-running enable is enough.
		if err := s.ensureDashboardTrigger(ctx, s.db, organizationID, projectID, existing.ID, existing.Name); err != nil {
			return assistantRecord{}, err
		}
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
	instances, err := triggerQueries.ListActiveTriggerInstancesByTarget(ctx, dashboardTriggerTarget(projectID, row.ID))
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

// dashboardTriggerTarget selects the managed assistant's dashboard direct-ingress
// trigger instance. Provisioning and teardown must key on the same target, so the
// selector lives in one place.
func dashboardTriggerTarget(projectID, assistantID uuid.UUID) triggerrepo.ListActiveTriggerInstancesByTargetParams {
	return triggerrepo.ListActiveTriggerInstancesByTargetParams{
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      assistantID.String(),
	}
}

// ensureDashboardTrigger provisions the direct-ingress trigger instance that
// routes dashboard sidebar messages to the managed assistant, creating one only
// when absent. Idempotent so the enable fast path can heal a managed assistant
// that predates dashboard ingress without depending on a fresh create.
func (s *ServiceCore) ensureDashboardTrigger(ctx context.Context, db triggerrepo.DBTX, organizationID string, projectID, assistantID uuid.UUID, name string) error {
	_, err := triggerrepo.New(db).CreateDashboardTriggerInstance(ctx, triggerrepo.CreateDashboardTriggerInstanceParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		Name:           name,
		EnvironmentID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      assistantID.String(),
		TargetDisplay:  name,
		ConfigJson:     []byte("{}"),
		Status:         StatusActive,
	})
	// ON CONFLICT DO NOTHING returns no rows when an active trigger already
	// exists for this project and target — the idempotent success path.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("create dashboard trigger instance: %w", err)
	}
	return nil
}

// createManagedAssistant inserts the assistant and records the managed mapping in
// a single transaction. It attaches no toolsets: a new managed assistant starts
// with only the built-in and platform tools, and an admin adds project MCP
// servers deliberately afterwards.
func (s *ServiceCore) createManagedAssistant(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	name string,
	createdByUserID string,
) (assistantRecord, error) {
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

	if err := s.ensureDashboardTrigger(ctx, tx, organizationID, projectID, record.ID, name); err != nil {
		return assistantRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit managed assistant tx: %w", err)
	}

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

// dashboardIngestPayload is the wire shape the dashboard trigger definition
// decodes in BuildDirectEvent.
type dashboardIngestPayload struct {
	Text           string `json:"text"`
	UserID         string `json:"user_id"`
	CorrelationID  string `json:"correlation_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// DashboardSendResult is what the sendMessage endpoint returns to the dashboard.
type DashboardSendResult struct {
	ChatID   uuid.UUID
	ThreadID uuid.UUID
	Accepted bool
}

// SendDashboardMessage ingests a message from a dashboard user as a turn on the
// given assistant, routing through the dashboard trigger so it shares the
// status/filter/delivery path with every other ingress source. Returns
// pgx.ErrNoRows when the assistant does not exist in the project.
//
// userID is the calling user (attribution). chatID identifies the conversation:
// pass uuid.Nil to start a new one (a fresh chat id is minted server-side and
// returned), or an existing chat id to continue it. idempotencyKey may be empty
// — a fresh one is minted so the ingest still succeeds, but callers that want
// retry-safe dedupe should pass a stable key.
func (s *ServiceCore) SendDashboardMessage(ctx context.Context, projectID, assistantID uuid.UUID, userID string, chatID uuid.UUID, text, idempotencyKey string) (DashboardSendResult, error) {
	assistant, err := s.GetAssistant(ctx, projectID, assistantID)
	if err != nil {
		return DashboardSendResult{}, err
	}

	if chatID == uuid.Nil {
		chatID = uuid.New()
	}
	correlationID := chatID.String()

	instanceID, err := s.resolveDashboardTriggerInstance(ctx, assistant.OrganizationID, projectID, assistant.ID, assistant.Name)
	if err != nil {
		return DashboardSendResult{}, err
	}

	if s.dashboardIngestor == nil {
		return DashboardSendResult{}, fmt.Errorf("dashboard ingestor is not configured")
	}

	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	payload, err := json.Marshal(dashboardIngestPayload{Text: text, UserID: userID, CorrelationID: correlationID, IdempotencyKey: idempotencyKey})
	if err != nil {
		return DashboardSendResult{}, fmt.Errorf("marshal dashboard message: %w", err)
	}

	task, err := s.dashboardIngestor.IngestDirect(ctx, instanceID, payload, time.Now().UTC())
	if err != nil {
		return DashboardSendResult{}, fmt.Errorf("ingest dashboard message: %w", err)
	}

	result := DashboardSendResult{
		ChatID:   chatID,
		ThreadID: uuid.Nil,
		Accepted: task != nil,
	}
	if task == nil {
		return result, nil
	}

	threadID, err := assistantrepo.New(s.db).GetAssistantThreadIDByCorrelation(ctx, assistantrepo.GetAssistantThreadIDByCorrelationParams{
		ProjectID:     projectID,
		AssistantID:   assistant.ID,
		CorrelationID: correlationID,
	})
	if err != nil {
		return DashboardSendResult{}, fmt.Errorf("resolve dashboard thread: %w", err)
	}
	result.ThreadID = threadID
	return result, nil
}

// resolveDashboardTriggerInstance returns the managed assistant's dashboard
// trigger instance, provisioning one first to heal assistants that predate
// dashboard ingress.
func (s *ServiceCore) resolveDashboardTriggerInstance(ctx context.Context, organizationID string, projectID, assistantID uuid.UUID, name string) (uuid.UUID, error) {
	queries := triggerrepo.New(s.db)
	instances, err := queries.ListActiveTriggerInstancesByTarget(ctx, dashboardTriggerTarget(projectID, assistantID))
	if err != nil {
		return uuid.Nil, fmt.Errorf("list dashboard trigger instances: %w", err)
	}
	if len(instances) == 0 {
		if err := s.ensureDashboardTrigger(ctx, s.db, organizationID, projectID, assistantID, name); err != nil {
			return uuid.Nil, err
		}
		instances, err = queries.ListActiveTriggerInstancesByTarget(ctx, dashboardTriggerTarget(projectID, assistantID))
		if err != nil {
			return uuid.Nil, fmt.Errorf("list dashboard trigger instances: %w", err)
		}
	}
	if len(instances) == 0 {
		return uuid.Nil, fmt.Errorf("dashboard trigger instance not provisioned for assistant %s", assistantID)
	}
	return instances[0].ID, nil
}
