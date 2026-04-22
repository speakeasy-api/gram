package assistants

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

const (
	DefaultWarmTTLSeconds = 300
	DefaultMaxConcurrency = 1

	StatusActive = "active"
	StatusPaused = "paused"

	sourceKindSlack      = "slack"
	sourceKindCron       = "cron"
	runtimeStateStarting = "starting"
	runtimeStateActive   = "active"
	runtimeStateStopped  = "stopped"
	runtimeStateFailed   = "failed"

	eventStatusPending    = "pending"
	eventStatusProcessing = "processing"
	eventStatusCompleted  = "completed"
	eventStatusFailed     = "failed"

	// maxEventAttempts caps how many times a single event will be retried
	// against a live runtime before it's marked terminally failed. Prevents
	// a broken upstream (LLM 502, bad tool, etc.) from burning the retry
	// loop forever.
	maxEventAttempts = 5

	runtimeStartupReapGrace      = 2 * time.Minute
	runtimeWarmExpiryReapGrace   = 1 * time.Minute
	runtimeProcessingLeaseGrace  = 2 * time.Minute
	eventProcessingRequeueGrace  = 3 * time.Minute
	processingLeaseHeartbeatTick = 30 * time.Second
)

type assistantRecord struct {
	ID             uuid.UUID
	ProjectID      uuid.UUID
	OrganizationID string
	Name           string
	Model          string
	Instructions   string
	Toolsets       []assistantToolsetRow
	WarmTTLSeconds int
	MaxConcurrency int
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      sql.NullTime
}

type assistantThreadRecord struct {
	ID            uuid.UUID
	AssistantID   uuid.UUID
	ProjectID     uuid.UUID
	CorrelationID string
	ChatID        uuid.UUID
	SourceKind    string
	SourceRefJSON []byte
	LastEventAt   time.Time
}

type assistantRuntimeRecord struct {
	ID                  uuid.UUID
	AssistantThreadID   uuid.UUID
	AssistantID         uuid.UUID
	ProjectID           uuid.UUID
	Backend             string
	BackendMetadataJSON []byte
	State               string
	WarmUntil           sql.NullTime
}

type assistantThreadEventRecord struct {
	ID                    uuid.UUID
	AssistantThreadID     uuid.UUID
	AssistantID           uuid.UUID
	ProjectID             uuid.UUID
	TriggerInstanceID     uuid.NullUUID
	EventID               string
	CorrelationID         string
	Status                string
	NormalizedPayloadJSON []byte
	SourcePayloadJSON     []byte
	Attempts              int
	LastError             sql.NullString
}

// assistantToolsetRow is the hydrated view of a row in assistant_toolsets
// joined with toolsets + environments. Everything dispatch needs to build
// MCP server URLs comes from one read.
type assistantToolsetRow struct {
	ToolsetID              uuid.UUID
	ToolsetSlug            string
	McpEnabled             bool
	McpSlug                sql.NullString
	DefaultEnvironmentSlug sql.NullString
	EnvironmentID          uuid.NullUUID
	EnvironmentSlug        sql.NullString
}

type EnqueueResult struct {
	AssistantID  uuid.UUID
	ThreadID     uuid.UUID
	EventCreated bool
}

type ProcessThreadEventsResult struct {
	AssistantID       uuid.UUID
	WarmUntil         time.Time
	RuntimeActive     bool
	RetryAdmission    bool
	ProcessedAnyEvent bool
}

type ServiceCore struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	runtime         RuntimeBackend
	chatRepo        *chatrepo.Queries
	slackClient     *slackclient.SlackClient
	assistantTokens *assistanttokens.Manager
	serverURL       *url.URL
	telemetryLogger *telemetry.Logger
}

func NewServiceCore(
	logger *slog.Logger,
	db *pgxpool.Pool,
	runtime RuntimeBackend,
	slackClient *slackclient.SlackClient,
	assistantTokens *assistanttokens.Manager,
	serverURL *url.URL,
	telemetryLogger *telemetry.Logger,
) *ServiceCore {
	return &ServiceCore{
		logger:          logger,
		db:              db,
		runtime:         runtime,
		chatRepo:        chatrepo.New(db),
		slackClient:     slackClient,
		assistantTokens: assistantTokens,
		serverURL:       serverURL,
		telemetryLogger: telemetryLogger,
	}
}

// emitAssistantTelemetry writes a single assistant-pipeline log event to the
// telemetry store. All fields are optional except phase/body — emissions at
// pre-event stages (runtime ensure/configure) omit the event-scoped attrs.
// Correlation id is the Slack channel ts in the Slack source case; grouping
// in the UI hangs off it so every execution-side event shares a parent
// with the originating trigger log row.
func (s *ServiceCore) emitAssistantTelemetry(
	ctx context.Context,
	assistant assistantRecord,
	thread assistantThreadRecord,
	runtime *assistantRuntimeRecord,
	event *assistantThreadEventRecord,
	phase string,
	body string,
	severity string,
	runErr error,
) {
	if severity == "" {
		severity = "INFO"
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:          string(telemetry.EventSourceAssistant),
		attr.LogBodyKey:              body,
		attr.LogSeverityKey:          severity,
		attr.AssistantPhaseKey:       phase,
		attr.AssistantIDKey:          assistant.ID.String(),
		attr.AssistantThreadIDKey:    thread.ID.String(),
		attr.TriggerCorrelationIDKey: thread.CorrelationID,
	}
	if runtime != nil {
		attrs[attr.AssistantRuntimeIDKey] = runtime.ID.String()
		attrs[attr.AssistantRuntimeBackendKey] = runtime.Backend
	}
	if event != nil {
		attrs[attr.AssistantEventIDKey] = event.ID.String()
		attrs[attr.AssistantAttemptKey] = int64(event.Attempts)
		attrs[attr.TriggerEventIDKey] = event.EventID
		if event.CorrelationID != "" {
			attrs[attr.TriggerCorrelationIDKey] = event.CorrelationID
		}
		if event.TriggerInstanceID.Valid {
			attrs[attr.TriggerInstanceIDKey] = event.TriggerInstanceID.UUID.String()
		}
	}
	if runErr != nil {
		attrs[attr.ErrorMessageKey] = runErr.Error()
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: time.Now().UTC(),
		ToolInfo: telemetry.ToolInfo{
			ID:             assistant.ID.String(),
			URN:            "urn:uuid:" + assistant.ID.String(),
			Name:           "assistant:" + assistant.Name,
			ProjectID:      assistant.ProjectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: assistant.OrganizationID,
		},
		Attributes: attrs,
	})
}

// ReapStuckRuntimesResult summarises one reap sweep for observability.
type ReapStuckRuntimesResult struct {
	StaleRuntimesStopped int64
	StaleEventsRequeued  int64
	// AffectedAssistantIDs lists distinct assistants whose events were
	// requeued or whose runtimes were stopped. The singleton reaper uses
	// these to kick the per-assistant coordinators so admit re-runs
	// promptly instead of waiting for organic traffic.
	AffectedAssistantIDs []uuid.UUID
}

