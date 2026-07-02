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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

const (
	DefaultWarmTTLSeconds = 60
	DefaultMaxConcurrency = 5

	StatusActive = "active"
	StatusPaused = "paused"

	sourceKindSlack     = bgtriggers.DefinitionSlugSlack
	sourceKindLinear    = bgtriggers.DefinitionSlugLinear
	sourceKindGithub    = bgtriggers.DefinitionSlugGithub
	sourceKindCron      = bgtriggers.DefinitionSlugCron
	sourceKindWake      = bgtriggers.DefinitionSlugWake
	sourceKindDashboard = bgtriggers.DefinitionSlugDashboard
	// sourceKindWarmup marks the event-less thread that eager-boots the
	// runtime at assistant creation. It has no source adapter — adapters are
	// only consulted while processing events, and this thread never has any.
	sourceKindWarmup = "warmup"
	// sourceKindSetup marks a client-driven setup/onboarding chat linked to an
	// assistant by the /chat/completions handler (see chat.linkSetupAssistantThread).
	// Like warmup it carries no source adapter and enqueues no runtime events, so
	// it must never be counted toward active/warm runtime concurrency — it is
	// excluded from CountActiveAssistantThreads. It exists purely so the setup
	// chat is listable and URL-addressable via chat.list?assistant_id=.
	sourceKindSetup      = "setup"
	warmupCorrelationID  = "runtime-warmup"
	runtimeStateStarting = "starting"
	runtimeStateActive   = "active"
	runtimeStateExpiring = "expiring"
	runtimeStateStopped  = "stopped"
	runtimeStateFailed   = "failed"
	runtimeStateReaped   = "reaped"

	eventStatusPending    = "pending"
	eventStatusProcessing = "processing"
	eventStatusCompleted  = "completed"
	eventStatusFailed     = "failed"

	// maxEventAttempts caps how many times a single event will be retried
	// against a live runtime before it's marked terminally failed. Prevents
	// a broken upstream (LLM 502, bad tool, etc.) from burning the retry
	// loop forever.
	maxEventAttempts = 5

	// maxRuntimeTeardowns caps how many times one event may tear down and
	// re-admit its runtime (the ErrRuntimeUnhealthy path) before it is failed
	// terminally. Higher than maxEventAttempts because a genuine infra blip
	// deserves generous retries, while a deterministic error misclassified as
	// unhealthy still cannot churn fresh runtimes without bound. event.Attempts
	// advances on every claim, so it doubles as the teardown counter here.
	maxRuntimeTeardowns = 10

	// teardownCapCleanupTimeout bounds the detached cleanup (stop runtime,
	// fail event) on the exhausted-teardown path. The turn that triggered it
	// already failed — often because its context was canceled — so the cleanup
	// runs on a fresh deadline rather than the dead request context.
	teardownCapCleanupTimeout = 30 * time.Second

	assistantPipelineTelemetryURN = "assistants:pipeline"
	assistantRuntimeTelemetryURN  = "assistants:runtime"

	meterAssistantTurnClassified = "assistant.turn.classified"

	runtimeStartupReapGrace = 2 * time.Minute
	// runtimeExpiringReapGrace is the cushion the reaper waits before
	// reclaiming a row stuck in `expiring`. It must exceed the worst-case
	// total budget of the ExpireThreadRuntime activity (Temporal
	// ScheduleToCloseTimeout = 25m) so a still-retrying activity isn't
	// stomped mid-flight; the row only becomes reapable after Temporal
	// gives up.
	runtimeExpiringReapGrace     = 30 * time.Minute
	eventProcessingRequeueGrace  = 3 * time.Minute
	processingLeaseHeartbeatTick = 30 * time.Second
	admitFailureBackoff          = 30 * time.Second
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
	MCPServers      []assistantMCPServerRow
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
	ID uuid.UUID
	// AssistantThreadID is uuid.Nil on v2 runtime rows — one VM serves
	// every thread under the assistant, so the binding is via AssistantID
	// and the foreign key to assistant_threads is dropped on this column
	// (so the sentinel does not have to back to a real row).
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
	CreatedAt             time.Time
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

// assistantMCPServerRow is the hydrated view of a row in assistant_mcp_servers
// joined with mcp_servers + its Gram-hosted endpoint + environments. Like
// assistantToolsetRow, everything dispatch needs to build the MCP server URL
// comes from one read; ServerSlug is the display/runtime ID and EndpointSlug is
// the public /mcp/{slug} path segment the runner connects to.
type assistantMCPServerRow struct {
	MCPServerID     uuid.UUID
	ServerSlug      pgtype.Text
	EndpointSlug    string
	EnvironmentID   uuid.NullUUID
	EnvironmentSlug pgtype.Text
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
		MCPServers:      nil,
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
		MCPServers:      nil,
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
		MCPServers:      nil,
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
		MCPServers:      nil,
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
		MCPServers:      nil,
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
	ShouldSignal bool
}

type ProcessThreadEventsResult struct {
	AssistantID       uuid.UUID
	WarmUntil         time.Time
	WarmTTLSeconds    int
	RuntimeActive     bool
	RetryAdmission    bool
	ProcessedAnyEvent bool
	// BootstrappedRuntime signals that this call transitioned the v2 runtime
	// row from `starting` to `active`. v2 admit only fans out the first
	// pending thread when reserving a fresh row (Ensure has no per-row CAS,
	// so concurrent ensures would race the Fly machine launch). The
	// workflow signals the coordinator on this flag so the remaining
	// pending threads get admitted against the now-active row.
	BootstrappedRuntime bool
}

// ExpireThreadRuntimeResult reports the outcome of an expire attempt.
// Stopped=false + RemainingSeconds means a turn slipped in past the warm
// timer; the workflow should re-arm with that window and try again.
type ExpireThreadRuntimeResult struct {
	Stopped          bool
	RemainingSeconds int
}

// WarmupThreadResult identifies the warmup thread to signal. ShouldSignal is
// false on every no-op path (assistant gone/paused, organic traffic owns the
// runtime row).
type WarmupThreadResult struct {
	ThreadID     uuid.UUID
	ProjectID    uuid.UUID
	ShouldSignal bool
}

// WakeCanceller cancels every pending wake trigger owned by an assistant on
// deletion. The trigger app implements this; assistants owns the interface to
// avoid a dependency back into the triggers package.
type WakeCanceller interface {
	CancelAssistantWakes(ctx context.Context, projectID, assistantID uuid.UUID) error
}

// DashboardIngestor ingests a synchronous, app-invoked message against a direct
// trigger instance, returning the dispatched task (nil when filtered/paused).
// Implemented by the triggers App's IngestDirect.
type DashboardIngestor interface {
	IngestDirect(ctx context.Context, instanceID uuid.UUID, payload []byte, receivedAt time.Time) (*bgtriggers.Task, error)
}

type ServiceCore struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	db                *pgxpool.Pool
	guardianPolicy    *guardian.Policy
	encryptionClient  *encryption.Client
	runtime           RuntimeBackend
	slackClient       *slackclient.SlackClient
	assistantTokens   *assistanttokens.Manager
	serverURL         *url.URL
	telemetryLogger   *telemetry.Logger
	contextWindow     *openrouter.ContextWindowResolver
	wakeCanceller     WakeCanceller
	chatWriter        *chat.ChatMessageWriter
	dashboardIngestor DashboardIngestor
	turnClassified    metric.Int64Counter
}

