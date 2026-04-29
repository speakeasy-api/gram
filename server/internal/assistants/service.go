package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

var errAssistantValidation = errors.New("assistant validation")

func assistantValidationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errAssistantValidation, fmt.Sprintf(format, args...))
}

type assistantRecord struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	OrganizationID  string
	CreatedByUserID string
	Name            string
	Model           string
	Instructions    string
	Toolsets        []assistantToolsetRow
	WarmTTLSeconds  int
	MaxConcurrency  int
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       pgtype.Timestamptz
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
	WarmUntil           pgtype.Timestamptz
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
	LastError             pgtype.Text
}

// assistantToolsetRow is the hydrated view of a row in assistant_toolsets
// joined with toolsets + environments. Everything dispatch needs to build
// MCP server URLs comes from one read.
type assistantToolsetRow struct {
	ToolsetID              uuid.UUID
	ToolsetSlug            string
	McpEnabled             bool
	McpSlug                pgtype.Text
	DefaultEnvironmentSlug pgtype.Text
	EnvironmentID          uuid.NullUUID
	EnvironmentSlug        pgtype.Text
}

func assistantRecordFromCreateRow(row assistantrepo.CreateAssistantRow) assistantRecord {
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

func assistantRecordFromListRow(row assistantrepo.ListAssistantsRow) assistantRecord {
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

func assistantRecordFromGetRow(row assistantrepo.GetAssistantRow) assistantRecord {
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

func assistantRecordFromDispatchRow(row assistantrepo.GetAssistantForDispatchRow) assistantRecord {
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

func assistantRecordFromUpdateRow(row assistantrepo.UpdateAssistantRow) assistantRecord {
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
		runtime:         newTelemetryRuntimeBackend(runtime, telemetryLogger),
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
	queries := assistantrepo.New(s.db)
	runtimeAssistantIDs, err := queries.ReapStuckAssistantRuntimes(ctx, assistantrepo.ReapStuckAssistantRuntimesParams{
		StoppedState:    runtimeStateStopped,
		StartingState:   runtimeStateStarting,
		StartingCutoff:  conv.ToPGTimestamptz(now.Add(-runtimeStartupReapGrace)),
		ActiveState:     runtimeStateActive,
		WarmCutoff:      conv.ToPGTimestamptz(now.Add(-runtimeWarmExpiryReapGrace)),
		HeartbeatCutoff: conv.ToPGTimestamptz(now.Add(-runtimeProcessingLeaseGrace)),
	})
	if err != nil {
		return out, fmt.Errorf("reap stuck assistant runtimes: %w", err)
	}
	for _, assistantID := range runtimeAssistantIDs {
		affected[assistantID] = struct{}{}
		out.StaleRuntimesStopped++
	}

	// 2. Re-queue events that were claimed but never completed — either the
	// worker crashed mid-turn, or we intentionally left the event in
	// 'processing' after an ErrRuntimeUnhealthy bailout so the next admit
	// cycle can re-deliver it under a fresh VM.
	eventAssistantIDs, err := queries.RequeueStaleAssistantEvents(ctx, assistantrepo.RequeueStaleAssistantEventsParams{
		PendingStatus:    eventStatusPending,
		ProcessingStatus: eventStatusProcessing,
		UpdatedBefore:    conv.ToPGTimestamptz(now.Add(-eventProcessingRequeueGrace)),
	})
	if err != nil {
		return out, fmt.Errorf("reap stuck assistant thread events: %w", err)
	}
	for _, assistantID := range eventAssistantIDs {
		affected[assistantID] = struct{}{}
		out.StaleEventsRequeued++
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
// NewUnexpectedRuntimeExitHandler returns a callback suitable for
// RuntimeManagerConfig.OnUnexpectedExit. It only needs the db pool and a
// logger, so it can be wired at deps.go time without creating an artificial
// dep on ServiceCore. The handler reconciles the DB runtime row when a VM
// dies without a Stop() call so admit can re-provision the thread on its
// next event.
func NewUnexpectedRuntimeExitHandler(logger *slog.Logger, db *pgxpool.Pool) func(threadID uuid.UUID) {
	return func(threadID uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		projectID, err := assistantrepo.New(db).ResolveThreadProjectID(ctx, threadID)
		if err != nil {
			logger.ErrorContext(ctx, "resolve assistant thread project after unexpected exit failed",
				attr.SlogAssistantThreadID(threadID.String()),
				attr.SlogError(err),
			)
			return
		}
		err = assistantrepo.New(db).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{
			State:         runtimeStateStopped,
			ProjectID:     projectID,
			ThreadID:      threadID,
			StartingState: runtimeStateStarting,
			ActiveState:   runtimeStateActive,
		})
		if err != nil {
			logger.ErrorContext(ctx, "reconcile assistant runtime after unexpected exit failed",
				attr.SlogAssistantThreadID(threadID.String()),
				attr.SlogError(err),
			)
		}
	}
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

	queries := assistantrepo.New(s.db)
	toolsetIDs := map[string]uuid.UUID{}
	toolsetRows, err := queries.ResolveToolsetsForWrite(ctx, assistantrepo.ResolveToolsetsForWriteParams{
		ProjectID: projectID,
		Slugs:     toolsetSlugs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve toolset slugs: %w", err)
	}
	for _, row := range toolsetRows {
		toolsetIDs[row.Slug] = row.ID
	}
	for _, slug := range toolsetSlugs {
		if _, ok := toolsetIDs[slug]; !ok {
			return nil, assistantValidationError("toolset %q not found in project", slug)
		}
	}

	envIDs := map[string]uuid.UUID{}
	if len(envSlugs) > 0 {
		envRows, err := queries.ResolveEnvironmentsForWrite(ctx, assistantrepo.ResolveEnvironmentsForWriteParams{
			ProjectID: projectID,
			Slugs:     envSlugs,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve environment slugs: %w", err)
		}
		for _, row := range envRows {
			envIDs[row.Slug] = row.ID
		}
		for _, slug := range envSlugs {
			if _, ok := envIDs[slug]; !ok {
				return nil, assistantValidationError("environment %q not found in project", slug)
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
			return nil, assistantValidationError("toolset %q listed more than once", ref.ToolsetSlug)
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
func (s *ServiceCore) loadAssistantToolsets(ctx context.Context, projectID uuid.UUID, assistantIDs []uuid.UUID) (map[uuid.UUID][]assistantToolsetRow, error) {
	out := map[uuid.UUID][]assistantToolsetRow{}
	if len(assistantIDs) == 0 {
		return out, nil
	}
	rows, err := assistantrepo.New(s.db).LoadAssistantToolsets(ctx, assistantrepo.LoadAssistantToolsetsParams{
		AssistantIds: assistantIDs,
		ProjectID:    projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load assistant toolsets: %w", err)
	}
	for _, row := range rows {
		out[row.AssistantID] = append(out[row.AssistantID], assistantToolsetRow{
			ToolsetID:              row.ToolsetID,
			ToolsetSlug:            row.ToolsetSlug,
			McpEnabled:             row.McpEnabled,
			McpSlug:                row.McpSlug,
			DefaultEnvironmentSlug: row.DefaultEnvironmentSlug,
			EnvironmentID:          row.EnvironmentID,
			EnvironmentSlug:        row.EnvironmentSlug,
		})
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
	queries := assistantrepo.New(tx)
	if err := queries.ClearAssistantToolsets(ctx, assistantrepo.ClearAssistantToolsetsParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	}); err != nil {
		return fmt.Errorf("clear assistant toolsets: %w", err)
	}
	if len(resolved) == 0 {
		return nil
	}
	rows := make([]assistantrepo.AddAssistantToolsetsParams, 0, len(resolved))
	for _, r := range resolved {
		rows = append(rows, assistantrepo.AddAssistantToolsetsParams{
			AssistantID:   assistantID,
			ToolsetID:     r.ToolsetID,
			EnvironmentID: r.EnvironmentID,
			ProjectID:     projectID,
		})
	}
	if _, err := queries.AddAssistantToolsets(ctx, rows); err != nil {
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
	createdByUserID string,
	name string,
	model string,
	instructions string,
	toolsets []*types.AssistantToolsetRef,
	warmTTLSeconds int,
	maxConcurrency int,
	status string,
) (assistantRecord, error) {
	if createdByUserID == "" {
		return assistantRecord{}, fmt.Errorf("create assistant: missing user id")
	}

	resolved, err := s.resolveToolsetRefsForWrite(ctx, projectID, toolsets)
	if err != nil {
		return assistantRecord{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := assistantrepo.New(tx)
	created, err := queries.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       projectID,
		OrganizationID:  organizationID,
		CreatedByUserID: conv.ToPGText(createdByUserID),
		Name:            name,
		Model:           model,
		Instructions:    instructions,
		WarmTtlSeconds:  int64(warmTTLSeconds),
		MaxConcurrency:  int64(maxConcurrency),
		Status:          status,
	})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("insert assistant: %w", err)
	}
	record := assistantRecordFromCreateRow(created)

	if err := writeAssistantToolsets(ctx, tx, record.ID, projectID, resolved); err != nil {
		return assistantRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	refs, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) ListAssistants(ctx context.Context, projectID uuid.UUID) ([]assistantRecord, error) {
	rows, err := assistantrepo.New(s.db).ListAssistants(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("query assistants: %w", err)
	}

	out := make([]assistantRecord, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		record := assistantRecordFromListRow(row)
		out = append(out, record)
		ids = append(ids, record.ID)
	}

	refs, err := s.loadAssistantToolsets(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Toolsets = refs[out[i].ID]
	}
	return out, nil
}

func (s *ServiceCore) GetAssistant(ctx context.Context, projectID uuid.UUID, assistantID uuid.UUID) (assistantRecord, error) {
	row, err := assistantrepo.New(s.db).GetAssistant(ctx, assistantrepo.GetAssistantParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("select assistant: %w", err)
	}
	record := assistantRecordFromGetRow(row)
	refs, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) getAssistantForDispatch(ctx context.Context, assistantID uuid.UUID) (assistantRecord, error) {
	row, err := assistantrepo.New(s.db).GetAssistantForDispatch(ctx, assistantID)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("select assistant for dispatch: %w", err)
	}
	record := assistantRecordFromDispatchRow(row)
	refs, err := s.loadAssistantToolsets(ctx, record.ProjectID, []uuid.UUID{record.ID})
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

	queries := assistantrepo.New(tx)
	updated, err := queries.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
		Name:           conv.PtrToPGText(name),
		Model:          conv.PtrToPGText(model),
		Instructions:   conv.PtrToPGText(instructions),
		WarmTtlSeconds: conv.PtrToPGInt8(warmTTLSeconds),
		MaxConcurrency: conv.PtrToPGInt8(maxConcurrency),
		Status:         conv.PtrToPGText(status),
		AssistantID:    assistantID,
		ProjectID:      projectID,
	})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("update assistant: %w", err)
	}
	record := assistantRecordFromUpdateRow(updated)

	if toolsets != nil {
		if err := writeAssistantToolsets(ctx, tx, record.ID, projectID, resolved); err != nil {
			return assistantRecord{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	refs, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return assistantRecord{}, err
	}
	record.Toolsets = refs[record.ID]
	return record, nil
}

func (s *ServiceCore) DeleteAssistant(ctx context.Context, projectID uuid.UUID, assistantID uuid.UUID) error {
	err := assistantrepo.New(s.db).DeleteAssistant(ctx, assistantrepo.DeleteAssistantParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	})
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
	triggerInstanceID, err := conv.PtrToNullUUID(conv.PtrEmpty(task.TriggerInstanceID))
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("parse trigger instance id: %w", err)
	}
	chatID := deterministicChatID(assistant.ID, task.CorrelationID)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("begin assistant enqueue tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := assistantrepo.New(tx)
	if err := queries.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      assistant.ProjectID,
		OrganizationID: assistant.OrganizationID,
		Title:          conv.ToPGText(assistant.Name),
	}); err != nil {
		return EnqueueResult{}, fmt.Errorf("upsert assistant chat: %w", err)
	}

	threadID, err := queries.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     assistant.ProjectID,
		CorrelationID: task.CorrelationID,
		ChatID:        chatID,
		SourceKind:    sourceKind,
		SourceRefJson: sourceRefJSON,
	})
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("upsert assistant thread: %w", err)
	}

	var eventCreated bool
	_, err = queries.InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistant.ID,
		ProjectID:             assistant.ProjectID,
		TriggerInstanceID:     triggerInstanceID,
		EventID:               task.EventID,
		CorrelationID:         task.CorrelationID,
		Status:                eventStatusPending,
		NormalizedPayloadJson: normalizedPayloadJSON,
		SourcePayloadJson:     sourcePayloadJSON,
	})
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

type AdmitPendingThreadsResult struct {
	ProjectID uuid.UUID
	ThreadIDs []uuid.UUID
}

func (s *ServiceCore) AdmitPendingThreads(ctx context.Context, assistantID uuid.UUID) (AdmitPendingThreadsResult, error) {
	assistant, err := s.getAssistantForDispatch(ctx, assistantID)
	if err != nil {
		return AdmitPendingThreadsResult{}, err
	}
	if assistant.Status != StatusActive {
		return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: nil}, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("begin assistant admit tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := assistantrepo.New(tx)
	warmThreadIDs, err := queries.ListWarmPendingThreads(ctx, assistantrepo.ListWarmPendingThreadsParams{
		ProjectID:     assistant.ProjectID,
		AssistantID:   assistantID,
		ActiveState:   runtimeStateActive,
		PendingStatus: eventStatusPending,
	})
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("query warm assistant threads: %w", err)
	}

	admitted := append([]uuid.UUID{}, warmThreadIDs...)

	activeCount, err := queries.CountActiveAssistantRuntimes(ctx, assistantrepo.CountActiveAssistantRuntimesParams{
		ProjectID:     assistant.ProjectID,
		AssistantID:   assistantID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
	})
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("count active assistant runtimes: %w", err)
	}

	available := max(assistant.MaxConcurrency-conv.SafeInt(activeCount), 0)
	if available > 0 {
		coldThreads, err := queries.ListColdPendingThreadsForAdmit(ctx, assistantrepo.ListColdPendingThreadsForAdmitParams{
			ProjectID:     assistant.ProjectID,
			AssistantID:   assistantID,
			PendingStatus: eventStatusPending,
			StartingState: runtimeStateStarting,
			ActiveState:   runtimeStateActive,
			LimitCount:    conv.SafeInt32(available),
		})
		if err != nil {
			return AdmitPendingThreadsResult{}, fmt.Errorf("select cold assistant threads: %w", err)
		}

		for _, coldThread := range coldThreads {
			if err := queries.ReserveAssistantRuntime(ctx, assistantrepo.ReserveAssistantRuntimeParams{
				AssistantThreadID: coldThread.ID,
				AssistantID:       assistantID,
				ProjectID:         coldThread.ProjectID,
				Backend:           s.runtime.Backend(),
				State:             runtimeStateStarting,
			}); err != nil {
				return AdmitPendingThreadsResult{}, fmt.Errorf("reserve assistant runtime: %w", err)
			}
			admitted = append(admitted, coldThread.ID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("commit assistant admit tx: %w", err)
	}
	return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: admitted}, nil
}

func (s *ServiceCore) ProcessThreadEvents(ctx context.Context, projectID, threadID uuid.UUID) (ProcessThreadEventsResult, error) {
	thread, assistant, runtimeRecord, err := s.loadThreadContext(ctx, projectID, threadID)
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
		s.logger.ErrorContext(ctx, "ensure assistant runtime failed", attr.SlogAssistantThreadID(thread.ID.String()), attr.SlogError(err))
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
			s.logger.ErrorContext(ctx, "build runtime startup config failed", attr.SlogAssistantThreadID(thread.ID.String()), attr.SlogError(err))
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
			s.logger.ErrorContext(ctx, "configure assistant runtime failed", attr.SlogAssistantThreadID(thread.ID.String()), attr.SlogError(err))
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
		if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds)*time.Second)); err != nil {
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

		stopLeaseHeartbeat := s.startProcessingLeaseHeartbeat(turnCtx, thread.ProjectID, runtimeRecord.ID, event.ID)
		runErr := s.processEventTurn(turnCtx, thread, assistant, runtimeRecord, event, coldStart)
		stopLeaseHeartbeat()
		if runErr != nil {
			s.logger.WarnContext(ctx, "assistant turn failed",
				attr.SlogAssistantThreadID(thread.ID.String()),
				attr.SlogAssistantEventID(event.ID.String()),
				attr.SlogAssistantAttempt(event.Attempts),
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

			// Upstream completion provider rejected the request (Anthropic 400
			// on a malformed message, OpenRouter rate limit, etc). The runtime
			// is fine — replaying the same input would just produce the same
			// failure, so terminally fail the event and keep the VM warm for
			// subsequent ones rather than churning Fly on every retry.
			if errors.Is(runErr, ErrCompletionFailed) {
				s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant event failed at completion provider", "ERROR", runErr)
				if err := s.failEvent(ctx, thread.ProjectID, event.ID, runErr); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
				if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
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

			// Terminal failure after maxEventAttempts — stop retrying this
			// event. The warm runtime stays up for subsequent events.
			if event.Attempts >= maxEventAttempts {
				s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant event exceeded max attempts", "ERROR", runErr)
				if err := s.failEvent(ctx, thread.ProjectID, event.ID, fmt.Errorf("exceeded %d attempts: %w", maxEventAttempts, runErr)); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
				if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
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
			if err := s.resetEventToPending(ctx, thread.ProjectID, event.ID, runErr); err != nil {
				return ProcessThreadEventsResult{}, err
			}
			warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
			if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
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

		if err := s.completeEvent(ctx, thread.ProjectID, event.ID); err != nil {
			return ProcessThreadEventsResult{}, err
		}
		s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_completed", "assistant event completed", "INFO", nil)
		processedAny = true
		coldStart = false
	}

	warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
	if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
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
	projectID uuid.UUID,
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
				if err := s.touchProcessingLease(hbCtx, projectID, runtimeID, eventID); err != nil && hbCtx.Err() == nil {
					s.logger.WarnContext(hbCtx, "refresh assistant processing lease failed",
						attr.SlogAssistantRuntimeID(runtimeID.String()),
						attr.SlogAssistantEventID(eventID.String()),
						attr.SlogError(err),
					)
				}
			}
		}
	}()
	return cancel
}

func (s *ServiceCore) touchProcessingLease(ctx context.Context, projectID, runtimeID, eventID uuid.UUID) error {
	err := assistantrepo.New(s.db).TouchProcessingLease(ctx, assistantrepo.TouchProcessingLeaseParams{
		EventID:          eventID,
		ProcessingStatus: eventStatusProcessing,
		RuntimeID:        runtimeID,
		ProjectID:        projectID,
		StartingState:    runtimeStateStarting,
		ActiveState:      runtimeStateActive,
	})
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
		Instructions:   conv.PtrEmpty(instructions),
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
	token, err := s.assistantTokens.Generate(assistanttokens.GenerateInput{
		OrgID:       assistant.OrganizationID,
		ProjectID:   assistant.ProjectID,
		UserID:      assistant.CreatedByUserID,
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

func (s *ServiceCore) ProcessThreadEventsByThreadID(ctx context.Context, projectID, threadID uuid.UUID) (ProcessThreadEventsResult, error) {
	return s.ProcessThreadEvents(ctx, projectID, threadID)
}

func (s *ServiceCore) ExpireThreadRuntime(ctx context.Context, projectID, threadID uuid.UUID) error {
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

func (s *ServiceCore) loadThreadContext(ctx context.Context, projectID, threadID uuid.UUID) (assistantThreadRecord, assistantRecord, assistantRuntimeRecord, error) {
	row, err := assistantrepo.New(s.db).LoadThreadContext(ctx, assistantrepo.LoadThreadContextParams{
		ThreadID:      threadID,
		ProjectID:     projectID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
	})
	if err != nil {
		return assistantThreadRecord{}, assistantRecord{}, assistantRuntimeRecord{}, fmt.Errorf("load assistant thread context: %w", err)
	}
	thread := assistantThreadRecord{
		ID:            row.ID,
		AssistantID:   row.AssistantID,
		ProjectID:     row.ProjectID,
		CorrelationID: row.CorrelationID,
		ChatID:        row.ChatID,
		SourceKind:    row.SourceKind,
		SourceRefJSON: row.SourceRefJson,
		LastEventAt:   row.LastEventAt.Time,
	}
	assistant := assistantRecord{
		ID:              row.AssistantRecordID,
		ProjectID:       row.AssistantRecordProjectID,
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
	runtime := assistantRuntimeRecord{
		ID:                  row.RuntimeID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.RuntimeAssistantID,
		ProjectID:           row.RuntimeProjectID,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}
	refs, err := s.loadAssistantToolsets(ctx, assistant.ProjectID, []uuid.UUID{assistant.ID})
	if err != nil {
		return assistantThreadRecord{}, assistantRecord{}, assistantRuntimeRecord{}, err
	}
	assistant.Toolsets = refs[assistant.ID]
	return thread, assistant, runtime, nil
}

func (s *ServiceCore) loadActiveRuntimeRecord(ctx context.Context, projectID, threadID uuid.UUID) (assistantRuntimeRecord, error) {
	row, err := assistantrepo.New(s.db).LoadActiveRuntimeRecord(ctx, assistantrepo.LoadActiveRuntimeRecordParams{
		ProjectID:     projectID,
		ThreadID:      threadID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
	})
	if err != nil {
		return assistantRuntimeRecord{}, fmt.Errorf("load active assistant runtime: %w", err)
	}
	return assistantRuntimeRecord{
		ID:                  row.ID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.AssistantID,
		ProjectID:           row.ProjectID,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}, nil
}

func (s *ServiceCore) loadChatHistory(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID) ([]runtimeMessage, error) {
	messages, err := chatrepo.New(s.db).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
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
	var zero assistantThreadEventRecord
	row, err := assistantrepo.New(s.db).ClaimNextPendingEvent(ctx, assistantrepo.ClaimNextPendingEventParams{
		ProcessingStatus: eventStatusProcessing,
		ProjectID:        projectID,
		ThreadID:         threadID,
		PendingStatus:    eventStatusPending,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("claim assistant thread event: %w", err)
	default:
		return assistantThreadEventRecord{
			ID:                    row.ID,
			AssistantThreadID:     row.AssistantThreadID,
			AssistantID:           row.AssistantID,
			ProjectID:             row.ProjectID,
			TriggerInstanceID:     row.TriggerInstanceID,
			EventID:               row.EventID,
			CorrelationID:         row.CorrelationID,
			Status:                row.Status,
			NormalizedPayloadJSON: row.NormalizedPayloadJson,
			SourcePayloadJSON:     row.SourcePayloadJson,
			Attempts:              conv.SafeInt(row.Attempts),
			LastError:             row.LastError,
		}, true, nil
	}
}

func (s *ServiceCore) completeEvent(ctx context.Context, projectID, eventID uuid.UUID) error {
	err := assistantrepo.New(s.db).CompleteAssistantThreadEvent(ctx, assistantrepo.CompleteAssistantThreadEventParams{
		CompletedStatus: eventStatusCompleted,
		EventID:         eventID,
		ProjectID:       projectID,
	})
	if err != nil {
		return fmt.Errorf("complete assistant thread event: %w", err)
	}
	return nil
}

func (s *ServiceCore) failEvent(ctx context.Context, projectID, eventID uuid.UUID, runErr error) error {
	err := assistantrepo.New(s.db).FailAssistantThreadEvent(ctx, assistantrepo.FailAssistantThreadEventParams{
		FailedStatus: eventStatusFailed,
		LastError:    conv.ToPGText(runErr.Error()),
		EventID:      eventID,
		ProjectID:    projectID,
	})
	if err != nil {
		return fmt.Errorf("fail assistant thread event: %w", err)
	}
	return nil
}

func (s *ServiceCore) resetEventToPending(ctx context.Context, projectID, eventID uuid.UUID, runErr error) error {
	err := assistantrepo.New(s.db).ResetAssistantThreadEventToPending(ctx, assistantrepo.ResetAssistantThreadEventToPendingParams{
		PendingStatus: eventStatusPending,
		LastError:     conv.ToPGText(runErr.Error()),
		EventID:       eventID,
		ProjectID:     projectID,
	})
	if err != nil {
		return fmt.Errorf("reset assistant thread event to pending: %w", err)
	}
	return nil
}

func (s *ServiceCore) setRuntimeActive(ctx context.Context, projectID, runtimeID uuid.UUID, warmUntil time.Time) error {
	err := assistantrepo.New(s.db).SetAssistantRuntimeActive(ctx, assistantrepo.SetAssistantRuntimeActiveParams{
		ActiveState: runtimeStateActive,
		WarmUntil:   conv.ToPGTimestamptz(warmUntil),
		RuntimeID:   runtimeID,
		ProjectID:   projectID,
	})
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
	if len(result.BackendMetadataJSON) == 0 {
		return nil
	}
	if bytes.Equal(runtime.BackendMetadataJSON, result.BackendMetadataJSON) {
		return nil
	}
	if err := assistantrepo.New(s.db).UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
		BackendMetadataJson: result.BackendMetadataJSON,
		RuntimeID:           runtime.ID,
		ProjectID:           runtime.ProjectID,
	}); err != nil {
		return fmt.Errorf("update assistant runtime backend metadata: %w", err)
	}
	runtime.BackendMetadataJSON = append([]byte(nil), result.BackendMetadataJSON...)
	return nil
}

func (s *ServiceCore) stopRuntimeRecord(ctx context.Context, projectID, threadID uuid.UUID, state string) error {
	err := assistantrepo.New(s.db).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{
		State:         state,
		ProjectID:     projectID,
		ThreadID:      threadID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
	})
	if err != nil {
		return fmt.Errorf("stop assistant runtime: %w", err)
	}
	return nil
}