// ReapStuckRuntimes releases resources that the happy-path has no way to
// reclaim — rows written by a worker/server that later died mid-turn and
// events claimed by a runtime that no longer exists. Safe to run
// concurrently with admit/processing: uses targeted WHERE clauses rather
// than locks so a live turn is never interrupted.
func (s *ServiceCore) ReapStuckRuntimes(ctx context.Context) (ReapStuckRuntimesResult, error) {
	var out ReapStuckRuntimesResult
	now := time.Now().UTC()
	affected := map[uuid.UUID]struct{}{}

	// 1. Retire runtime rows whose liveness markers indicate the owning
	// process is gone:
	//   - 'starting' rows that never transitioned to active within the
	//     startup grace window (usually server crashed mid-boot).
	//   - 'active' rows whose warm_until passed a grace window ago (usually
	//     server crashed after a turn; unexpected-exit callback didn't fire
	//     because the whole process died).
	runtimeRows, err := s.db.Query(ctx, `
UPDATE assistant_runtimes
SET
  state = $1,
  updated_at = clock_timestamp(),
  deleted_at = clock_timestamp()
WHERE deleted IS FALSE
  AND (
    (state = $2 AND updated_at < $4)
    OR (
      state = $3
      AND warm_until IS NOT NULL
      AND warm_until < $5
      AND COALESCE(last_heartbeat_at, updated_at) < $6
    )
  )
RETURNING assistant_id
`, runtimeStateStopped, runtimeStateStarting, runtimeStateActive, now.Add(-runtimeStartupReapGrace), now.Add(-runtimeWarmExpiryReapGrace), now.Add(-runtimeProcessingLeaseGrace))
	if err != nil {
		return out, fmt.Errorf("reap stuck assistant runtimes: %w", err)
	}
	for runtimeRows.Next() {
		var assistantID uuid.UUID
		if err := runtimeRows.Scan(&assistantID); err != nil {
			runtimeRows.Close()
			return out, fmt.Errorf("scan reaped runtime assistant id: %w", err)
		}
		affected[assistantID] = struct{}{}
		out.StaleRuntimesStopped++
	}
	runtimeRows.Close()
	if err := runtimeRows.Err(); err != nil {
		return out, fmt.Errorf("iterate reaped runtimes: %w", err)
	}

	// 2. Re-queue events that were claimed but never completed — either the
	// worker crashed mid-turn, or we intentionally left the event in
	// 'processing' after an ErrRuntimeUnhealthy bailout so the next admit
	// cycle can re-deliver it under a fresh VM.
	eventRows, err := s.db.Query(ctx, `
UPDATE assistant_thread_events
SET
  status = $1,
  updated_at = clock_timestamp()
WHERE deleted IS FALSE
  AND status = $2
  AND updated_at < $3
RETURNING assistant_id
`, eventStatusPending, eventStatusProcessing, now.Add(-eventProcessingRequeueGrace))
	if err != nil {
		return out, fmt.Errorf("reap stuck assistant thread events: %w", err)
	}
	for eventRows.Next() {
		var assistantID uuid.UUID
		if err := eventRows.Scan(&assistantID); err != nil {
			eventRows.Close()
			return out, fmt.Errorf("scan requeued event assistant id: %w", err)
		}
		affected[assistantID] = struct{}{}
		out.StaleEventsRequeued++
	}
	eventRows.Close()
	if err := eventRows.Err(); err != nil {
		return out, fmt.Errorf("iterate requeued events: %w", err)
	}

	if len(affected) > 0 {
		out.AffectedAssistantIDs = make([]uuid.UUID, 0, len(affected))
		for id := range affected {
			out.AffectedAssistantIDs = append(out.AffectedAssistantIDs, id)
		}
	}

	return out, nil
}

// HandleUnexpectedRuntimeExit is invoked by the runtime backend when a VM
// terminates without a Stop() call. Marks the DB runtime row stopped and
// soft-deletes it so admit can re-provision a fresh row on the next event;
// without this the partial unique index on (assistant_thread_id) WHERE
// deleted IS FALSE AND ended IS FALSE silently blocks admit's ON CONFLICT
// DO NOTHING insert and the thread wedges.
func (s *ServiceCore) HandleUnexpectedRuntimeExit(threadID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	projectID, err := s.resolveThreadProjectID(ctx, threadID)
	if err != nil {
		s.logger.ErrorContext(ctx, "resolve assistant thread project after unexpected exit failed",
			slogThreadID(threadID.String()),
			attr.SlogError(err),
		)
		return
	}
	if err := s.stopRuntimeRecord(ctx, projectID, threadID, runtimeStateStopped); err != nil {
		s.logger.ErrorContext(ctx, "reconcile assistant runtime after unexpected exit failed",
			slogThreadID(threadID.String()),
			attr.SlogError(err),
		)
	}
}

func (s *ServiceCore) resolveThreadProjectID(ctx context.Context, threadID uuid.UUID) (uuid.UUID, error) {
	var projectID uuid.UUID
	if err := s.db.QueryRow(ctx, `
SELECT project_id FROM assistant_threads WHERE id = $1
`, threadID).Scan(&projectID); err != nil {
		return uuid.Nil, fmt.Errorf("resolve assistant thread project id: %w", err)
	}
	return projectID, nil
}

func normalizeWarmTTLSeconds(v *int) int {
	if v == nil || *v < 0 {
		return DefaultWarmTTLSeconds
	}
	return *v
}

func normalizeMaxConcurrency(v *int) int {
	if v == nil || *v < 1 {
		return DefaultMaxConcurrency
	}
	return *v
}

func normalizeStatus(v *string) string {
	if v == nil || *v == "" {
		return StatusActive
	}
	return *v
}

// resolvedToolsetInsert captures the FK values we need to write one row in
// assistant_toolsets for a single (toolset_slug, environment_slug?) ref
// supplied by the user.
type resolvedToolsetInsert struct {
	ToolsetID     uuid.UUID
	EnvironmentID uuid.NullUUID
}

// resolveToolsetRefsForWrite validates that every user-supplied slug exists
// within the project and returns the FK ids to persist. Failing fast here
// turns silent dispatch-time errors ("unknown toolset") into 400s at
// create/update time.
func (s *ServiceCore) resolveToolsetRefsForWrite(
	ctx context.Context,
	projectID uuid.UUID,
	refs []*types.AssistantToolsetRef,
) ([]resolvedToolsetInsert, error) {
	if len(refs) == 0 {
		return nil, nil
	}

	toolsetSlugs := make([]string, 0, len(refs))
	envSlugs := make([]string, 0, len(refs))
	seenToolsetSlug := map[string]struct{}{}
	seenEnvSlug := map[string]struct{}{}
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		if _, ok := seenToolsetSlug[ref.ToolsetSlug]; !ok {
			seenToolsetSlug[ref.ToolsetSlug] = struct{}{}
			toolsetSlugs = append(toolsetSlugs, ref.ToolsetSlug)
		}
		if ref.EnvironmentSlug != nil && *ref.EnvironmentSlug != "" {
			if _, ok := seenEnvSlug[*ref.EnvironmentSlug]; !ok {
				seenEnvSlug[*ref.EnvironmentSlug] = struct{}{}
				envSlugs = append(envSlugs, *ref.EnvironmentSlug)
			}
		}
	}

	toolsetIDs := map[string]uuid.UUID{}
	rows, err := s.db.Query(ctx, `
SELECT id, slug FROM toolsets WHERE project_id = $1 AND slug = ANY($2) AND deleted IS FALSE
`, projectID, toolsetSlugs)
	if err != nil {
		return nil, fmt.Errorf("resolve toolset slugs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, fmt.Errorf("scan toolset ref: %w", err)
		}
		toolsetIDs[slug] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate toolset refs: %w", err)
	}
	for _, slug := range toolsetSlugs {
		if _, ok := toolsetIDs[slug]; !ok {
			return nil, fmt.Errorf("toolset %q not found in project", slug)
		}
	}

	envIDs := map[string]uuid.UUID{}
	if len(envSlugs) > 0 {
		rows, err := s.db.Query(ctx, `
SELECT id, slug FROM environments WHERE project_id = $1 AND slug = ANY($2) AND deleted IS FALSE
`, projectID, envSlugs)
		if err != nil {
			return nil, fmt.Errorf("resolve environment slugs: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var slug string
			if err := rows.Scan(&id, &slug); err != nil {
				return nil, fmt.Errorf("scan environment ref: %w", err)
			}
			envIDs[slug] = id
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate environment refs: %w", err)
		}
		for _, slug := range envSlugs {
			if _, ok := envIDs[slug]; !ok {
				return nil, fmt.Errorf("environment %q not found in project", slug)
			}
		}
	}

	out := make([]resolvedToolsetInsert, 0, len(refs))
	seenPair := map[uuid.UUID]struct{}{}
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		toolsetID := toolsetIDs[ref.ToolsetSlug]
		if _, dup := seenPair[toolsetID]; dup {
			return nil, fmt.Errorf("toolset %q listed more than once", ref.ToolsetSlug)
		}
		seenPair[toolsetID] = struct{}{}
		var envID uuid.NullUUID
		if ref.EnvironmentSlug != nil && *ref.EnvironmentSlug != "" {
			envID = uuid.NullUUID{UUID: envIDs[*ref.EnvironmentSlug], Valid: true}
		}
		out = append(out, resolvedToolsetInsert{ToolsetID: toolsetID, EnvironmentID: envID})
	}
	return out, nil
}