func NewServiceCore(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	guardianPolicy *guardian.Policy,
	encryptionClient *encryption.Client,
	runtime RuntimeBackend,
	slackClient *slackclient.SlackClient,
	assistantTokens *assistanttokens.Manager,
	serverURL *url.URL,
	telemetryLogger *telemetry.Logger,
	contextWindow *openrouter.ContextWindowResolver,
) *ServiceCore {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/assistants")
	turnClassified, err := meter.Int64Counter(
		meterAssistantTurnClassified,
		metric.WithDescription("Assistant turn failures bucketed by classifyTurnError outcome"),
		metric.WithUnit("{turn}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterAssistantTurnClassified), attr.SlogError(err))
	}

	return &ServiceCore{
		logger:            logger,
		tracer:            tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
		db:                db,
		guardianPolicy:    guardianPolicy,
		encryptionClient:  encryptionClient,
		runtime:           newTelemetryRuntimeBackend(runtime, telemetryLogger),
		slackClient:       slackClient,
		assistantTokens:   assistantTokens,
		serverURL:         serverURL,
		telemetryLogger:   telemetryLogger,
		contextWindow:     contextWindow,
		wakeCanceller:     nil,
		chatWriter:        nil,
		dashboardIngestor: nil,
		turnClassified:    turnClassified,
	}
}

// SetWakeCanceller is set after construction to break an import cycle:
// assistants must not import triggers.
func (s *ServiceCore) SetWakeCanceller(c WakeCanceller) {
	s.wakeCanceller = c
}

// SetDashboardIngestor wires the trigger App used to ingest dashboard sidebar
// messages. Set after construction to match the existing post-construction
// injection pattern. SendDashboardMessage fails if the ingestor was never set.
func (s *ServiceCore) SetDashboardIngestor(i DashboardIngestor) {
	s.dashboardIngestor = i
}

// SetChatMessageWriter wires the chat writer used by self-heal. Set after
// construction (rather than via NewServiceCore) to match the existing
// post-construction injection pattern and avoid churning every test call
// site. Self-heal is skipped if the writer was never set.
func (s *ServiceCore) SetChatMessageWriter(w *chat.ChatMessageWriter) {
	s.chatWriter = w
}

// resolveAssistantContextWindow returns the smallest context_length the gram
// backend has cached for the assistant's model, or 0 on lookup failure. The
// runner reads this from `runtimeStartupConfig` and uses it to threshold
// input-token-aware compaction.
func (s *ServiceCore) resolveAssistantContextWindow(ctx context.Context, model string) uint64 {
	if s.contextWindow == nil || model == "" {
		return 0
	}
	resolved := model
	if alias := openrouter.ResolveModel(model); alias != "" {
		resolved = alias
	}
	tokens, err := s.contextWindow.Resolve(ctx, resolved)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve assistant model context window", attr.SlogError(err), attr.SlogGenAIRequestModel(resolved))
		return 0
	}
	if tokens <= 0 {
		return 0
	}
	return uint64(tokens)
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
			URN:            assistantPipelineTelemetryURN,
			Name:           "assistant:" + assistant.Name,
			ProjectID:      assistant.ProjectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: assistant.OrganizationID,
		},
		UserInfo:   telemetry.UserInfo{},
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
	// process is gone or its driving workflow has given up:
	//   - 'starting' rows that never transitioned to active within the
	//     startup grace window (usually server crashed mid-boot).
	//   - 'expiring' rows whose updated_at is older than the activity's full
	//     retry budget — the ExpireThreadRuntime activity exhausted Temporal
	//     attempts after CAS active->expiring without reaching Stop or
	//     Revert. Without this the row blocks the partial unique index
	//     ReserveAssistantRuntime depends on.
	// 'active' rows are never reaped: an idle runtime keeps its VM until the
	// assistant is deleted.
	queries := assistantrepo.New(s.db)
	runtimeAssistantIDs, err := queries.ReapStuckAssistantRuntimes(ctx, assistantrepo.ReapStuckAssistantRuntimesParams{
		StoppedState:   runtimeStateStopped,
		StartingState:  runtimeStateStarting,
		StartingCutoff: conv.ToPGTimestamptz(now.Add(-runtimeStartupReapGrace)),
		ExpiringState:  runtimeStateExpiring,
		ExpiringCutoff: conv.ToPGTimestamptz(now.Add(-runtimeExpiringReapGrace)),
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

func warmRemainingSeconds(idleSeconds *uint64, ttlSeconds int) int {
	if ttlSeconds <= 0 {
		return 0
	}
	// nil signals the runner reported no live threads — VM is fully idle, so
	// no warm window remains. A non-nil min-idle bigger than the TTL also
	// returns 0, matching the "expired" boundary.
	if idleSeconds == nil || *idleSeconds >= uint64(ttlSeconds) {
		return 0
	}
	return ttlSeconds - int(*idleSeconds) //nolint:gosec // bounded above by ttlSeconds (int)
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

// resolvedMcpServerInsert captures the FK values we need to write one row in
// assistant_mcp_servers for a single (mcp_server_slug, environment_slug?) ref.
type resolvedMcpServerInsert struct {
	MCPServerID   uuid.UUID
	EnvironmentID uuid.NullUUID
}

// resolveMcpServerRefsForWrite validates that every user-supplied mcp server
// slug exists within the project and returns the FK ids to persist, mirroring
// resolveToolsetRefsForWrite. Failing fast turns silent dispatch-time errors
// ("unknown mcp server") into 400s at create/update time.
func (s *ServiceCore) resolveMcpServerRefsForWrite(
	ctx context.Context,
	projectID uuid.UUID,
	refs []*types.AssistantMCPServerRef,
) ([]resolvedMcpServerInsert, error) {
	if len(refs) == 0 {
		return nil, nil
	}

	serverSlugs := make([]string, 0, len(refs))
	envSlugs := make([]string, 0, len(refs))
	seenServerSlug := map[string]struct{}{}
	seenEnvSlug := map[string]struct{}{}
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		if _, ok := seenServerSlug[ref.McpServerSlug]; !ok {
			seenServerSlug[ref.McpServerSlug] = struct{}{}
			serverSlugs = append(serverSlugs, ref.McpServerSlug)
		}
		if ref.EnvironmentSlug != nil && *ref.EnvironmentSlug != "" {
			if _, ok := seenEnvSlug[*ref.EnvironmentSlug]; !ok {
				seenEnvSlug[*ref.EnvironmentSlug] = struct{}{}
				envSlugs = append(envSlugs, *ref.EnvironmentSlug)
			}
		}
	}

	queries := assistantrepo.New(s.db)
	serverIDs := map[string]uuid.UUID{}
	serverRows, err := queries.ResolveMcpServersForWrite(ctx, assistantrepo.ResolveMcpServersForWriteParams{
		ProjectID: projectID,
		Slugs:     serverSlugs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve mcp server slugs: %w", err)
	}
	for _, row := range serverRows {
		if row.Slug.Valid {
			serverIDs[row.Slug.String] = row.ID
		}
	}
	for _, slug := range serverSlugs {
		if _, ok := serverIDs[slug]; !ok {
			return nil, assistantValidationError("mcp server %q not found in project", slug)
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

	out := make([]resolvedMcpServerInsert, 0, len(refs))
	seen := map[uuid.UUID]struct{}{}
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		serverID := serverIDs[ref.McpServerSlug]
		if _, dup := seen[serverID]; dup {
			return nil, assistantValidationError("mcp server %q listed more than once", ref.McpServerSlug)
		}
		seen[serverID] = struct{}{}
		var envID uuid.NullUUID
		if ref.EnvironmentSlug != nil && *ref.EnvironmentSlug != "" {
			envID = uuid.NullUUID{UUID: envIDs[*ref.EnvironmentSlug], Valid: true}
		}
		out = append(out, resolvedMcpServerInsert{MCPServerID: serverID, EnvironmentID: envID})
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

// loadAssistantMcpServers pulls the hydrated mcp_servers attachments for one or
// more assistants in a single query, mirroring loadAssistantToolsets. Rows
// whose server has no Gram-hosted endpoint (empty EndpointSlug) are dropped
// here so dispatch never builds a slugless MCP URL.
func (s *ServiceCore) loadAssistantMcpServers(ctx context.Context, projectID uuid.UUID, assistantIDs []uuid.UUID) (map[uuid.UUID][]assistantMCPServerRow, error) {
	out := map[uuid.UUID][]assistantMCPServerRow{}
	if len(assistantIDs) == 0 {
		return out, nil
	}
	rows, err := assistantrepo.New(s.db).LoadAssistantMcpServers(ctx, assistantrepo.LoadAssistantMcpServersParams{
		AssistantIds: assistantIDs,
		ProjectID:    projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load assistant mcp servers: %w", err)
	}
	for _, row := range rows {
		if row.EndpointSlug == "" {
			continue
		}
		out[row.AssistantID] = append(out[row.AssistantID], assistantMCPServerRow{
			MCPServerID:     row.McpServerID,
			ServerSlug:      row.ServerSlug,
			EndpointSlug:    row.EndpointSlug,
			EnvironmentID:   row.EnvironmentID,
			EnvironmentSlug: row.EnvironmentSlug,
		})
	}
	return out, nil
}

// hydrateAssistantToolSources loads both attachment kinds — toolsets and
// directly-attached mcp_servers — onto a single assistant record. Every read
// that feeds the runtime (dispatch, bootstrap, reconcile) and every API read
// goes through here so record.Toolsets and record.MCPServers stay in lockstep.
func (s *ServiceCore) hydrateAssistantToolSources(ctx context.Context, projectID uuid.UUID, record *assistantRecord) error {
	toolsets, err := s.loadAssistantToolsets(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return err
	}
	record.Toolsets = toolsets[record.ID]

	mcpServers, err := s.loadAssistantMcpServers(ctx, projectID, []uuid.UUID{record.ID})
	if err != nil {
		return err
	}
	record.MCPServers = mcpServers[record.ID]
	return nil
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
	toolsetIDs := make([]uuid.UUID, 0, len(resolved))
	for _, r := range resolved {
		rows = append(rows, assistantrepo.AddAssistantToolsetsParams{
			AssistantID:   assistantID,
			ToolsetID:     r.ToolsetID,
			EnvironmentID: r.EnvironmentID,
			ProjectID:     projectID,
		})
		toolsetIDs = append(toolsetIDs, r.ToolsetID)
	}
	if _, err := queries.AddAssistantToolsets(ctx, rows); err != nil {
		return fmt.Errorf("insert assistant toolsets: %w", err)
	}
	// The runtime startup config requires every assistant-attached toolset
	// to be MCP-reachable; assistants address tools via the MCP server.
	// Auto-enable on attach so the user doesn't have to toggle it
	// separately on each toolset.
	if err := queries.EnableMCPForToolsets(ctx, assistantrepo.EnableMCPForToolsetsParams{
		ToolsetIds: toolsetIDs,
		ProjectID:  projectID,
	}); err != nil {
		return fmt.Errorf("enable mcp for assistant toolsets: %w", err)
	}
	return nil
}

// writeAssistantMcpServers replaces the assistant's directly-attached mcp
// server set. Caller-supplied tx so it shares the same atomic boundary as the
// assistant row and toolset writes. Unlike toolsets there is no MCP-enable
// step: a remote/tunnelled mcp_server is already reachable at its own endpoint.
func writeAssistantMcpServers(
	ctx context.Context,
	tx pgx.Tx,
	assistantID, projectID uuid.UUID,
	resolved []resolvedMcpServerInsert,
) error {
	queries := assistantrepo.New(tx)
	if err := queries.ClearAssistantMcpServers(ctx, assistantrepo.ClearAssistantMcpServersParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	}); err != nil {
		return fmt.Errorf("clear assistant mcp servers: %w", err)
	}
	if len(resolved) == 0 {
		return nil
	}
	rows := make([]assistantrepo.AddAssistantMcpServersParams, 0, len(resolved))
	for _, r := range resolved {
		rows = append(rows, assistantrepo.AddAssistantMcpServersParams{
			AssistantID:   assistantID,
			McpServerID:   r.MCPServerID,
			EnvironmentID: r.EnvironmentID,
			ProjectID:     projectID,
		})
	}
	if _, err := queries.AddAssistantMcpServers(ctx, rows); err != nil {
		return fmt.Errorf("insert assistant mcp servers: %w", err)
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
	mcpServers := make([]*types.AssistantMCPServerRef, 0, len(record.MCPServers))
	for _, row := range record.MCPServers {
		ref := &types.AssistantMCPServerRef{
			McpServerSlug:   row.ServerSlug.String,
			EnvironmentSlug: nil,
		}
		if row.EnvironmentSlug.Valid {
			envSlug := row.EnvironmentSlug.String
			ref.EnvironmentSlug = &envSlug
		}
		mcpServers = append(mcpServers, ref)
	}
	return &types.Assistant{
		ID:              record.ID.String(),
		ProjectID:       record.ProjectID.String(),
		CreatedByUserID: conv.PtrEmpty(record.CreatedByUserID),
		Name:            record.Name,
		Model:           record.Model,
		Instructions:    record.Instructions,
		Toolsets:        toolsets,
		McpServers:      mcpServers,
		WarmTTLSeconds:  record.WarmTTLSeconds,
		MaxConcurrency:  record.MaxConcurrency,
		Status:          record.Status,
		CreatedAt:       record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       record.UpdatedAt.UTC().Format(time.RFC3339),
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
	mcpServers []*types.AssistantMCPServerRef,
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
	resolvedMcpServers, err := s.resolveMcpServerRefsForWrite(ctx, projectID, mcpServers)
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
	if err := writeAssistantMcpServers(ctx, tx, record.ID, projectID, resolvedMcpServers); err != nil {
		return assistantRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	if err := s.hydrateAssistantToolSources(ctx, projectID, &record); err != nil {
		return assistantRecord{}, err
	}
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
	mcpRefs, err := s.loadAssistantMcpServers(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Toolsets = refs[out[i].ID]
		out[i].MCPServers = mcpRefs[out[i].ID]
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
	if err := s.hydrateAssistantToolSources(ctx, projectID, &record); err != nil {
		return assistantRecord{}, err
	}
	return record, nil
}

func (s *ServiceCore) getAssistantForDispatch(ctx context.Context, assistantID uuid.UUID) (assistantRecord, error) {
	row, err := assistantrepo.New(s.db).GetAssistantForDispatch(ctx, assistantID)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("select assistant for dispatch: %w", err)
	}
	record := assistantRecordFromDispatchRow(row)
	if err := s.hydrateAssistantToolSources(ctx, record.ProjectID, &record); err != nil {
		return assistantRecord{}, err
	}
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
	mcpServers []*types.AssistantMCPServerRef,
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
	var resolvedMcpServers []resolvedMcpServerInsert
	if mcpServers != nil {
		r, err := s.resolveMcpServerRefsForWrite(ctx, projectID, mcpServers)
		if err != nil {
			return assistantRecord{}, err
		}
		resolvedMcpServers = r
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
	if mcpServers != nil {
		if err := writeAssistantMcpServers(ctx, tx, record.ID, projectID, resolvedMcpServers); err != nil {
			return assistantRecord{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant tx: %w", err)
	}

	if err := s.hydrateAssistantToolSources(ctx, projectID, &record); err != nil {
		return assistantRecord{}, err
	}
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

	// Best-effort: tear down per-assistant backend resources (e.g. Fly app)
	// inline so the common case does not wait for the janitor. Failures here
	// must not roll back the soft-delete; the long-inactivity janitor is the
	// safety net.
	if reapResult, reapErr := s.ReapAssistantRuntimes(ctx, projectID, assistantID); reapErr != nil {
		s.logger.WarnContext(ctx, "reap assistant runtimes on delete failed",
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogProjectID(projectID.String()),
			attr.SlogError(reapErr),
		)
	} else if reapResult.Reaped > 0 || reapResult.Errors > 0 {
		s.logger.InfoContext(ctx, "reaped assistant runtimes on delete",
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogProjectID(projectID.String()),
			attr.SlogVisibilityInternal(),
		)
	}

	if s.wakeCanceller != nil {
		if cancelErr := s.wakeCanceller.CancelAssistantWakes(ctx, projectID, assistantID); cancelErr != nil {
			s.logger.WarnContext(ctx, "cancel pending wakes on assistant delete failed",
				attr.SlogAssistantID(assistantID.String()),
				attr.SlogProjectID(projectID.String()),
				attr.SlogError(cancelErr),
			)
		}
	}

	return nil
}

// ReapAssistantRuntimesResult summarises the outcome of one reap pass —
// counted at the runtime-row level, not the backend-app level. A row whose
// backend resource was already gone (404 from the Fly API, no in-memory
// state for local) still counts as Reaped.
type ReapAssistantRuntimesResult struct {
	Reaped int
	Errors int
}

// ReapAssistantRuntimes tears down backend resources for every runtime row
// belonging to the given assistant that still carries metadata. Used by the
// assistant-delete handler so a deletion cleans up the corresponding Fly app
// without waiting for the janitor.
func (s *ServiceCore) ReapAssistantRuntimes(ctx context.Context, projectID, assistantID uuid.UUID) (ReapAssistantRuntimesResult, error) {
	rows, err := assistantrepo.New(s.db).ListAssistantRuntimesForReap(ctx, assistantrepo.ListAssistantRuntimesForReapParams{
		AssistantID: assistantID,
		ProjectID:   projectID,
	})
	if err != nil {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("list assistant runtimes for reap: %w", err)
	}

	result := ReapAssistantRuntimesResult{Reaped: 0, Errors: 0}
	for _, row := range rows {
		if s.reapRuntimeRow(ctx, assistantRuntimeRecord{
			ID:                  row.ID,
			AssistantThreadID:   row.AssistantThreadID,
			AssistantID:         row.AssistantID,
			ProjectID:           row.ProjectID,
			Backend:             row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson,
			State:               row.State,
			WarmUntil:           row.WarmUntil,
		}) {
			result.Reaped++
		} else {
			result.Errors++
		}
	}
	return result, nil
}

// ReapInactiveAssistantRuntimesParams configures one janitor sweep.
type ReapInactiveAssistantRuntimesParams struct {
	// InactivityThreshold is the minimum quiet period before an assistant's
	// runtime rows become candidates for collection. The query compares
	// against r.updated_at across all of an assistant's rows so a normal
	// cold-warm-cold cycle keeps the assistant out of the candidate set.
	InactivityThreshold time.Duration
	// BatchSize caps how many runtime rows one sweep will reap. Keeps the
	// activity duration bounded under Temporal's StartToCloseTimeout.
	BatchSize int32
	// OnRowProcessed, if set, fires once per row after the reap attempt
	// (success or failure).
	OnRowProcessed func()
}

// ReapInactiveAssistantRuntimes drives the long-inactivity janitor. It picks
// runtime rows that are finalized (soft-deleted or ended) or belong to a
// soft-deleted assistant, whose owning assistant has had no recorded
// activity within InactivityThreshold, and tears down the backend resources
// their Stop/Reap left behind. A live row under a live assistant is never a
// candidate: an idle runtime keeps its VM until the assistant is deleted.
func (s *ServiceCore) ReapInactiveAssistantRuntimes(ctx context.Context, params ReapInactiveAssistantRuntimesParams) (ReapAssistantRuntimesResult, error) {
	if params.InactivityThreshold <= 0 {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("inactivity threshold must be positive")
	}
	if params.BatchSize <= 0 {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("batch size must be positive")
	}

	rows, err := assistantrepo.New(s.db).ListInactiveAssistantRuntimesForReap(ctx, assistantrepo.ListInactiveAssistantRuntimesForReapParams{
		InactiveBefore: conv.ToPGTimestamptz(time.Now().UTC().Add(-params.InactivityThreshold)),
		// Also collect runtimes parked on a backend we no longer target so a
		// provider switch (e.g. flyio -> gke) drains the old backend lazily as
		// assistants go idle, without an admission-path teardown.
		TargetBackend: s.runtime.Backend(),
		StoppedState:  runtimeStateStopped,
		LimitCount:    params.BatchSize,
	})
	if err != nil {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("list inactive assistant runtimes for reap: %w", err)
	}

	result := ReapAssistantRuntimesResult{Reaped: 0, Errors: 0}
	for _, row := range rows {
		if s.reapRuntimeRow(ctx, assistantRuntimeRecord{
			ID:                  row.ID,
			AssistantThreadID:   row.AssistantThreadID,
			AssistantID:         row.AssistantID,
			ProjectID:           row.ProjectID,
			Backend:             row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson,
			State:               row.State,
			WarmUntil:           row.WarmUntil,
		}) {
			result.Reaped++
		} else {
			result.Errors++
		}
		if params.OnRowProcessed != nil {
			params.OnRowProcessed()
		}
	}
	return result, nil
}

// ReapStoppedAssistantRuntimesParams configures one per-thread reap sweep.
type ReapStoppedAssistantRuntimesParams struct {
	// StoppedTTL is the minimum age (against the row's own updated_at) before
	// a stopped or failed runtime row becomes eligible for collection.
	StoppedTTL time.Duration
	// BatchSize caps how many runtime rows one sweep will reap. Bounds the
	// activity's duration under Temporal's StartToCloseTimeout.
	BatchSize int32
}

// ReapStoppedAssistantRuntimes drives the per-thread janitor. It collects
// runtime rows whose own updated_at has aged past StoppedTTL, regardless of
// sibling activity on the same assistant. The Fly machine for each row is
// destroyed; the surrounding app stays in place so the next admit for the
// same thread can cold-launch into it and keep its IP and secrets.
func (s *ServiceCore) ReapStoppedAssistantRuntimes(ctx context.Context, params ReapStoppedAssistantRuntimesParams) (ReapAssistantRuntimesResult, error) {
	if params.StoppedTTL <= 0 {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("stopped TTL must be positive")
	}
	if params.BatchSize <= 0 {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("batch size must be positive")
	}

	rows, err := assistantrepo.New(s.db).ListStoppedRuntimesForReap(ctx, assistantrepo.ListStoppedRuntimesForReapParams{
		StoppedState:  runtimeStateStopped,
		FailedState:   runtimeStateFailed,
		StoppedBefore: conv.ToPGTimestamptz(time.Now().UTC().Add(-params.StoppedTTL)),
		ReapedState:   runtimeStateReaped,
		LimitCount:    params.BatchSize,
	})
	if err != nil {
		return ReapAssistantRuntimesResult{}, fmt.Errorf("list stopped assistant runtimes for reap: %w", err)
	}

	result := ReapAssistantRuntimesResult{Reaped: 0, Errors: 0}
	for _, row := range rows {
		if s.reapStoppedRuntimeRow(ctx, assistantRuntimeRecord{
			ID:                  row.ID,
			AssistantThreadID:   row.AssistantThreadID,
			AssistantID:         row.AssistantID,
			ProjectID:           row.ProjectID,
			Backend:             row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson,
			State:               row.State,
			WarmUntil:           row.WarmUntil,
		}) {
			result.Reaped++
		} else {
			result.Errors++
		}
	}
	return result, nil
}

// RecycleAssistantRuntimeImagesResult summarises one deploy-time image sweep
// at the runtime-row level. Skipped covers every expected non-recycle: image
// already current, machine busy with a turn, or backend resource gone.
type RecycleAssistantRuntimeImagesResult struct {
	Recycled int
	Skipped  int
	Errors   int
}

// RecycleAssistantRuntimeImagesParams configures one image recycle sweep.
type RecycleAssistantRuntimeImagesParams struct {
	// OnRowProcessed, if set, fires once per row after the recycle attempt
	// (success, skip or failure).
	OnRowProcessed func()
}

// RecycleActiveRuntimeImages best-effort rolls every active v2 runtime onto
// the currently configured runtime image, so deploys absorb the image-pull +
// reboot cost while runtimes are idle instead of the next turn paying it.
// Busy or failed rows are not chased — the per-admission path catches them
// lazily.
//
// This is the in-place roll for backends that reuse idle runtimes (Fly). A
// non-reuse backend (GKE) has no in-place swap and rolls onto a new image by
// terminating idle runtimes instead (the warm-TTL expiry stops them, which
// deletes the claim, and the next /turn re-admits onto a fresh warm-pool pod
// already running the new image), so this sweep is a no-op for it.
func (s *ServiceCore) RecycleActiveRuntimeImages(ctx context.Context, params RecycleAssistantRuntimeImagesParams) (RecycleAssistantRuntimeImagesResult, error) {
	if !s.runtime.ReusesIdleRuntimes() {
		return RecycleAssistantRuntimeImagesResult{Recycled: 0, Skipped: 0, Errors: 0}, nil
	}

	queries := assistantrepo.New(s.db)
	rows, err := queries.ListActiveAssistantRuntimesForImageRecycle(ctx, runtimeStateActive)
	if err != nil {
		return RecycleAssistantRuntimeImagesResult{}, fmt.Errorf("list active assistant runtimes for image recycle: %w", err)
	}

	result := RecycleAssistantRuntimeImagesResult{Recycled: 0, Skipped: 0, Errors: 0}
	for _, row := range rows {
		record := assistantRuntimeRecord{
			ID:                  row.ID,
			AssistantThreadID:   row.AssistantThreadID,
			AssistantID:         row.AssistantID,
			ProjectID:           row.ProjectID,
			Backend:             row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson,
			State:               row.State,
			WarmUntil:           row.WarmUntil,
		}
		// In-flight events mean turns are queued for or running on this VM —
		// the runner's idle clock only clears on /turn enqueue, so the idle
		// probe alone can miss a turn that admission is about to deliver.
		// Those admissions recycle the image lazily through Ensure anyway.
		inFlight, err := queries.CountInFlightAssistantThreadEvents(ctx, assistantrepo.CountInFlightAssistantThreadEventsParams{
			ProjectID:        row.ProjectID,
			AssistantID:      row.AssistantID,
			PendingStatus:    eventStatusPending,
			ProcessingStatus: eventStatusProcessing,
		})
		if err != nil {
			s.logger.WarnContext(ctx, "count in-flight assistant thread events for image recycle failed",
				attr.SlogAssistantID(row.AssistantID.String()),
				attr.SlogProjectID(row.ProjectID.String()),
				attr.SlogError(err),
			)
			result.Errors++
			if params.OnRowProcessed != nil {
				params.OnRowProcessed()
			}
			continue
		}
		if inFlight > 0 {
			result.Skipped++
			if params.OnRowProcessed != nil {
				params.OnRowProcessed()
			}
			continue
		}

		recycled, err := s.runtime.RecycleImage(ctx, record)
		switch {
		case err != nil:
			s.logger.WarnContext(ctx, "assistant runtime image recycle failed",
				attr.SlogAssistantID(row.AssistantID.String()),
				attr.SlogProjectID(row.ProjectID.String()),
				attr.SlogError(err),
			)
			result.Errors++
		case recycled.Recycled:
			affected, err := queries.UpdateActiveAssistantRuntimeMetadata(ctx, assistantrepo.UpdateActiveAssistantRuntimeMetadataParams{
				BackendMetadataJson: recycled.BackendMetadataJSON,
				RuntimeID:           row.ID,
				ProjectID:           row.ProjectID,
				ActiveState:         runtimeStateActive,
			})
			switch {
			case err != nil:
				// A persist failure only costs the next Ensure a cold-start
				// health budget (LastBootID mismatch) — the machine itself
				// is already on the new image, so still count the recycle.
				s.logger.WarnContext(ctx, "persist recycled assistant runtime metadata failed",
					attr.SlogAssistantID(row.AssistantID.String()),
					attr.SlogProjectID(row.ProjectID.String()),
					attr.SlogError(err),
				)
				result.Recycled++
			case affected == 0:
				// The warm timer expired the row mid-recycle and Stop already
				// ran against the pre-recycle machine. Undo the restart so the
				// sweep never leaves a machine running that no live row tracks.
				if stopErr := s.runtime.Stop(ctx, record); stopErr != nil {
					s.logger.WarnContext(ctx, "stop assistant runtime after raced image recycle failed",
						attr.SlogAssistantID(row.AssistantID.String()),
						attr.SlogProjectID(row.ProjectID.String()),
						attr.SlogError(stopErr),
					)
				}
				result.Skipped++
			default:
				result.Recycled++
			}
		default:
			result.Skipped++
		}
		if params.OnRowProcessed != nil {
			params.OnRowProcessed()
		}
	}
	return result, nil
}

// reapStoppedRuntimeRow tears down the machine first and only flips the row
// to `reaped` once the destroy call returns. Inverting the order would orphan
// the machine if the Fly call hits a transient error: a row marked `reaped`
// with `machine_id` cleared no longer matches `ListStoppedRuntimesForReap`,
// so the next sweep cannot retry — and the whole-assistant 7d janitor cannot
// either, because it inherits the same cleared metadata. The narrow window
// where a racing warm-resume admit copies this row's `machine_id` before
// destroy is bounded by `ListStoppedRuntimesForReap`'s per-thread NOT EXISTS
// guard, which excludes any row whose thread already has a fresher
// starting/active sibling. Returns true on success (including idempotent
// no-op when the machine was already gone).
func (s *ServiceCore) reapStoppedRuntimeRow(ctx context.Context, record assistantRuntimeRecord) bool {
	if err := s.runtime.ReapStoppedMachine(ctx, record); err != nil {
		s.logger.WarnContext(ctx, "reap stopped runtime machine failed",
			attr.SlogAssistantID(record.AssistantID.String()),
			attr.SlogAssistantThreadID(record.AssistantThreadID.String()),
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogAssistantRuntimeBackend(record.Backend),
			attr.SlogError(err),
		)
		return false
	}

	if _, err := assistantrepo.New(s.db).MarkAssistantRuntimeMachineReaped(ctx, assistantrepo.MarkAssistantRuntimeMachineReapedParams{
		ReapedState:  runtimeStateReaped,
		StoppedState: runtimeStateStopped,
		FailedState:  runtimeStateFailed,
		RuntimeID:    record.ID,
		ProjectID:    record.ProjectID,
	}); err != nil {
		s.logger.WarnContext(ctx, "mark assistant runtime machine reaped failed",
			attr.SlogAssistantID(record.AssistantID.String()),
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogError(err),
		)
		return false
	}
	return true
}

// RuntimeImageRef returns the runtime image reference the configured backend
// launches machines with. The deploy-time recycle workflow is keyed on it so
// each image version triggers exactly one sweep.
func (s *ServiceCore) RuntimeImageRef() string {
	return s.runtime.ImageRef()
}

// reapRuntimeRow tears down the backend resource for one row and records the
// outcome in DB. Returns true on success (including idempotent no-op when
// the resource was already gone). Errors are logged here so callers can
// keep the loop simple.
func (s *ServiceCore) reapRuntimeRow(ctx context.Context, record assistantRuntimeRecord) bool {
	if err := s.runtime.Reap(ctx, record); err != nil {
		s.logger.WarnContext(ctx, "reap runtime backend failed",
			attr.SlogAssistantID(record.AssistantID.String()),
			attr.SlogAssistantThreadID(record.AssistantThreadID.String()),
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogAssistantRuntimeBackend(record.Backend),
			attr.SlogError(err),
		)
		return false
	}

	if err := assistantrepo.New(s.db).MarkAssistantRuntimeReaped(ctx, assistantrepo.MarkAssistantRuntimeReapedParams{
		ReapedState: runtimeStateReaped,
		RuntimeID:   record.ID,
		ProjectID:   record.ProjectID,
	}); err != nil {
		s.logger.WarnContext(ctx, "mark assistant runtime reaped failed",
			attr.SlogAssistantID(record.AssistantID.String()),
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogError(err),
		)
		return false
	}
	return true
}

func (s *ServiceCore) EnqueueTriggerTask(ctx context.Context, task bgtriggers.Task) (EnqueueResult, error) {
	assistantID, err := uuid.Parse(task.TargetRef)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("parse assistant id: %w", err)
	}
	assistant, err := s.getAssistantForDispatch(ctx, assistantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Trigger targets an assistant that no longer exists (deleted, or the
		// trigger was created against the wrong id). Retrying won't help —
		// drop the dispatch so the activity succeeds and Temporal doesn't
		// hammer the same row through three attempts.
		s.logger.WarnContext(ctx, "skipping trigger dispatch: assistant not found",
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogTriggerInstanceID(task.TriggerInstanceID),
		)
		return EnqueueResult{AssistantID: uuid.Nil, ThreadID: uuid.Nil, ShouldSignal: false}, nil
	case err != nil:
		return EnqueueResult{}, err
	}
	if assistant.Status != StatusActive {
		return EnqueueResult{
			AssistantID:  assistant.ID,
			ThreadID:     uuid.Nil,
			ShouldSignal: false,
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
	adapter, err := getSourceAdapter(sourceKind)
	if err != nil {
		return EnqueueResult{}, err
	}
	chatID := adapter.ChatID(assistant.ID, task.CorrelationID)

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
		UserID:         conv.ToPGTextEmpty(dashboardChatUserID(sourceKind, normalizedPayloadJSON)),
		Title:          conv.ToPGText(chat.DefaultChatTitle),
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
	// pgx.ErrNoRows means the event was already enqueued by an earlier attempt
	// (idempotent retry). We still signal: a turn whose earlier coordinator
	// signal failed must be picked up when the client retries.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EnqueueResult{}, fmt.Errorf("insert assistant thread event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return EnqueueResult{}, fmt.Errorf("commit assistant enqueue tx: %w", err)
	}

	return EnqueueResult{
		AssistantID:  assistant.ID,
		ThreadID:     threadID,
		ShouldSignal: true,
	}, nil
}

// dashboardChatUserID extracts the Gram user id from a dashboard turn payload
// so UpsertAssistantChat can stamp it on the chats row. External-source turns
// return empty — their chat rows are owner-less.
func dashboardChatUserID(sourceKind string, normalizedPayloadJSON []byte) string {
	if sourceKind != sourceKindDashboard {
		return ""
	}
	var dash dashboardEventPayload
	if err := json.Unmarshal(normalizedPayloadJSON, &dash); err != nil {
		return ""
	}
	return dash.UserID
}

// CheckDashboardChatOwnership returns nil when callerUserID owns the chats row
// for (projectID, chatID), pgx.ErrNoRows when they don't, and a wrapped error
// otherwise. Callers gate sendMessage on this so a leaked or guessed chat_id
// can't be used to inject into another user's conversation.
func (s *ServiceCore) CheckDashboardChatOwnership(ctx context.Context, projectID, chatID uuid.UUID, callerUserID string) error {
	_, err := assistantrepo.New(s.db).CallerOwnsDashboardChat(ctx, assistantrepo.CallerOwnsDashboardChatParams{
		ChatID:    chatID,
		ProjectID: projectID,
		UserID:    conv.ToPGText(callerUserID),
	})
	if err != nil {
		return fmt.Errorf("resolve dashboard chat access: %w", err)
	}
	return nil
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
	case sourceKindLinear:
		var event linearEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode linear trigger event: %w", err)
		}
		sourceRefJSON, err := json.Marshal(linearSourceRef{
			EventType: event.EventType,
			URL:       event.URL,
		})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal linear source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindLinear, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
	case sourceKindGithub:
		var event githubEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode github trigger event: %w", err)
		}
		sourceRefJSON, err := json.Marshal(githubSourceRef{
			EventType: event.EventType,
			Action:    event.Action,
			Repo:      event.Repo,
		})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal github source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindGithub, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
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
	case sourceKindWake:
		var event wakeEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode wake trigger event: %w", err)
		}
		sourceRefJSON, err := json.Marshal(wakeSourceRef{
			TriggerInstanceID: event.TriggerInstanceID,
			ScheduledAt:       event.ScheduledAt,
		})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal wake source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindWake, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
	case sourceKindDashboard:
		var event dashboardEventPayload
		if err := json.Unmarshal(task.EventJSON, &event); err != nil {
			return "", nil, nil, nil, fmt.Errorf("decode dashboard trigger event: %w", err)
		}
		sourceRefJSON, err := json.Marshal(dashboardSourceRef{UserID: event.UserID})
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("marshal dashboard source ref: %w", err)
		}
		sourcePayloadJSON := task.RawPayload
		if !json.Valid(sourcePayloadJSON) {
			sourcePayloadJSON, err = json.Marshal(map[string]string{"raw": string(task.RawPayload)})
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("marshal fallback source payload: %w", err)
			}
		}
		return sourceKindDashboard, sourceRefJSON, task.EventJSON, sourcePayloadJSON, nil
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

	return s.admitPendingThreadsV2(ctx, assistant)
}

// admitPendingThreadsV2 reserves the assistant's single v2 runtime under
// pg_advisory_xact_lock (which auto-releases at commit) and admits pending
// threads up to the assistant's MaxConcurrency. "Active" — for cap
// accounting — is any thread whose last_event_at falls inside the
// assistant's WarmTTLSeconds window (the same TTL that drives the warm-
// wait workflow expiry); the count is taken inside the advisory tx so it
// is consistent with the admit decision.
func (s *ServiceCore) admitPendingThreadsV2(ctx context.Context, assistant assistantRecord) (AdmitPendingThreadsResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("begin assistant admit tx (v2): %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, assistant.ID.String()); err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("acquire assistant advisory lock: %w", err)
	}

	threads, err := queries.ListAssistantPendingThreads(ctx, assistantrepo.ListAssistantPendingThreadsParams{
		ProjectID:     assistant.ProjectID,
		AssistantID:   assistant.ID,
		PendingStatus: eventStatusPending,
	})
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("list assistant pending threads: %w", err)
	}
	if len(threads) == 0 {
		return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: nil}, nil
	}

	row, err := queries.LookupActiveAssistantRuntimeV2(ctx, assistantrepo.LookupActiveAssistantRuntimeV2Params{
		ProjectID:   assistant.ProjectID,
		AssistantID: assistant.ID,
	})
	// When the runtime row is freshly reserved here, only admit the first
	// thread. The Fly Ensure path has no CAS or lock around machine launch:
	// fanning out all pending threads against a `starting` row lets two
	// thread workflows race their Ensure activities and launch separate
	// machines, with only one app/machine recorded in
	// backend_metadata_json. The first admitted thread brings the runtime
	// to `active`; subsequent admits fall through the lookup with row in
	// active state and admit the rest.
	firstThreadOnly := false
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := queries.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
			AssistantThreadID: threads[0].ID,
			AssistantID:       assistant.ID,
			ProjectID:         assistant.ProjectID,
			Backend:           s.runtime.Backend(),
			State:             runtimeStateStarting,
		}); err != nil {
			return AdmitPendingThreadsResult{}, fmt.Errorf("reserve v2 assistant runtime: %w", err)
		}
		firstThreadOnly = true
	case err != nil:
		return AdmitPendingThreadsResult{}, fmt.Errorf("lookup v2 runtime: %w", err)
	case row.State == runtimeStateStarting:
		// Another worker reserved the row but its admitted thread hasn't
		// finished Ensure yet. Same race condition — don't fan out until
		// the runtime is active.
		return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: nil}, nil
	case row.State == runtimeStateExpiring:
		// The warm-timer workflow has CASed the row to expiring and is
		// driving Stop. Signalling threads now would race with that path:
		// LoadThreadContextV2 only accepts starting/active and the partial
		// unique index forbids inserting a replacement row until the
		// current one is soft-deleted. Bail out — the thread workflow
		// signals the coordinator after Stop completes, which retriggers
		// admit under a clean slot.
		return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: nil}, nil
	}

	active, err := queries.CountActiveAssistantThreads(ctx, assistantrepo.CountActiveAssistantThreadsParams{
		ProjectID:        assistant.ProjectID,
		AssistantID:      assistant.ID,
		WarmupSourceKind: sourceKindWarmup,
		SetupSourceKind:  sourceKindSetup,
		ActiveSince:      conv.ToPGTimestamptz(time.Now().UTC().Add(-time.Duration(assistant.WarmTTLSeconds) * time.Second)),
		PendingStatus:    eventStatusPending,
	})
	if err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("count active assistant threads: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return AdmitPendingThreadsResult{}, fmt.Errorf("commit assistant admit tx (v2): %w", err)
	}

	if firstThreadOnly {
		// We just reserved the runtime row; the starter must be admitted
		// even when active siblings already saturate MaxConcurrency, or
		// nothing drives Ensure and the row stays starting until reaped.
		threads = threads[:1]
	} else {
		headroom := assistant.MaxConcurrency - int(active)
		if headroom <= 0 {
			return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: nil}, nil
		}
		if len(threads) > headroom {
			threads = threads[:headroom]
		}
	}

	admitted := make([]uuid.UUID, 0, len(threads))
	for _, t := range threads {
		admitted = append(admitted, t.ID)
	}
	return AdmitPendingThreadsResult{ProjectID: assistant.ProjectID, ThreadIDs: admitted}, nil
}

// EnsureWarmupThread sets up the assistant's event-less warmup thread and
// reserves the v2 runtime row against it, so that signalling the standard
// thread workflow boots the runtime exactly as a turn would — Ensure,
// coordinator kicks and the warm window all run through the existing
// machinery; the thread simply never has events to process.
//
// Idempotent under the same advisory lock admit uses. ShouldSignal is false
// when a live runtime row exists that the warmup thread doesn't own —
// organic traffic got there first and its threads drive the boot.
func (s *ServiceCore) EnsureWarmupThread(ctx context.Context, assistantID uuid.UUID) (WarmupThreadResult, error) {
	noop := WarmupThreadResult{ThreadID: uuid.Nil, ProjectID: uuid.Nil, ShouldSignal: false}

	dispatchRow, err := assistantrepo.New(s.db).GetAssistantForDispatch(ctx, assistantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Deleted between creation and the warmup running.
		return noop, nil
	case err != nil:
		return noop, fmt.Errorf("select assistant for warmup: %w", err)
	}
	// Skip getAssistantForDispatch's toolset hydration — warmup only needs
	// the scalar fields.
	assistant := assistantRecordFromDispatchRow(dispatchRow)
	if assistant.Status != StatusActive {
		return noop, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return noop, fmt.Errorf("begin assistant warmup tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, assistant.ID.String()); err != nil {
		return noop, fmt.Errorf("acquire assistant advisory lock: %w", err)
	}

	chatID := deterministicChatID(assistant.ID, warmupCorrelationID)
	if err := queries.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      assistant.ProjectID,
		OrganizationID: assistant.OrganizationID,
		UserID:         pgtype.Text{String: "", Valid: false},
		Title:          conv.ToPGText(chat.DefaultChatTitle),
	}); err != nil {
		return noop, fmt.Errorf("upsert warmup chat: %w", err)
	}
	threadID, err := queries.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     assistant.ProjectID,
		CorrelationID: warmupCorrelationID,
		ChatID:        chatID,
		SourceKind:    sourceKindWarmup,
		SourceRefJson: []byte("{}"),
	})
	if err != nil {
		return noop, fmt.Errorf("upsert warmup thread: %w", err)
	}

	row, err := queries.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID:   assistant.ProjectID,
		AssistantID: assistant.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := queries.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
			AssistantThreadID: threadID,
			AssistantID:       assistant.ID,
			ProjectID:         assistant.ProjectID,
			Backend:           s.runtime.Backend(),
			State:             runtimeStateStarting,
		}); err != nil {
			return noop, fmt.Errorf("reserve v2 assistant runtime for warmup: %w", err)
		}
	case err != nil:
		return noop, fmt.Errorf("lookup v2 runtime for warmup: %w", err)
	case row.AssistantThreadID != threadID || row.State == runtimeStateExpiring:
		// A real thread owns the boot, or the row is mid-teardown — either
		// way the existing workflows drive the lifecycle from here.
		return noop, nil
		// Else: the warmup thread already owns a live row (an earlier signal
		// was lost or the workflow died mid-boot) — fall through and
		// re-signal; SignalWithStart and ProcessThreadEvents are idempotent.
	}

	if err := tx.Commit(ctx); err != nil {
		return noop, fmt.Errorf("commit assistant warmup tx: %w", err)
	}
	return WarmupThreadResult{ThreadID: threadID, ProjectID: assistant.ProjectID, ShouldSignal: true}, nil
}