// loadAssistantToolsets pulls the hydrated toolset rows for one or more
// assistants in a single query so callers can attach them without N+1.
func (s *ServiceCore) loadAssistantToolsets(ctx context.Context, assistantIDs []uuid.UUID) (map[uuid.UUID][]assistantToolsetRow, error) {
	out := map[uuid.UUID][]assistantToolsetRow{}
	if len(assistantIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
SELECT
  at.assistant_id, at.toolset_id, t.slug, t.mcp_enabled, t.mcp_slug, t.default_environment_slug,
  at.environment_id, e.slug
FROM assistant_toolsets at
JOIN toolsets t ON t.id = at.toolset_id
LEFT JOIN environments e ON e.id = at.environment_id
WHERE at.assistant_id = ANY($1)
ORDER BY at.created_at
`, assistantIDs)
	if err != nil {
		return nil, fmt.Errorf("load assistant toolsets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var assistantID uuid.UUID
		var row assistantToolsetRow
		if err := rows.Scan(
			&assistantID,
			&row.ToolsetID,
			&row.ToolsetSlug,
			&row.McpEnabled,
			&row.McpSlug,
			&row.DefaultEnvironmentSlug,
			&row.EnvironmentID,
			&row.EnvironmentSlug,
		); err != nil {
			return nil, fmt.Errorf("scan assistant toolset: %w", err)
		}
		out[assistantID] = append(out[assistantID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assistant toolsets: %w", err)
	}
	return out, nil
}

// writeAssistantToolsets replaces the assistant's toolset membership with
// the resolved set. Caller-supplied tx so create + update can share the
// same atomic boundary as the assistant row write.
func writeAssistantToolsets(
	ctx context.Context,
	tx pgx.Tx,
	assistantID, projectID uuid.UUID,
	resolved []resolvedToolsetInsert,
) error {
	if _, err := tx.Exec(ctx, `DELETE FROM assistant_toolsets WHERE assistant_id = $1`, assistantID); err != nil {
		return fmt.Errorf("clear assistant toolsets: %w", err)
	}
	if len(resolved) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(resolved))
	for _, r := range resolved {
		rows = append(rows, []any{assistantID, r.ToolsetID, r.EnvironmentID, projectID})
	}
	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"assistant_toolsets"},
		[]string{"assistant_id", "toolset_id", "environment_id", "project_id"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("insert assistant toolsets: %w", err)
	}
	return nil
}

func deterministicChatID(assistantID uuid.UUID, correlationID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("assistant-thread:"+assistantID.String()+":"+correlationID))
}

func toHTTPAssistant(record assistantRecord) (*types.Assistant, error) {
	toolsets := make([]*types.AssistantToolsetRef, 0, len(record.Toolsets))
	for _, row := range record.Toolsets {
		ref := &types.AssistantToolsetRef{
			ToolsetSlug:     row.ToolsetSlug,
			EnvironmentSlug: nil,
		}
		if row.EnvironmentSlug.Valid {
			envSlug := row.EnvironmentSlug.String
			ref.EnvironmentSlug = &envSlug
		}
		toolsets = append(toolsets, ref)
	}
	return &types.Assistant{
		ID:             record.ID.String(),
		ProjectID:      record.ProjectID.String(),
		Name:           record.Name,
		Model:          record.Model,
		Instructions:   record.Instructions,
		Toolsets:       toolsets,
		WarmTTLSeconds: record.WarmTTLSeconds,
		MaxConcurrency: record.MaxConcurrency,
		Status:         record.Status,
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *ServiceCore) CreateAssistant(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	name string,
	model string,
	instructions string,
	toolsets []*types.AssistantToolsetRef,
	warmTTLSeconds int,
	maxConcurrency int,
	status string,
) (assistantRecord, error) {
	resolved, err := s.resolveToolsetRefsForWrite(ctx, projectID, toolsets)
	if err != nil {
		return assistantRecord{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var record assistantRecord
	if err := tx.QueryRow(ctx, `
INSERT INTO assistants (
  project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
`, projectID, organizationID, name, model, instructions, warmTTLSeconds, maxConcurrency, status).Scan(
		&record.ID,
		&record.ProjectID,
		&record.OrganizationID,
		&record.Name,
		&record.Model,
		&record.Instructions,
		&record.WarmTTLSeconds,
		&record.MaxConcurrency,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.DeletedAt,
	); err != nil {
		return assistantRecord{}, fmt.Errorf("insert assistant: %w", err)
	}

	if err := writeAssistantToolsets(ctx, tx, record.ID, projectID, resolved); err != nil {
		return assistantRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	refs, err := s.loadAssistantToolsets(ctx, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) ListAssistants(ctx context.Context, projectID uuid.UUID) ([]assistantRecord, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE project_id = $1 AND deleted IS FALSE
ORDER BY created_at DESC
`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query assistants: %w", err)
	}
	defer rows.Close()

	out := []assistantRecord{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var record assistantRecord
		if err := rows.Scan(
			&record.ID,
			&record.ProjectID,
			&record.OrganizationID,
			&record.Name,
			&record.Model,
			&record.Instructions,
			&record.WarmTTLSeconds,
			&record.MaxConcurrency,
			&record.Status,
			&record.CreatedAt,
			&record.UpdatedAt,
			&record.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan assistant: %w", err)
		}
		out = append(out, record)
		ids = append(ids, record.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assistants: %w", err)
	}

	refs, err := s.loadAssistantToolsets(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Toolsets = refs[out[i].ID]
	}
	return out, nil
}

func (s *ServiceCore) GetAssistant(ctx context.Context, projectID uuid.UUID, assistantID uuid.UUID) (assistantRecord, error) {
	var record assistantRecord
	err := s.db.QueryRow(ctx, `
SELECT id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = $1 AND project_id = $2 AND deleted IS FALSE
`, assistantID, projectID).Scan(
		&record.ID,
		&record.ProjectID,
		&record.OrganizationID,
		&record.Name,
		&record.Model,
		&record.Instructions,
		&record.WarmTTLSeconds,
		&record.MaxConcurrency,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.DeletedAt,
	)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("select assistant: %w", err)
	}
	refs, err := s.loadAssistantToolsets(ctx, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) getAssistantForDispatch(ctx context.Context, assistantID uuid.UUID) (assistantRecord, error) {
	var record assistantRecord
	err := s.db.QueryRow(ctx, `
SELECT id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = $1 AND deleted IS FALSE
`, assistantID).Scan(
		&record.ID,
		&record.ProjectID,
		&record.OrganizationID,
		&record.Name,
		&record.Model,
		&record.Instructions,
		&record.WarmTTLSeconds,
		&record.MaxConcurrency,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.DeletedAt,
	)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("select assistant for dispatch: %w", err)
	}
	refs, err := s.loadAssistantToolsets(ctx, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) UpdateAssistant(
	ctx context.Context,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	name *string,
	model *string,
	instructions *string,
	toolsets []*types.AssistantToolsetRef,
	warmTTLSeconds *int,
	maxConcurrency *int,
	status *string,
) (assistantRecord, error) {
	var resolved []resolvedToolsetInsert
	if toolsets != nil {
		r, err := s.resolveToolsetRefsForWrite(ctx, projectID, toolsets)
		if err != nil {
			return assistantRecord{}, err
		}
		resolved = r
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var record assistantRecord
	if err := tx.QueryRow(ctx, `
UPDATE assistants
SET
  name = COALESCE($1, name),
  model = COALESCE($2, model),
  instructions = COALESCE($3, instructions),
  warm_ttl_seconds = COALESCE($4, warm_ttl_seconds),
  max_concurrency = COALESCE($5, max_concurrency),
  status = COALESCE($6, status),
  updated_at = clock_timestamp()
WHERE id = $7 AND project_id = $8 AND deleted IS FALSE
RETURNING id, project_id, organization_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
`, name, model, instructions, warmTTLSeconds, maxConcurrency, status, assistantID, projectID).Scan(
		&record.ID,
		&record.ProjectID,
		&record.OrganizationID,
		&record.Name,
		&record.Model,
		&record.Instructions,
		&record.WarmTTLSeconds,
		&record.MaxConcurrency,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.DeletedAt,
	); err != nil {
		return assistantRecord{}, fmt.Errorf("update assistant: %w", err)
	}

	if toolsets != nil {
		if err := writeAssistantToolsets(ctx, tx, record.ID, projectID, resolved); err != nil {
			return assistantRecord{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	refs, err := s.loadAssistantToolsets(ctx, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) DeleteAssistant(ctx context.Context, projectID uuid.UUID, assistantID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistants
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = $1 AND project_id = $2 AND deleted IS FALSE
`, assistantID, projectID)
	if err != nil {
		return fmt.Errorf("delete assistant: %w", err)
	}
	return nil
}

func (s *ServiceCore) EnqueueTriggerTask(ctx context.Context, task bgtriggers.Task) (EnqueueResult, error) {
	assistantID, err := uuid.Parse(task.TargetRef)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("parse assistant id: %w", err)
	}
	assistant, err := s.getAssistantForDispatch(ctx, assistantID)
	if err != nil {
		return EnqueueResult{}, err
	}
	if assistant.Status != StatusActive {
		return EnqueueResult{
			AssistantID:  assistant.ID,
			ThreadID:     uuid.Nil,
			EventCreated: false,
		}, nil
	}

	sourceKind, sourceRefJSON, normalizedPayloadJSON, sourcePayloadJSON, err := buildAssistantEventPayload(task)
	if err != nil {
		return EnqueueResult{}, err
	}
	chatID := deterministicChatID(assistant.ID, task.CorrelationID)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("begin assistant enqueue tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at)
VALUES ($1, $2, $3, NULL, NULL, $4, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id
`, chatID, assistant.ProjectID, assistant.OrganizationID, assistant.Name); err != nil {
		return EnqueueResult{}, fmt.Errorf("upsert assistant chat: %w", err)
	}

	var threadID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO assistant_threads (
  assistant_id, project_id, correlation_id, chat_id, source_kind, source_ref_json, last_event_at
) VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp())
ON CONFLICT (project_id, assistant_id, correlation_id) WHERE deleted IS FALSE
DO UPDATE SET
  source_ref_json = EXCLUDED.source_ref_json,
  last_event_at = clock_timestamp(),
  updated_at = clock_timestamp()
RETURNING id
`, assistant.ID, assistant.ProjectID, task.CorrelationID, chatID, sourceKind, sourceRefJSON).Scan(&threadID)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("upsert assistant thread: %w", err)
	}

	var eventCreated bool
	var insertedID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO assistant_thread_events (
  assistant_thread_id, assistant_id, project_id, trigger_instance_id, event_id, correlation_id,
  status, normalized_payload_json, source_payload_json
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (project_id, assistant_id, event_id) WHERE deleted IS FALSE DO NOTHING
RETURNING id
`, threadID, assistant.ID, assistant.ProjectID, nullableUUID(task.TriggerInstanceID), task.EventID, task.CorrelationID, eventStatusPending, normalizedPayloadJSON, sourcePayloadJSON).Scan(&insertedID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		eventCreated = false
	case err != nil:
		return EnqueueResult{}, fmt.Errorf("insert assistant thread event: %w", err)
	default:
		eventCreated = true
	}

	if err := tx.Commit(ctx); err != nil {
		return EnqueueResult{}, fmt.Errorf("commit assistant enqueue tx: %w", err)
	}

	return EnqueueResult{
		AssistantID:  assistant.ID,
		ThreadID:     threadID,
		EventCreated: eventCreated,
	}, nil
}

func buildAssistantEventPayload(task bgtriggers.Task) (string, []byte, []byte, []byte, error) {
	switch task.DefinitionSlug {
	case "slack":
		var event slackEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode slack trigger event: %w", err)
		}
		if event.ThreadID == "" {
			event.ThreadID = event.Timestamp
		}
		sourceRefJSON, err := json.Marshal(slackSourceRef{
			TeamID:    event.TeamID,
			ChannelID: event.ChannelID,
			ThreadID:  event.ThreadID,
			UserID:    event.UserID,
		})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal slack source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindSlack, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
	case sourceKindCron:
		var event cronEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode cron trigger event: %w", err)
		}
		sourceRefJSON, err := json.Marshal(cronSourceRef{
			TriggerInstanceID: event.TriggerInstanceID,
			Schedule:          event.Schedule,
		})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal cron source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindCron, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
	default:
		return "", nil, nil, nil, fmt.Errorf("assistant source %q is not supported", task.DefinitionSlug)
	}
}

func nullableUUID(raw string) uuid.NullUUID {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}

func (s *ServiceCore) AdmitPendingThreads(ctx context.Context, assistantID uuid.UUID) ([]uuid.UUID, error) {
	assistant, err := s.getAssistantForDispatch(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	if assistant.Status != StatusActive {
		return nil, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin assistant admit tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	warmRows, err := tx.Query(ctx, `
SELECT DISTINCT t.id
FROM assistant_threads t
JOIN assistant_runtimes r
  ON r.assistant_thread_id = t.id
  AND r.project_id = t.project_id
WHERE t.project_id = $1
  AND t.assistant_id = $2
  AND t.deleted IS FALSE
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.state = $3
  AND (r.warm_until IS NULL OR r.warm_until > clock_timestamp())
  AND EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = $4
  )
`, assistant.ProjectID, assistantID, runtimeStateActive, eventStatusPending)
	if err != nil {
		return nil, fmt.Errorf("query warm assistant threads: %w", err)
	}
	defer warmRows.Close()

	admitted := []uuid.UUID{}
	for warmRows.Next() {
		var threadID uuid.UUID
		if err := warmRows.Scan(&threadID); err != nil {
			return nil, fmt.Errorf("scan warm assistant thread: %w", err)
		}
		admitted = append(admitted, threadID)
	}
	if err := warmRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate warm assistant threads: %w", err)
	}

	var activeCount int
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)
FROM assistant_runtimes
WHERE project_id = $1
  AND assistant_id = $2
  AND deleted IS FALSE
  AND ended IS FALSE
  AND (
    state = $3
    OR (state = $4 AND (warm_until IS NULL OR warm_until > clock_timestamp()))
  )
`, assistant.ProjectID, assistantID, runtimeStateStarting, runtimeStateActive).Scan(&activeCount); err != nil {
		return nil, fmt.Errorf("count active assistant runtimes: %w", err)
	}

	available := max(assistant.MaxConcurrency-activeCount, 0)
	if available > 0 {
		rows, err := tx.Query(ctx, `
SELECT t.id, t.project_id
FROM assistant_threads t
WHERE t.project_id = $1
  AND t.assistant_id = $2
  AND t.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = $3
  )
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_runtimes r
    WHERE r.project_id = t.project_id
      AND r.assistant_thread_id = t.id
      AND r.deleted IS FALSE
      AND r.ended IS FALSE
      AND (
        r.state = $4
        OR (r.state = $5 AND (r.warm_until IS NULL OR r.warm_until > clock_timestamp()))
      )
  )
ORDER BY (
  SELECT MIN(e.created_at)
  FROM assistant_thread_events e
  WHERE e.project_id = t.project_id
    AND e.assistant_thread_id = t.id
    AND e.deleted IS FALSE
    AND e.status = $3
) ASC
LIMIT $6
FOR UPDATE OF t SKIP LOCKED
`, assistant.ProjectID, assistantID, eventStatusPending, runtimeStateStarting, runtimeStateActive, available)
		if err != nil {
			return nil, fmt.Errorf("select cold assistant threads: %w", err)
		}

		type coldThread struct {
			threadID  uuid.UUID
			projectID uuid.UUID
		}
		coldThreads := make([]coldThread, 0, available)
		for rows.Next() {
			var threadID uuid.UUID
			var projectID uuid.UUID
			if err := rows.Scan(&threadID, &projectID); err != nil {
				return nil, fmt.Errorf("scan cold assistant thread: %w", err)
			}
			coldThreads = append(coldThreads, coldThread{
				threadID:  threadID,
				projectID: projectID,
			})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate cold assistant threads: %w", err)
		}

		for _, coldThread := range coldThreads {
			if _, err := tx.Exec(ctx, `
INSERT INTO assistant_runtimes (
  assistant_thread_id, assistant_id, project_id, backend, state, backend_metadata_json
) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb)
ON CONFLICT DO NOTHING
`, coldThread.threadID, assistantID, coldThread.projectID, s.runtime.Backend(), runtimeStateStarting); err != nil {
				return nil, fmt.Errorf("reserve assistant runtime: %w", err)
			}
			admitted = append(admitted, coldThread.threadID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit assistant admit tx: %w", err)
	}
	return admitted, nil
}

func (s *ServiceCore) ProcessThreadEvents(ctx context.Context, threadID uuid.UUID) (ProcessThreadEventsResult, error) {
	thread, assistant, runtimeRecord, err := s.loadThreadContext(ctx, threadID)
	if err != nil {
		return ProcessThreadEventsResult{}, err
	}

	ctx = withAssistantLogContext(ctx, assistantLogContext{
		OrganizationID:    assistant.OrganizationID,
		ProjectID:         assistant.ProjectID.String(),
		AssistantID:       assistant.ID.String(),
		AssistantName:     assistant.Name,
		ThreadID:          thread.ID.String(),
		CorrelationID:     thread.CorrelationID,
		RuntimeID:         runtimeRecord.ID.String(),
		RuntimeBackend:    runtimeRecord.Backend,
		EventID:           "",
		TriggerEventID:    "",
		TriggerInstanceID: "",
		Attempt:           0,
	})

	ensureResult, err := s.runtime.Ensure(ctx, runtimeRecord)
	if err != nil {
		// Ensure failed: mark the runtime row failed so the coordinator's
		// admit query can re-admit this thread, and return RetryAdmission
		// rather than propagating an error. Propagating would trigger activity
		// retries against a now-soft-deleted runtime row (loadThreadContext
		// would return no rows) and burn the workflow's retry budget.
		s.logger.ErrorContext(ctx, "ensure assistant runtime failed", slogThreadID(thread.ID.String()), attr.SlogError(err))
		_ = s.stopRuntimeRecord(ctx, thread.ProjectID, thread.ID, runtimeStateFailed)
		return ProcessThreadEventsResult{
			AssistantID:       assistant.ID,
			WarmUntil:         time.Time{},
			RuntimeActive:     false,
			RetryAdmission:    true,
			ProcessedAnyEvent: false,
		}, nil
	}
	if err := s.updateRuntimeEnsureResult(ctx, &runtimeRecord, ensureResult); err != nil {
		return ProcessThreadEventsResult{}, err
	}

	coldStart := ensureResult.ColdStart
	if ensureResult.NeedsConfigure {
		startupConfig, err := s.buildRuntimeStartupConfig(ctx, thread, runtimeRecord, assistant)
		if err != nil {
			s.logger.ErrorContext(ctx, "build runtime startup config failed", slogThreadID(thread.ID.String()), attr.SlogError(err))
			_ = s.runtime.Stop(ctx, runtimeRecord)
			_ = s.stopRuntimeRecord(ctx, thread.ProjectID, thread.ID, runtimeStateFailed)
			return ProcessThreadEventsResult{
				AssistantID:       assistant.ID,
				WarmUntil:         time.Time{},
				RuntimeActive:     false,
				RetryAdmission:    true,
				ProcessedAnyEvent: false,
			}, nil
		}
		if err := s.runtime.Configure(ctx, runtimeRecord, startupConfig); err != nil {
			s.logger.ErrorContext(ctx, "configure assistant runtime failed", slogThreadID(thread.ID.String()), attr.SlogError(err))
			_ = s.runtime.Stop(ctx, runtimeRecord)
			_ = s.stopRuntimeRecord(ctx, thread.ProjectID, thread.ID, runtimeStateFailed)
			return ProcessThreadEventsResult{
				AssistantID:       assistant.ID,
				WarmUntil:         time.Time{},
				RuntimeActive:     false,
				RetryAdmission:    true,
				ProcessedAnyEvent: false,
			}, nil
		}
	}

	if runtimeRecord.State == runtimeStateStarting {
		if err := s.setRuntimeActive(ctx, runtimeRecord.ID, time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds)*time.Second)); err != nil {
			return ProcessThreadEventsResult{}, err
		}
	}

	processedAny := false
	for {
		event, ok, err := s.claimNextPendingEvent(ctx, thread.ProjectID, thread.ID)
		if err != nil {
			return ProcessThreadEventsResult{}, err
		}
		if !ok {
			break
		}

		turnCtx := withAssistantLogEvent(ctx, event)
		s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "turn_start", "assistant turn started", "INFO", nil)

		stopLeaseHeartbeat := s.startProcessingLeaseHeartbeat(turnCtx, runtimeRecord.ID, event.ID)
		runErr := s.processEventTurn(turnCtx, thread, assistant, runtimeRecord, event, coldStart)
		stopLeaseHeartbeat()
		if runErr != nil {
			s.logger.WarnContext(ctx, "assistant turn failed",
				slogThreadID(thread.ID.String()),
				slogEventID(event.ID.String()),
				slogAttempt(event.Attempts),
				attr.SlogError(runErr),
			)
			s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "turn_failed", "assistant turn failed", "ERROR", runErr)

			// Runtime-level failure (dead VM, connection refused, missing
			// state). Tear down the runtime row and leave the event in
			// 'processing' — do NOT reset it to 'pending', or the outer
			// workflow retry will hammer the dead VM with duplicate turns.
			// A reaper reclaims stuck 'processing' events after a grace
			// window so they flow through cleanly under a fresh VM.
			if errors.Is(runErr, ErrRuntimeUnhealthy) {
				_ = s.runtime.Stop(ctx, runtimeRecord)
				_ = s.stopRuntimeRecord(ctx, thread.ProjectID, thread.ID, runtimeStateStopped)
				return ProcessThreadEventsResult{
					AssistantID:       assistant.ID,
					WarmUntil:         time.Time{},
					RuntimeActive:     false,
					RetryAdmission:    true,
					ProcessedAnyEvent: processedAny,
				}, nil
			}

			// Terminal failure after maxEventAttempts — stop retrying this
			// event. The warm runtime stays up for subsequent events.
			if event.Attempts >= maxEventAttempts {
				s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant event exceeded max attempts", "ERROR", runErr)
				if err := s.failEvent(ctx, event.ID, fmt.Errorf("exceeded %d attempts: %w", maxEventAttempts, runErr)); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
				if err := s.setRuntimeActive(ctx, runtimeRecord.ID, warmUntil); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				return ProcessThreadEventsResult{
					AssistantID:       assistant.ID,
					WarmUntil:         warmUntil,
					RuntimeActive:     true,
					RetryAdmission:    false,
					ProcessedAnyEvent: processedAny,
				}, nil
			}
			// Transient turn-level failure (LLM 5xx, MCP blip) — reset event,
			// keep the warm runtime, let the coordinator re-kick on the next
			// admit cycle.
			s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_requeued", "assistant event requeued for retry", "WARN", runErr)
			if err := s.resetEventToPending(ctx, event.ID, runErr); err != nil {
				return ProcessThreadEventsResult{}, err
			}
			warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
			if err := s.setRuntimeActive(ctx, runtimeRecord.ID, warmUntil); err != nil {
				return ProcessThreadEventsResult{}, err
			}
			return ProcessThreadEventsResult{
				AssistantID:       assistant.ID,
				WarmUntil:         warmUntil,
				RuntimeActive:     true,
				RetryAdmission:    true,
				ProcessedAnyEvent: processedAny,
			}, nil
		}

		if err := s.completeEvent(ctx, event.ID); err != nil {
			return ProcessThreadEventsResult{}, err
		}
		s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_completed", "assistant event completed", "INFO", nil)
		processedAny = true
		coldStart = false
	}

	warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
	if err := s.setRuntimeActive(ctx, runtimeRecord.ID, warmUntil); err != nil {
		return ProcessThreadEventsResult{}, err
	}
	return ProcessThreadEventsResult{
		AssistantID:       assistant.ID,
		WarmUntil:         warmUntil,
		RuntimeActive:     true,
		RetryAdmission:    false,
		ProcessedAnyEvent: processedAny,
	}, nil
}

func (s *ServiceCore) processEventTurn(
	ctx context.Context,
	thread assistantThreadRecord,
	assistant assistantRecord,
	runtime assistantRuntimeRecord,
	event assistantThreadEventRecord,
	includeHistory bool,
) error {
	adapter, err := getSourceAdapter(thread.SourceKind)
	if err != nil {
		return err
	}
	prompt, err := adapter.DecodeTurn(event)
	if err != nil {
		return fmt.Errorf("decode assistant turn: %w", err)
	}
	var history []runtimeMessage
	if includeHistory {
		history, err = s.loadChatHistory(ctx, thread.ChatID, thread.ProjectID)
		if err != nil {
			return err
		}
	}
	turnToken, err := s.mintAssistantRuntimeToken(assistant, thread)
	if err != nil {
		return err
	}
	if err := s.runtime.RunTurn(ctx, runtime, event.ID.String(), turnToken, history, prompt); err != nil {
		return fmt.Errorf("run assistant turn: %w", err)
	}
	return nil
}

func (s *ServiceCore) startProcessingLeaseHeartbeat(
	ctx context.Context,
	runtimeID uuid.UUID,
	eventID uuid.UUID,
) func() {
	//nolint:gosec // cancel is returned and invoked by the caller to stop the heartbeat goroutine
	hbCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(processingLeaseHeartbeatTick)
		defer ticker.Stop()

		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				if err := s.touchProcessingLease(hbCtx, runtimeID, eventID); err != nil && hbCtx.Err() == nil {
					s.logger.WarnContext(hbCtx, "refresh assistant processing lease failed",
						slogRuntimeID(runtimeID.String()),
						slogEventID(eventID.String()),
						attr.SlogError(err),
					)
				}
			}
		}
	}()
	return cancel
}

func (s *ServiceCore) touchProcessingLease(ctx context.Context, runtimeID uuid.UUID, eventID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
WITH touch_runtime AS (
  UPDATE assistant_runtimes
  SET
    last_heartbeat_at = clock_timestamp(),
    updated_at = clock_timestamp()
  WHERE id = $1
    AND deleted IS FALSE
    AND state IN ($3, $4)
)
UPDATE assistant_thread_events
SET updated_at = clock_timestamp()
WHERE id = $2
  AND deleted IS FALSE
  AND status = $5
`, runtimeID, eventID, runtimeStateStarting, runtimeStateActive, eventStatusProcessing)
	if err != nil {
		return fmt.Errorf("touch assistant processing lease: %w", err)
	}
	return nil
}