// ReleaseWarmupRuntime best-effort clears a runtime row reserved for the
// warmup thread whose workflow signal could not be delivered. Only a
// still-starting row owned by that thread is touched — anything else means a
// workflow or real traffic took over.
func (s *ServiceCore) ReleaseWarmupRuntime(ctx context.Context, projectID, assistantID, warmupThreadID uuid.UUID) {
	// The signal often fails precisely because ctx was canceled or timed
	// out; detach so the cleanup's own DB writes can still land.
	ctx = context.WithoutCancel(ctx)

	row, err := assistantrepo.New(s.db).GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	if err != nil || row.AssistantThreadID != warmupThreadID || row.State != runtimeStateStarting {
		return
	}
	// Re-check `starting` inside the UPDATE (every state slot pinned to it):
	// if the signal was actually delivered and the workflow advanced the row
	// between the read above and this write, no-op instead of tearing down a
	// live runtime.
	if err := assistantrepo.New(s.db).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{
		State:         runtimeStateFailed,
		ProjectID:     projectID,
		RuntimeID:     row.ID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateStarting,
		ExpiringState: runtimeStateStarting,
	}); err != nil {
		s.logger.WarnContext(ctx, "release warmup runtime reservation failed",
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogError(err),
		)
	}
}

func (s *ServiceCore) ProcessThreadEvents(ctx context.Context, projectID, threadID uuid.UUID) (ProcessThreadEventsResult, error) {
	bootstrappedRuntime := false
	thread, assistant, runtimeRecord, err := s.loadThreadContext(ctx, projectID, threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The runtime row was retired (reaper, expire, manual stop) between
		// this thread being admitted and the activity executing. The events
		// are still pending, so hand back to the coordinator: re-admission
		// reserves a fresh runtime row and re-dispatches. Failing the
		// activity here instead would burn its retry budget against a
		// tombstone and drop the turn.
		row, rerr := assistantrepo.New(s.db).ResolveThreadCorrelation(ctx, assistantrepo.ResolveThreadCorrelationParams{
			ThreadID:  threadID,
			ProjectID: projectID,
		})
		if rerr != nil {
			return ProcessThreadEventsResult{}, fmt.Errorf("resolve thread for retry admission: %w", errors.Join(err, rerr))
		}
		return ProcessThreadEventsResult{
			AssistantID:         row.AssistantID,
			WarmUntil:           time.Time{},
			WarmTTLSeconds:      0,
			RuntimeActive:       false,
			RetryAdmission:      true,
			ProcessedAnyEvent:   false,
			BootstrappedRuntime: false,
		}, nil
	}
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
		_ = s.stopRuntimeRecord(ctx, thread.ProjectID, runtimeRecord.ID, runtimeStateFailed)
		return ProcessThreadEventsResult{
			AssistantID:         assistant.ID,
			WarmUntil:           time.Time{},
			WarmTTLSeconds:      assistant.WarmTTLSeconds,
			RuntimeActive:       false,
			RetryAdmission:      true,
			ProcessedAnyEvent:   false,
			BootstrappedRuntime: bootstrappedRuntime,
		}, nil
	}
	if err := s.updateRuntimeEnsureResult(ctx, &runtimeRecord, ensureResult); err != nil {
		return ProcessThreadEventsResult{}, err
	}

	if runtimeRecord.State == runtimeStateStarting {
		if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds)*time.Second)); err != nil {
			return ProcessThreadEventsResult{}, err
		}
		bootstrappedRuntime = true
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
		runErr := s.processEventTurn(turnCtx, thread, assistant, runtimeRecord, event)
		stopLeaseHeartbeat()
		if runErr != nil {
			s.logger.WarnContext(ctx, "assistant turn failed",
				attr.SlogAssistantThreadID(thread.ID.String()),
				attr.SlogAssistantEventID(event.ID.String()),
				attr.SlogAssistantAttempt(event.Attempts),
				attr.SlogError(runErr),
			)
			s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "turn_failed", "assistant turn failed", "ERROR", runErr)

			teardownExhausted := errors.Is(runErr, ErrRuntimeUnhealthy) && event.Attempts >= maxRuntimeTeardowns
			outcome := turnErrorBucket(runErr)
			if teardownExhausted {
				outcome = turnOutcomeRuntimeUnhealthyExhausted
			}
			s.recordTurnClassification(turnCtx, assistant.ID, thread.ID, outcome)

			// Runtime-level failure (dead VM, connection refused, missing
			// state). Tear down the runtime row and leave the event in
			// 'processing' — do NOT reset it to 'pending', or the outer
			// workflow retry will hammer the dead VM with duplicate turns.
			// A reaper reclaims stuck 'processing' events after a grace
			// window so they flow through cleanly under a fresh VM.
			if errors.Is(runErr, ErrRuntimeUnhealthy) {
				// An event that has torn down this many runtimes is far more
				// likely a deterministic failure misread as infra than a
				// transient blip, so fail it terminally instead of re-admitting
				// it forever. Still request admission afterwards: the failed
				// event is no longer claimable, but any other pending event on
				// the thread must get a fresh runtime rather than sit idle.
				if teardownExhausted {
					s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant event exceeded runtime teardown limit", "ERROR", runErr)
					// Each step gets its own detached budget so a slow best-effort
					// runtime Stop cannot starve the two writes that actually break
					// the loop: failEvent makes the event terminal, and
					// stopRuntimeRecord frees the per-thread runtime slot so the
					// re-admission below reserves a fresh VM instead of redispatching
					// onto this dead one.
					failCtx, cancelFail := context.WithTimeout(context.WithoutCancel(ctx), teardownCapCleanupTimeout)
					err := s.failEvent(failCtx, thread.ProjectID, event.ID, fmt.Errorf("exceeded %d runtime teardowns: %w", maxRuntimeTeardowns, runErr))
					cancelFail()
					if err != nil {
						return ProcessThreadEventsResult{}, err
					}
					stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), teardownCapCleanupTimeout)
					_ = s.runtime.Stop(stopCtx, runtimeRecord)
					cancelStop()
					recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), teardownCapCleanupTimeout)
					recordErr := s.stopRuntimeRecord(recordCtx, thread.ProjectID, runtimeRecord.ID, runtimeStateFailed)
					cancelRecord()
					if recordErr != nil {
						// The slot is still held by an active row; re-admitting now
						// would redispatch siblings onto the dead runtime. Surface the
						// error so the workflow retries and frees the slot first.
						return ProcessThreadEventsResult{}, fmt.Errorf("free runtime slot after teardown cap: %w", recordErr)
					}
					return ProcessThreadEventsResult{
						AssistantID:         assistant.ID,
						WarmUntil:           time.Time{},
						WarmTTLSeconds:      assistant.WarmTTLSeconds,
						RuntimeActive:       false,
						RetryAdmission:      true,
						ProcessedAnyEvent:   processedAny,
						BootstrappedRuntime: bootstrappedRuntime,
					}, nil
				}
				_ = s.runtime.Stop(ctx, runtimeRecord)
				_ = s.stopRuntimeRecord(ctx, thread.ProjectID, runtimeRecord.ID, runtimeStateFailed)
				return ProcessThreadEventsResult{
					AssistantID:         assistant.ID,
					WarmUntil:           time.Time{},
					WarmTTLSeconds:      assistant.WarmTTLSeconds,
					RuntimeActive:       false,
					RetryAdmission:      true,
					ProcessedAnyEvent:   processedAny,
					BootstrappedRuntime: bootstrappedRuntime,
				}, nil
			}

			// First-attempt history corruption: write a trimmed generation,
			// tear the runtime down so the next admit /configures with it,
			// and re-pend the event. Further attempts fall through to the
			// terminal-fail branch so persistent corruption can't loop.
			if errors.Is(runErr, ErrHistoryCorrupted) && event.Attempts <= 1 {
				if healErr := s.selfHealCorruptHistory(ctx, thread.ChatID, thread.ProjectID); healErr != nil {
					s.logger.ErrorContext(ctx, "assistant self-heal failed",
						attr.SlogAssistantThreadID(thread.ID.String()),
						attr.SlogAssistantEventID(event.ID.String()),
						attr.SlogError(healErr),
					)
					s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant self-heal failed", "ERROR", healErr)
					if err := s.failEvent(ctx, thread.ProjectID, event.ID, fmt.Errorf("self-heal failed: %w", healErr)); err != nil {
						return ProcessThreadEventsResult{}, err
					}
					warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
					if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
						return ProcessThreadEventsResult{}, err
					}
					return ProcessThreadEventsResult{
						AssistantID:         assistant.ID,
						WarmUntil:           warmUntil,
						WarmTTLSeconds:      assistant.WarmTTLSeconds,
						RuntimeActive:       true,
						RetryAdmission:      false,
						ProcessedAnyEvent:   processedAny,
						BootstrappedRuntime: bootstrappedRuntime,
					}, nil
				}
				s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_self_heal", "assistant history self-heal applied", "WARN", runErr)
				_ = s.runtime.Stop(ctx, runtimeRecord)
				_ = s.stopRuntimeRecord(ctx, thread.ProjectID, runtimeRecord.ID, runtimeStateStopped)
				if err := s.resetEventToPending(ctx, thread.ProjectID, event.ID, runErr); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				return ProcessThreadEventsResult{
					AssistantID:         assistant.ID,
					WarmUntil:           time.Time{},
					WarmTTLSeconds:      assistant.WarmTTLSeconds,
					RuntimeActive:       false,
					RetryAdmission:      true,
					ProcessedAnyEvent:   processedAny,
					BootstrappedRuntime: bootstrappedRuntime,
				}, nil
			}

			// Upstream completion provider rejected the request (Anthropic 400
			// on a malformed message, OpenRouter rate limit, etc), or a live
			// runtime returned a deterministic 4xx. The runtime is fine —
			// replaying the same input would just reproduce it, so terminally
			// fail the event and keep the VM warm. Request admission so any
			// other pending event on the thread is drained on the warm runtime
			// instead of waiting out the warm timer; the failed event is no
			// longer claimable, so this cannot loop on it.
			if errors.Is(runErr, ErrCompletionFailed) || errors.Is(runErr, ErrHistoryCorrupted) {
				s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_terminal", "assistant event failed at completion provider", "ERROR", runErr)
				if err := s.failEvent(ctx, thread.ProjectID, event.ID, runErr); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
				if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
					return ProcessThreadEventsResult{}, err
				}
				return ProcessThreadEventsResult{
					AssistantID:         assistant.ID,
					WarmUntil:           warmUntil,
					WarmTTLSeconds:      assistant.WarmTTLSeconds,
					RuntimeActive:       true,
					RetryAdmission:      true,
					ProcessedAnyEvent:   processedAny,
					BootstrappedRuntime: bootstrappedRuntime,
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
					AssistantID:         assistant.ID,
					WarmUntil:           warmUntil,
					WarmTTLSeconds:      assistant.WarmTTLSeconds,
					RuntimeActive:       true,
					RetryAdmission:      false,
					ProcessedAnyEvent:   processedAny,
					BootstrappedRuntime: bootstrappedRuntime,
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
				AssistantID:         assistant.ID,
				WarmUntil:           warmUntil,
				WarmTTLSeconds:      assistant.WarmTTLSeconds,
				RuntimeActive:       true,
				RetryAdmission:      true,
				ProcessedAnyEvent:   processedAny,
				BootstrappedRuntime: bootstrappedRuntime,
			}, nil
		}

		if err := s.completeEvent(ctx, thread.ProjectID, event.ID); err != nil {
			return ProcessThreadEventsResult{}, err
		}
		s.emitAssistantTelemetry(turnCtx, assistant, thread, &runtimeRecord, &event, "event_completed", "assistant event completed", "INFO", nil)
		processedAny = true
	}

	warmUntil := time.Now().UTC().Add(time.Duration(assistant.WarmTTLSeconds) * time.Second)
	if err := s.setRuntimeActive(ctx, thread.ProjectID, runtimeRecord.ID, warmUntil); err != nil {
		return ProcessThreadEventsResult{}, err
	}
	return ProcessThreadEventsResult{
		AssistantID:         assistant.ID,
		WarmUntil:           warmUntil,
		WarmTTLSeconds:      assistant.WarmTTLSeconds,
		RuntimeActive:       true,
		RetryAdmission:      false,
		ProcessedAnyEvent:   processedAny,
		BootstrappedRuntime: bootstrappedRuntime,
	}, nil
}

func (s *ServiceCore) processEventTurn(
	ctx context.Context,
	thread assistantThreadRecord,
	assistant assistantRecord,
	runtime assistantRuntimeRecord,
	event assistantThreadEventRecord,
) error {
	mcpServers := s.currentRuntimeMCPServers(ctx, assistant)

	if prompt, ok := decodeMCPAuthTurn(ctx, s.logger, event); ok {
		// MCP auth resumption is a system event with no human sender — act as
		// the assistant's creator.
		turnToken, err := s.MintThreadScopedRuntimeToken(assistant, thread.ID, assistant.CreatedByUserID)
		if err != nil {
			return err
		}
		if err := s.runtime.RunTurn(ctx, runtime, thread.ID, event.ID.String(), turnToken, prompt, mcpServers); err != nil {
			return fmt.Errorf("run assistant turn: %w", err)
		}
		return nil
	}

	adapter, err := getSourceAdapter(thread.SourceKind)
	if err != nil {
		return err
	}
	prompt, err := adapter.DecodeTurn(event)
	if err != nil {
		return fmt.Errorf("decode assistant turn: %w", err)
	}
	turnToken, err := s.MintThreadScopedRuntimeToken(assistant, thread.ID, turnUserID(assistant, thread, event))
	if err != nil {
		return err
	}
	if err := s.runtime.RunTurn(ctx, runtime, thread.ID, event.ID.String(), turnToken, prompt, mcpServers); err != nil {
		return fmt.Errorf("run assistant turn: %w", err)
	}
	return nil
}