func (s *ServiceCore) buildRuntimeStartupConfig(
	ctx context.Context,
	thread assistantThreadRecord,
	runtime assistantRuntimeRecord,
	assistant assistantRecord,
) (runtimeStartupConfig, error) {
	token, err := s.mintAssistantRuntimeToken(assistant, thread)
	if err != nil {
		return runtimeStartupConfig{}, err
	}

	runtimeServerURL, err := s.runtime.ServerURL(ctx, runtime, s.serverURL)
	if err != nil {
		return runtimeStartupConfig{}, fmt.Errorf("resolve assistant runtime server URL: %w", err)
	}

	mcpServers, err := resolveAssistantMCPServers(runtimeServerURL, assistant.Toolsets)
	if err != nil {
		return runtimeStartupConfig{}, err
	}

	instructions, err := composeInstructions(assistant.Instructions, thread)
	if err != nil {
		return runtimeStartupConfig{}, fmt.Errorf("compose assistant instructions: %w", err)
	}

	completionsURL := runtimeServerURL.JoinPath("chat", "completions").String()
	return runtimeStartupConfig{
		Model:          assistant.Model,
		Instructions:   optionalString(instructions),
		AuthToken:      token,
		CompletionsURL: &completionsURL,
		ChatID:         thread.ChatID.String(),
		MCPServers:     mcpServers,
	}, nil
}