// currentRuntimeMCPServers builds the MCP server set the runner should be
// running with right now. The runner reconciles its live thread state
// against this list on every turn, so toolset edits (added/removed MCP
// servers) take effect on the next event without recycling the VM. Falls
// back to nil when the runtime server URL is not configured — the runner
// already has the bootstrap-time set and we'd rather skip reconcile than
// dispatch a turn with bogus URLs. Platform toolsets must be included so
// the reconcile target matches what bootstrap granted — otherwise the
// runner would treat them as removed and disconnect them mid-thread.
func (s *ServiceCore) currentRuntimeMCPServers(ctx context.Context, assistant assistantRecord) []runtimeMCPServer {
	serverURL := s.runtime.ServerURL()
	if serverURL == nil {
		return nil
	}
	platformSlugs, err := s.assistantPlatformSlugs(ctx, assistant)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve platform toolsets for mcp reconcile failed; skipping reconcile",
			attr.SlogError(err),
		)
		return nil
	}
	return resolveAssistantMCPServers(ctx, s.logger, serverURL, assistant.Toolsets, assistant.MCPServers, platformSlugs)
}

// assistantPlatformSlugs returns the platform toolset slugs granted to this
// assistant's runtime. Every assistant gets the base assistants toolset; the
// project's managed assistant additionally gets the managed-only toolset,
// which must never be reachable by any other assistant.
func (s *ServiceCore) assistantPlatformSlugs(ctx context.Context, assistant assistantRecord) ([]string, error) {
	platformSlugs := []string{platformtools.AssistantsPlatformToolsetSlug}
	switch managed, mErr := assistantrepo.New(s.db).GetManagedAssistantByProject(ctx, assistant.ProjectID); {
	case mErr == nil:
		if managed.ID == assistant.ID {
			platformSlugs = append(platformSlugs, platformtools.ManagedAssistantPlatformToolsetSlug)
		}
	case errors.Is(mErr, pgx.ErrNoRows):
		// Project has no managed assistant; managed-only tools stay ungranted.
	default:
		return nil, fmt.Errorf("resolve managed assistant: %w", mErr)
	}
	return platformSlugs, nil
}

// turnUserID returns the Gram user whose identity a turn should act under.
// Dashboard turns carry a Gram user id on the event payload (the sender), so
// MCP calls, audit attribution, and per-user RBAC reflect the actual sender
// rather than the assistant's creator. Other sources either don't carry a
// Gram user identity (cron/wake) or carry an external one (Slack), so they
// fall back to the creator.
func turnUserID(assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) string {
	if thread.SourceKind == sourceKindDashboard {
		var payload dashboardEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err == nil && payload.UserID != "" {
			return payload.UserID
		}
	}
	return assistant.CreatedByUserID
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

// MintThreadScopedRuntimeToken issues the JWT the runner uses for every
// outbound call originating from a specific thread (chat completions, MCP,
// platform tools). The ThreadID claim populates principal.ThreadID
// downstream, so platform tools that key on the calling thread (wake,
// memory, telemetry) keep working under the v2 single-VM-per-assistant
// runtime — the VM is shared but the auth identity is per-thread.
func (s *ServiceCore) MintThreadScopedRuntimeToken(assistant assistantRecord, threadID uuid.UUID, userID string) (string, error) {
	token, err := s.assistantTokens.Generate(assistanttokens.GenerateInput{
		OrgID:       assistant.OrganizationID,
		ProjectID:   assistant.ProjectID,
		UserID:      userID,
		AssistantID: assistant.ID,
		ThreadID:    threadID,
		TTL:         assistantRuntimeTokenTTL,
	})
	if err != nil {
		return "", fmt.Errorf("generate assistant execution token: %w", err)
	}
	return token, nil
}

// BuildThreadBootstrap composes the response the v2 runner pulls from
// /rpc/assistants.getThreadBootstrap when it first sees a thread. The
// caller is responsible for confirming the requesting principal is
// scoped to the thread's assistant before invoking this method.
func (s *ServiceCore) BuildThreadBootstrap(ctx context.Context, projectID, threadID, principalAssistantID uuid.UUID) (threadBootstrap, error) {
	logAttrs := []slog.Attr{
		attr.SlogProjectID(projectID.String()),
		attr.SlogAssistantID(principalAssistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
	}
	row, err := assistantrepo.New(s.db).LoadAssistantThreadForBootstrap(ctx, assistantrepo.LoadAssistantThreadForBootstrapParams{
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return threadBootstrap{}, oops.E(oops.CodeNotFound, nil, "assistant thread not found").LogError(ctx, s.logger, logAttrs...)
		}
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "load assistant thread").LogError(ctx, s.logger, logAttrs...)
	}
	if row.AssistantID != principalAssistantID {
		return threadBootstrap{}, oops.E(oops.CodeForbidden, nil, "thread does not belong to assistant").LogError(ctx, s.logger, logAttrs...)
	}

	thread := assistantThreadRecord{
		ID:            row.ID,
		AssistantID:   row.AssistantID,
		ProjectID:     row.ProjectID,
		CorrelationID: row.CorrelationID,
		ChatID:        row.ChatID,
		SourceKind:    row.SourceKind,
		SourceRefJSON: row.SourceRefJson,
		LastEventAt:   time.Time{},
	}
	assistant := assistantRecord{
		ID:              row.AssistantID,
		ProjectID:       row.ProjectID,
		OrganizationID:  row.OrganizationID,
		CreatedByUserID: conv.FromPGTextOrEmpty[string](row.CreatedByUserID),
		Name:            row.Name,
		Model:           row.Model,
		Instructions:    row.Instructions,
		Toolsets:        nil,
		MCPServers:      nil,
		WarmTTLSeconds:  conv.SafeInt(row.WarmTtlSeconds),
		MaxConcurrency:  conv.SafeInt(row.MaxConcurrency),
		Status:          row.Status,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
		DeletedAt:       row.DeletedAt,
	}
	if err := s.hydrateAssistantToolSources(ctx, assistant.ProjectID, &assistant); err != nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "load assistant tool sources").LogError(ctx, s.logger, logAttrs...)
	}

	runtimeServerURL := s.runtime.ServerURL()
	if runtimeServerURL == nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, nil, "assistant runtime server url not configured").LogError(ctx, s.logger, logAttrs...)
	}

	// The managed-assistant platform toolset is granted only to the project's
	// managed assistant; tools in it must not be reachable by any other
	// assistant.
	platformSlugs, err := s.assistantPlatformSlugs(ctx, assistant)
	if err != nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "resolve managed assistant").LogError(ctx, s.logger, logAttrs...)
	}

	// Misconfigured toolsets (no MCP slug, MCP disabled) are surfaced as
	// best-effort URLs rather than aborting the whole bootstrap. The runner
	// will discover the failure when it tries to list tools and the
	// assistant can tell the user which integration is broken.
	mcpServers := resolveAssistantMCPServers(ctx, s.logger, runtimeServerURL, assistant.Toolsets, assistant.MCPServers, platformSlugs)

	instructions, err := composeInstructions(assistant.Instructions, thread)
	if err != nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "compose assistant instructions").LogError(ctx, s.logger, logAttrs...)
	}

	history, err := s.loadChatHistory(ctx, thread.ChatID, thread.ProjectID)
	if err != nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "load assistant chat history").LogError(ctx, s.logger, logAttrs...)
	}

	completionsEndpoint := runtimeServerURL.JoinPath("chat", "completions")
	completionsQuery := completionsEndpoint.Query()
	completionsQuery.Set("unstable_normalizeOutboundMessages", "1")
	completionsEndpoint.RawQuery = completionsQuery.Encode()

	compaction := compactionPolicyFor(thread.SourceKind)
	if err := compaction.Validate(); err != nil {
		return threadBootstrap{}, oops.E(oops.CodeUnexpected, err, "build compaction policy").LogError(ctx, s.logger, logAttrs...)
	}

	return threadBootstrap{
		Model:          assistant.Model,
		Instructions:   instructions,
		CompletionsURL: completionsEndpoint.String(),
		ChatID:         thread.ChatID.String(),
		MCPServers:     mcpServers,
		History:        history,
		ContextWindow:  s.resolveAssistantContextWindow(ctx, assistant.Model),
		Compaction:     compaction,
		SourceRefJSON:  thread.SourceRefJSON,
	}, nil
}

// assistantRuntimeTokenTTL bounds the lifetime of tokens handed to runners.
// Long enough to cover a typical 30-min turn plus bootstrap slack; short
// enough that a leaked token ages out well before the thread retires. Fresh
// tokens are pushed on /configure and on every /turn, so this is the upper
// bound between refreshes for an idle runtime.
const assistantRuntimeTokenTTL = 60 * time.Minute

// mcpAuthAddendum is source-agnostic framing for MCP auth: who may see an
// AuthURL, when auth events appear, and what each event carries. Per-source
// delivery mechanics (Slack Block Kit button, dashboard Markdown link, etc.)
// live in each adapter's OutputChannelGuidance.
const mcpAuthAddendum = `## MCP authentication

OAuth + MCP auth are owner-only: only owner can sign in and complete flow. Owner = the person who set this assistant up — NOT simply whoever triggered the current turn. When your instructions record the owner's identity (an "Owner" entry, e.g. a Slack handle + user ID), treat the requester as owner only if they match it. AuthURL must never be visible to non-owner. Don't pre-emptively call tools or surface auth URLs for toolsets not yet needed — only call tools required for current task. Auth events appear only as consequence of a needed tool call.

Two MCP auth events may appear in thread, each as <message-context> block with EventType and field lines.

- EventType "assistant_mcp_auth_required" carries AuthURL. Surface AuthURL to owner verbatim (don't shorten/summarize/rewrite). Reference MCP server by MCPSlug, not MCPServerID. Never expose AuthURL to non-owners or in any channel readable by non-owners. If no owner identity is recorded on this surface, deliver the URL privately to the requester but say explicitly that it should be completed by the assistant's owner, so an unexpected prompt isn't mistaken for a failure. If owner identity is recorded and the requester is not the owner, don't surface the URL to them — tell them (without URL) that only the owner can complete auth, naming the owner, and still deliver the AuthURL to the owner privately when this surface can reach them (per the output preferences below). If owner identity is recorded but no private route to the owner exists, stop without posting the URL. The per-surface output preferences below describe how to deliver the URL on this surface.

- EventType "assistant_mcp_auth" reports result. Status "success" + still need server → call mcp_force_reconnect with server_id = MCPServerID, then continue task. Status "failed" → inform the user the auth attempt failed, include ErrorDescription if present.`