// mintAssistantRuntimeToken issues the per-thread JWT the runner uses for
// both completions bearer auth and as the dynamic Authorization header stamped
// on every MCP request via its token registry. Scope is tight (thread +
// assistant) and server-side Authorize revokes instantly when the thread or
// assistant is deleted/paused.
func (s *ServiceCore) mintAssistantRuntimeToken(assistant assistantRecord, thread assistantThreadRecord) (string, error) {
	if s.assistantTokens == nil {
		return "", fmt.Errorf("assistant token manager is not configured")
	}
	token, err := s.assistantTokens.Generate(assistanttokens.GenerateInput{
		OrgID:       assistant.OrganizationID,
		ProjectID:   assistant.ProjectID,
		AssistantID: assistant.ID,
		ThreadID:    thread.ID,
		TTL:         assistantRuntimeTokenTTL,
	})
	if err != nil {
		return "", fmt.Errorf("generate assistant execution token: %w", err)
	}
	return token, nil
}

// assistantRuntimeTokenTTL bounds the lifetime of tokens handed to runners.
// Long enough to cover a typical 30-min turn plus bootstrap slack; short
// enough that a leaked token ages out well before the thread retires. Fresh
// tokens are pushed on /configure and on every /turn, so this is the upper
// bound between refreshes for an idle runtime.
const assistantRuntimeTokenTTL = 60 * time.Minute