func composeInstructions(base string, thread assistantThreadRecord) (string, error) {
	adapter, err := getSourceAdapter(thread.SourceKind)
	if err != nil {
		return "", err
	}
	ctxBlock, err := adapter.ThreadContext(thread.SourceRefJSON)
	if err != nil {
		return "", fmt.Errorf("load assistant thread context: %w", err)
	}
	parts := make([]string, 0, 4)
	if base != "" {
		parts = append(parts, base)
	}
	parts = append(parts, mcpAuthAddendum)
	if guidance := adapter.OutputChannelGuidance(); guidance != "" {
		parts = append(parts, guidance)
	}
	if ctxBlock != "" {
		parts = append(parts, ctxBlock)
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveAssistantMCPServers(ctx context.Context, logger *slog.Logger, serverURL *url.URL, toolsets []assistantToolsetRow, mcpServers []assistantMCPServerRow, platformToolsets []string) []runtimeMCPServer {
	servers := make([]runtimeMCPServer, 0, len(toolsets)+len(mcpServers)+len(platformToolsets))
	for _, t := range toolsets {
		// Misconfiguration (no MCP slug, MCP disabled) is a tenant-side
		// problem, not a server fault. Skip the broken toolset and let
		// the rest of the thread admit — the assistant just won't see
		// these tools.
		if !t.McpEnabled || !t.McpSlug.Valid || t.McpSlug.String == "" {
			logger.WarnContext(ctx, "skipping assistant toolset that is not MCP-reachable",
				attr.SlogToolsetID(t.ToolsetID.String()),
				attr.SlogToolsetSlug(t.ToolsetSlug),
				attr.SlogToolsetMCPSlug(t.McpSlug.String),
				attr.SlogToolsetMCPEnabled(t.McpEnabled),
			)
			continue
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

	// MCP servers attached directly to the assistant (assistant_mcp_servers) —
	// remote- or tunnelled-backed servers that have no toolsets row, so they
	// can't ride the toolset branch above. The runner treats every entry
	// uniformly as an MCP endpoint to connect to, so these need only the same
	// {ID, URL, Headers} shape: the public /mcp/{endpoint} path that
	// serveRemoteBackend already proxies, plus an optional bound environment.
	// loadAssistantMcpServers already dropped rows without a Gram-hosted
	// endpoint. ServerSlug is the runtime ID (agentkit namespaces tool names by
	// it, 64-char cap); it shares a slug space with toolsets only by
	// coincidence, which the attach path is responsible for keeping distinct.
	for _, m := range mcpServers {
		if m.EndpointSlug == "" {
			continue
		}
		headers := map[string]string{}
		if m.EnvironmentSlug.Valid && m.EnvironmentSlug.String != "" {
			headers["Gram-Environment"] = m.EnvironmentSlug.String
		}
		id := m.EndpointSlug
		if m.ServerSlug.Valid && m.ServerSlug.String != "" {
			id = m.ServerSlug.String
		}
		servers = append(servers, runtimeMCPServer{
			ID:      id,
			URL:     serverURL.JoinPath("mcp", m.EndpointSlug).String(),
			Headers: headers,
		})
	}

	// Platform toolsets granted to this runtime (caller-determined); not
	// surfaced as user-managed toolsets and not persisted in
	// assistant_toolsets so users can't detach them. The "_p-" prefix
	// marks the runtime server ID as platform-issued and gives it enough
	// distance from any plausible user toolset slug (the current SlugPattern
	// still allows leading "_", so this is convention, not a hard fence —
	// tightening the pattern is a separate follow-up). The URL slug stays
	// the public platform slug so warm runners keep resolving across
	// deploys; only the in-process server ID is shortened, since it is
	// concatenated into the agentkit MCP tool name which has a 64-char cap.
	for _, slug := range platformToolsets {
		servers = append(servers, runtimeMCPServer{
			ID:      "_p-" + slug,
			URL:     platformtools.PlatformToolsetURL(serverURL, slug),
			Headers: nil,
		})
	}

	return servers
}

func (s *ServiceCore) ProcessThreadEventsByThreadID(ctx context.Context, projectID, threadID uuid.UUID) (ProcessThreadEventsResult, error) {
	return s.ProcessThreadEvents(ctx, projectID, threadID)
}

// ExpireThreadRuntime tears down an idle runtime, guarding the TOCTOU between
// the workflow's warm timer and a new turn being dispatched. The CAS to
// `expiring` blocks new dispatches; the post-CAS /state poll catches any turn
// that slipped in (the runner clears idle_seconds synchronously inside /turn).
func (s *ServiceCore) ExpireThreadRuntime(ctx context.Context, projectID, threadID uuid.UUID, warmTTLSeconds int) (ExpireThreadRuntimeResult, error) {
	q := assistantrepo.New(s.db)

	row, err := q.BeginExpireAssistantRuntime(ctx, assistantrepo.BeginExpireAssistantRuntimeParams{
		ExpiringState: runtimeStateExpiring,
		ProjectID:     projectID,
		ThreadID:      threadID,
		ActiveState:   runtimeStateActive,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No active row, or another actor (Stop, reaper, manual API) moved it
		// before us. The runtime is going away regardless — report stopped so
		// the workflow exits its expiry loop.
		return ExpireThreadRuntimeResult{Stopped: true, RemainingSeconds: 0}, nil
	case err != nil:
		return ExpireThreadRuntimeResult{}, fmt.Errorf("begin expire assistant runtime: %w", err)
	}

	runtimeRecord := assistantRuntimeRecord{
		ID:                  row.ID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.AssistantID,
		ProjectID:           row.ProjectID,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}

	status, statusErr := s.runtime.Status(ctx, runtimeRecord)
	if statusErr != nil {
		// Runtime is already gone or unhealthy — fall through to Stop so the
		// row + backend resources get cleaned up.
		s.logger.WarnContext(ctx, "runtime status failed during expire; tearing down",
			attr.SlogAssistantThreadID(threadID.String()),
			attr.SlogError(statusErr),
		)
	} else if remaining := warmRemainingSeconds(status.IdleSeconds, warmTTLSeconds); remaining > 0 {
		// A turn slipped in between the workflow's warm timer and our CAS.
		// Revert to active and let the workflow re-arm with the remaining
		// window measured against the runner's current idle.
		warmUntil := time.Now().UTC().Add(time.Duration(remaining) * time.Second)
		revertErr := q.RevertExpireAssistantRuntimeToActive(ctx, assistantrepo.RevertExpireAssistantRuntimeToActiveParams{
			ActiveState:   runtimeStateActive,
			WarmUntil:     conv.ToPGTimestamptz(warmUntil),
			RuntimeID:     runtimeRecord.ID,
			ProjectID:     projectID,
			ExpiringState: runtimeStateExpiring,
		})
		if revertErr != nil {
			return ExpireThreadRuntimeResult{}, fmt.Errorf("revert expire assistant runtime: %w", revertErr)
		}
		return ExpireThreadRuntimeResult{Stopped: false, RemainingSeconds: remaining}, nil
	}

	if err := s.runtime.Stop(ctx, runtimeRecord); err != nil {
		return ExpireThreadRuntimeResult{}, fmt.Errorf("stop assistant runtime backend: %w", err)
	}
	if err := s.stopRuntimeRecord(ctx, projectID, runtimeRecord.ID, runtimeStateStopped); err != nil {
		return ExpireThreadRuntimeResult{}, err
	}
	return ExpireThreadRuntimeResult{Stopped: true, RemainingSeconds: 0}, nil
}

// loadThreadContext joins assistant_thread → assistant → v2 assistant_runtime
// and hydrates the records ProcessThreadEvents needs. The runtime row is
// keyed on (project_id, assistant_id) and serves every thread under the
// assistant; AssistantThreadID on the returned record is uuid.Nil so the
// Fly backend dispatches to /threads/{id}/turn.
func (s *ServiceCore) loadThreadContext(ctx context.Context, projectID, threadID uuid.UUID) (assistantThreadRecord, assistantRecord, assistantRuntimeRecord, error) {
	row, err := assistantrepo.New(s.db).LoadThreadContextV2(ctx, assistantrepo.LoadThreadContextV2Params{
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
		MCPServers:      nil,
		WarmTTLSeconds:  conv.SafeInt(row.WarmTtlSeconds),
		MaxConcurrency:  conv.SafeInt(row.MaxConcurrency),
		Status:          row.Status,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
		DeletedAt:       row.DeletedAt,
	}
	runtime := assistantRuntimeRecord{
		ID:                  row.RuntimeID,
		AssistantThreadID:   uuid.Nil,
		AssistantID:         row.RuntimeAssistantID,
		ProjectID:           row.RuntimeProjectID,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}
	if err := s.hydrateAssistantToolSources(ctx, assistant.ProjectID, &assistant); err != nil {
		return assistantThreadRecord{}, assistantRecord{}, assistantRuntimeRecord{}, err
	}
	return thread, assistant, runtime, nil
}

const (
	selfHealUserMessageCap    = 5
	selfHealUserMessageMaxLen = 1000
)

// selfHealRecoveryNoticeTemplate prefixes the trimmed generation when the
// upstream provider rejects the replayed transcript. The "%d" slot is the
// number of user messages retained; the second is the per-message rune cap.
// Sent as a user-role row because loadChatHistory drops system rows.
const selfHealRecoveryNoticeTemplate = "[gram self-heal] Earlier conversation history was rejected by the inference provider as malformed and has been discarded. " +
	"The %d user message(s) that follow are the most recent ones, each truncated to %d characters. " +
	"Prior context (including any tool calls and their results) is not available. " +
	"Before asking the user to repeat themselves, try to recover the lost context using your available tools — " +
	"e.g. search prior messages or threads in whichever channel this conversation lives in, look up referenced records, " +
	"or re-fetch any IDs that appear in the surviving messages. Only ask the user to restate context if your tools can't recover it."

// selfHealCorruptHistory writes a fresh generation containing a leading
// recovery notice followed by the last selfHealUserMessageCap user-role
// messages (each truncated to selfHealUserMessageMaxLen runes). The next
// /configure pulls this generation as the live history; assistant/tool
// turns are dropped — they're the most likely source of the rejection.
func (s *ServiceCore) selfHealCorruptHistory(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID) error {
	if s.chatWriter == nil {
		return fmt.Errorf("self-heal: chat writer not configured")
	}

	messages, err := chatrepo.New(s.db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("self-heal: list chat messages: %w", err)
	}

	var currentGen int32
	userMessages := make([]chatrepo.ChatMessage, 0, len(messages))
	for _, m := range messages {
		if m.Generation > currentGen {
			currentGen = m.Generation
		}
		if m.Role == "user" {
			userMessages = append(userMessages, m)
		}
	}
	if len(userMessages) > selfHealUserMessageCap {
		userMessages = userMessages[len(userMessages)-selfHealUserMessageCap:]
	}

	nextGen := currentGen + 1
	empty := conv.ToPGTextEmpty("")
	base := chatrepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "user",
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  empty,
		StorageError:     empty,
		Model:            empty,
		MessageID:        empty,
		ToolCallID:       empty,
		UserID:           empty,
		ExternalUserID:   empty,
		FinishReason:     empty,
		ToolCalls:        nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           empty,
		UserAgent:        empty,
		IpAddress:        empty,
		Source:           empty,
		ContentHash:      nil,
		Generation:       nextGen,
	}

	rows := make([]chatrepo.CreateChatMessageParams, 0, len(userMessages)+1)
	notice := base
	notice.Content = fmt.Sprintf(selfHealRecoveryNoticeTemplate, len(userMessages), selfHealUserMessageMaxLen)
	rows = append(rows, notice)
	for _, m := range userMessages {
		row := base
		row.Content = conv.TruncateString(m.Content, selfHealUserMessageMaxLen)
		row.UserID = m.UserID
		row.ExternalUserID = m.ExternalUserID
		rows = append(rows, row)
	}

	if _, err := s.chatWriter.Write(ctx, projectID, rows); err != nil {
		return fmt.Errorf("self-heal: write recovery generation: %w", err)
	}
	return nil
}

func (s *ServiceCore) loadChatHistory(ctx context.Context, chatID uuid.UUID, projectID uuid.UUID) ([]runtimeMessage, error) {
	// Earlier generations are audit-only snapshots; only the latest is the live transcript.
	messages, err := chatrepo.New(s.db).ListLatestGenerationChatMessages(ctx, chatrepo.ListLatestGenerationChatMessagesParams{
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
			CreatedAt:             row.CreatedAt.Time,
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

// recordTurnClassification counts a failed turn by its classifyTurnError
// bucket. The thread and assistant ids are attached explicitly — metric
// instruments do not inherit them from the context — so a runaway teardown
// loop can be grouped to a single thread.
func (s *ServiceCore) recordTurnClassification(ctx context.Context, assistantID, threadID uuid.UUID, outcome turnOutcome) {
	s.turnClassified.Add(ctx, 1, metric.WithAttributes(
		attr.AssistantTurnOutcome(string(outcome)),
		attr.AssistantID(assistantID.String()),
		attr.AssistantThreadID(threadID.String()),
	))
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

func (s *ServiceCore) stopRuntimeRecord(ctx context.Context, projectID, runtimeID uuid.UUID, state string) error {
	err := assistantrepo.New(s.db).StopAssistantRuntime(ctx, assistantrepo.StopAssistantRuntimeParams{
		State:         state,
		ProjectID:     projectID,
		RuntimeID:     runtimeID,
		StartingState: runtimeStateStarting,
		ActiveState:   runtimeStateActive,
		ExpiringState: runtimeStateExpiring,
	})
	if err != nil {
		return fmt.Errorf("stop assistant runtime: %w", err)
	}
	return nil
}