const outputChannelAddendum = `## Output channel

Your text responses are not delivered to the user. To communicate, call a tool (e.g. post a Slack message, send an email). If no suitable tool is available, the user will not see your reply.`

func composeInstructions(base string, thread assistantThreadRecord) (string, error) {
	adapter, err := getSourceAdapter(thread.SourceKind)
	if err != nil {
		return "", err
	}
	ctxBlock, err := adapter.ThreadContext(thread.SourceRefJSON)
	if err != nil {
		return "", fmt.Errorf("load assistant thread context: %w", err)
	}
	parts := make([]string, 0, 3)
	if base != "" {
		parts = append(parts, base)
	}
	parts = append(parts, outputChannelAddendum)
	if ctxBlock != "" {
		parts = append(parts, ctxBlock)
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveAssistantMCPServers(serverURL *url.URL, toolsets []assistantToolsetRow) ([]runtimeMCPServer, error) {
	if serverURL == nil {
		return nil, fmt.Errorf("assistant runtime server URL is not configured")
	}

	servers := make([]runtimeMCPServer, 0, len(toolsets))
	for _, t := range toolsets {
		if !t.McpEnabled {
			return nil, fmt.Errorf("toolset %q does not have MCP enabled", t.ToolsetSlug)
		}
		if !t.McpSlug.Valid || t.McpSlug.String == "" {
			return nil, fmt.Errorf("toolset %q has no MCP slug", t.ToolsetSlug)
		}

		headers := map[string]string{}
		envSlug := ""
		if t.EnvironmentSlug.Valid {
			envSlug = t.EnvironmentSlug.String
		} else if t.DefaultEnvironmentSlug.Valid {
			envSlug = t.DefaultEnvironmentSlug.String
		}
		if envSlug != "" {
			headers["Gram-Environment"] = envSlug
		}

		servers = append(servers, runtimeMCPServer{
			ID:      t.ToolsetSlug,
			URL:     serverURL.JoinPath("mcp", t.McpSlug.String).String(),
			Headers: headers,
		})
	}

	return servers, nil
}

func (s *ServiceCore) ExpireThreadRuntime(ctx context.Context, threadID uuid.UUID) error {
	projectID, err := s.resolveThreadProjectID(ctx, threadID)
	if err != nil {
		return err
	}
	runtimeRecord, err := s.loadActiveRuntimeRecord(ctx, projectID, threadID)
	if err != nil {
		return err
	}
	if err := s.runtime.Stop(ctx, runtimeRecord); err != nil {
		return fmt.Errorf("stop assistant runtime backend: %w", err)
	}
	if err := s.stopRuntimeRecord(ctx, projectID, threadID, runtimeStateStopped); err != nil {
		return err
	}
	return nil
}

func (s *ServiceCore) loadThreadContext(ctx context.Context, threadID uuid.UUID) (assistantThreadRecord, assistantRecord, assistantRuntimeRecord, error) {
	var thread assistantThreadRecord
	var assistant assistantRecord
	var runtime assistantRuntimeRecord
	err := s.db.QueryRow(ctx, `
SELECT
  t.id, t.assistant_id, t.project_id, t.correlation_id, t.chat_id, t.source_kind, t.source_ref_json, t.last_event_at,
  a.id, a.project_id, a.organization_id, a.name, a.model, a.instructions, a.warm_ttl_seconds, a.max_concurrency, a.status, a.created_at, a.updated_at, a.deleted_at,
  r.id, r.assistant_thread_id, r.assistant_id, r.project_id, r.backend, r.backend_metadata_json, r.state, r.warm_until
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id AND a.project_id = t.project_id
JOIN assistant_runtimes r ON r.assistant_thread_id = t.id AND r.project_id = t.project_id
WHERE t.id = $1
  AND t.deleted IS FALSE
  AND a.deleted IS FALSE
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.state IN ($2, $3)
ORDER BY r.created_at DESC
LIMIT 1
`, threadID, runtimeStateStarting, runtimeStateActive).Scan(
		&thread.ID,
		&thread.AssistantID,
		&thread.ProjectID,
		&thread.CorrelationID,
		&thread.ChatID,
		&thread.SourceKind,
		&thread.SourceRefJSON,
		&thread.LastEventAt,
		&assistant.ID,
		&assistant.ProjectID,
		&assistant.OrganizationID,
		&assistant.Name,
		&assistant.Model,
		&assistant.Instructions,
		&assistant.WarmTTLSeconds,
		&assistant.MaxConcurrency,
		&assistant.Status,
		&assistant.CreatedAt,
		&assistant.UpdatedAt,
		&assistant.DeletedAt,
		&runtime.ID,
		&runtime.AssistantThreadID,
		&runtime.AssistantID,
		&runtime.ProjectID,
		&runtime.Backend,
		&runtime.BackendMetadataJSON,
		&runtime.State,
		&runtime.WarmUntil,
	)
	if err != nil {
		return assistantThreadRecord{}, assistantRecord{}, assistantRuntimeRecord{}, fmt.Errorf("load assistant thread context: %w", err)
	}
	refs, err := s.loadAssistantToolsets(ctx, []uuid.UUID{assistant.ID})
	if err != nil {
		return assistantThreadRecord{}, assistantRecord{}, assistantRuntimeRecord{}, err
	}
	assistant.Toolsets = refs[assistant.ID]
	return thread, assistant, runtime, nil
}

func (s *ServiceCore) loadActiveRuntimeRecord(ctx context.Context, projectID, threadID uuid.UUID) (assistantRuntimeRecord, error) {
	var runtime assistantRuntimeRecord
	err := s.db.QueryRow(ctx, `
SELECT id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, warm_until
FROM assistant_runtimes
WHERE project_id = $1
  AND assistant_thread_id = $2
  AND deleted IS FALSE
  AND ended IS FALSE
  AND state IN ($3, $4)
ORDER BY created_at DESC
LIMIT 1
`, projectID, threadID, runtimeStateStarting, runtimeStateActive).Scan(
		&runtime.ID,
		&runtime.AssistantThreadID,
		&runtime.AssistantID,
		&runtime.ProjectID,
		&runtime.Backend,
		&runtime.BackendMetadataJSON,
		&runtime.State,
		&runtime.WarmUntil,
	)
	if err != nil {
		return assistantRuntimeRecord{}, fmt.Errorf("load active assistant runtime: %w", err)
	}
	return runtime, nil
}

func (s *ServiceCore) loadChatHistory(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID) ([]runtimeMessage, error) {
	messages, err := s.chatRepo.ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list assistant chat messages: %w", err)
	}

	history := make([]runtimeMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "user":
			history = append(history, runtimeMessage{
				Role:       "user",
				Content:    message.Content,
				ToolCalls:  nil,
				ToolCallID: "",
			})
		case "assistant":
			toolCalls, err := decodePersistedToolCalls(message.ToolCalls)
			if err != nil {
				return nil, fmt.Errorf("decode assistant tool calls (seq=%d): %w", message.Seq, err)
			}
			history = append(history, runtimeMessage{
				Role:       "assistant",
				Content:    message.Content,
				ToolCalls:  toolCalls,
				ToolCallID: "",
			})
		case "tool":
			if !message.ToolCallID.Valid || message.ToolCallID.String == "" {
				return nil, fmt.Errorf("tool chat row missing tool_call_id (seq=%d)", message.Seq)
			}
			history = append(history, runtimeMessage{
				Role:       "tool",
				Content:    message.Content,
				ToolCalls:  nil,
				ToolCallID: message.ToolCallID.String,
			})
		case "system":
			// The runner re-injects a fresh system prompt from the assistant's
			// instructions on every cold start. Leaving the persisted system
			// row out of the replay keeps the outgoing /chat/completions
			// request exactly one entry longer than the DB row count (the
			// runner-side system offsets the dropped row here), which is what
			// the capture strategy's length check at
			// chat/message_capture_strategy.go expects.
			continue
		default:
			return nil, fmt.Errorf("unexpected chat message role %q (seq=%d)", message.Role, message.Seq)
		}
	}
	return history, nil
}

// decodePersistedToolCalls unmarshals the JSONB stored by the chat capture
// strategy (json.Marshal over []openrouter.ToolCall) into the wire shape the
// runner expects. Empty/null blobs return an empty slice.
func decodePersistedToolCalls(raw []byte) ([]runtimeToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var decoded []openrouter.ToolCall
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal tool_calls: %w", err)
	}
	out := make([]runtimeToolCall, 0, len(decoded))
	for _, tc := range decoded {
		out = append(out, runtimeToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

func (s *ServiceCore) claimNextPendingEvent(ctx context.Context, projectID, threadID uuid.UUID) (assistantThreadEventRecord, bool, error) {
	var event assistantThreadEventRecord
	var zero assistantThreadEventRecord
	err := s.db.QueryRow(ctx, `
WITH next_event AS (
  SELECT id
  FROM assistant_thread_events
  WHERE project_id = $1
    AND assistant_thread_id = $2
    AND deleted IS FALSE
    AND status = $3
  ORDER BY created_at ASC
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
UPDATE assistant_thread_events e
SET
  status = $4,
  attempts = attempts + 1,
  updated_at = clock_timestamp()
FROM next_event
WHERE e.id = next_event.id
RETURNING e.id, e.assistant_thread_id, e.assistant_id, e.project_id, e.trigger_instance_id, e.event_id, e.correlation_id, e.status, e.normalized_payload_json, e.source_payload_json, e.attempts, e.last_error
`, projectID, threadID, eventStatusPending, eventStatusProcessing).Scan(
		&event.ID,
		&event.AssistantThreadID,
		&event.AssistantID,
		&event.ProjectID,
		&event.TriggerInstanceID,
		&event.EventID,
		&event.CorrelationID,
		&event.Status,
		&event.NormalizedPayloadJSON,
		&event.SourcePayloadJSON,
		&event.Attempts,
		&event.LastError,
	)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("claim assistant thread event: %w", err)
	default:
		return event, true, nil
	}
}

func (s *ServiceCore) completeEvent(ctx context.Context, eventID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistant_thread_events
SET
  status = $1,
  processed_at = clock_timestamp(),
  last_error = NULL,
  updated_at = clock_timestamp()
WHERE id = $2
`, eventStatusCompleted, eventID)
	if err != nil {
		return fmt.Errorf("complete assistant thread event: %w", err)
	}
	return nil
}

func (s *ServiceCore) failEvent(ctx context.Context, eventID uuid.UUID, runErr error) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistant_thread_events
SET
  status = $1,
  last_error = $2,
  updated_at = clock_timestamp()
WHERE id = $3
`, eventStatusFailed, runErr.Error(), eventID)
	if err != nil {
		return fmt.Errorf("fail assistant thread event: %w", err)
	}
	return nil
}

func (s *ServiceCore) resetEventToPending(ctx context.Context, eventID uuid.UUID, runErr error) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistant_thread_events
SET
  status = $1,
  last_error = $2,
  updated_at = clock_timestamp()
WHERE id = $3
`, eventStatusPending, runErr.Error(), eventID)
	if err != nil {
		return fmt.Errorf("reset assistant thread event to pending: %w", err)
	}
	return nil
}

func (s *ServiceCore) setRuntimeActive(ctx context.Context, runtimeID uuid.UUID, warmUntil time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistant_runtimes
SET
  state = $1,
  warm_until = $2,
  last_heartbeat_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = $3
`, runtimeStateActive, warmUntil, runtimeID)
	if err != nil {
		return fmt.Errorf("set assistant runtime active: %w", err)
	}
	return nil
}

func (s *ServiceCore) updateRuntimeEnsureResult(
	ctx context.Context,
	runtime *assistantRuntimeRecord,
	result RuntimeBackendEnsureResult,
) error {
	if runtime == nil {
		return fmt.Errorf("assistant runtime record is not configured")
	}
	if len(result.BackendMetadataJSON) == 0 {
		return nil
	}
	if bytes.Equal(runtime.BackendMetadataJSON, result.BackendMetadataJSON) {
		return nil
	}
	if _, err := s.db.Exec(ctx, `
UPDATE assistant_runtimes
SET
  backend_metadata_json = $1,
  updated_at = clock_timestamp()
WHERE id = $2
`, result.BackendMetadataJSON, runtime.ID); err != nil {
		return fmt.Errorf("update assistant runtime backend metadata: %w", err)
	}
	runtime.BackendMetadataJSON = append([]byte(nil), result.BackendMetadataJSON...)
	return nil
}

func (s *ServiceCore) stopRuntimeRecord(ctx context.Context, projectID, threadID uuid.UUID, state string) error {
	_, err := s.db.Exec(ctx, `
UPDATE assistant_runtimes
SET
  state = $1,
  warm_until = clock_timestamp(),
  updated_at = clock_timestamp(),
  deleted_at = clock_timestamp()
WHERE project_id = $2
  AND assistant_thread_id = $3
  AND deleted IS FALSE
  AND ended IS FALSE
  AND state IN ($4, $5)
`, state, projectID, threadID, runtimeStateStarting, runtimeStateActive)
	if err != nil {
		return fmt.Errorf("stop assistant runtime: %w", err)
	}
	return nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
