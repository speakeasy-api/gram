package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	ProviderCursor              = "cursor"
	ProviderAnthropicCompliance = "anthropic_compliance"
	ProviderCodexCompliance     = "codex_compliance"
)

var codexExternalOrganizationIDPattern = regexp.MustCompile(`^org-[A-Za-z0-9_-]+$`)

// Sync schedules name the independent polling pipelines a config can run,
// each with its own ai_integration_syncs row (cadence, checkpoint, failure
// state). Every provider has a schedule that shares the provider's name and
// backs config-level reads; the other schedules get their own provider-style
// names. The Admin Analytics usage and cost reports are separate schedules so
// each tracks its own endpoint's finalization watermark and failure state
// independently.
const (
	ScheduleCursor                  = ProviderCursor
	ScheduleAnthropicCompliance     = ProviderAnthropicCompliance
	ScheduleAnthropicAnalyticsUsage = "anthropic_analytics_usage"
	ScheduleAnthropicAnalyticsCost  = "anthropic_analytics_cost"
	ScheduleCodexCompliance         = ProviderCodexCompliance
)

// Sync kinds record how a schedule checkpoints progress.
const (
	// SyncKindCursor schedules resume from an opaque pagination token
	// (last_cursor_id).
	SyncKindCursor = "cursor"
	// SyncKindTime schedules resume from poll_watermark_at.
	SyncKindTime = "time"
)

// Stream kinds classify what a schedule's stream carries: discrete events or
// aggregated metrics.
const (
	StreamKindEvents  = "events"
	StreamKindMetrics = "metrics"
)

// Stream identifiers are the product-level names for the data each sync
// schedule imports. They are the stable, user-facing handle for a stream
// (shown in the dashboard and returned by listSchedules) and are meant to
// eventually tag the imported data itself. Dotted lowercase, named after the
// product surface the data comes from rather than the provider API used to
// fetch it.
const (
	StreamCursorUsage = "cursor.usage"
	// The claude.chat.* streams all carry Claude Chat (claude.ai web and
	// desktop) data — the Compliance and Admin Analytics APIs report on the
	// Chat surface, not Claude API usage — so they share a chat namespace
	// segment.
	StreamClaudeChatMessage     = "claude.chat.message"
	StreamClaudeChatUsageTokens = "claude.chat.usage.tokens"
	StreamClaudeChatCostUSD     = "claude.chat.cost.usd"
	StreamCodexCostUSD          = "codex.cost.usd"
)

// streamInfo names the product-level stream a schedule writes.
type streamInfo struct {
	name string
	kind string
}

// streamForSchedule maps a sync schedule to its stream. The zero value marks
// a schedule with no registered stream (e.g. an unknown legacy schedule);
// callers omit stream metadata in that case.
func streamForSchedule(schedule string) streamInfo {
	switch schedule {
	case ScheduleCursor:
		// Cursor's Admin API serves aggregated per-user usage and spend
		// records (tokens, cost) that land as usage metrics, not discrete
		// activity events like Claude chat messages.
		return streamInfo{name: StreamCursorUsage, kind: StreamKindMetrics}
	case ScheduleAnthropicCompliance:
		return streamInfo{name: StreamClaudeChatMessage, kind: StreamKindEvents}
	case ScheduleAnthropicAnalyticsUsage:
		return streamInfo{name: StreamClaudeChatUsageTokens, kind: StreamKindMetrics}
	case ScheduleAnthropicAnalyticsCost:
		return streamInfo{name: StreamClaudeChatCostUSD, kind: StreamKindMetrics}
	case ScheduleCodexCompliance:
		return streamInfo{name: StreamCodexCostUSD, kind: StreamKindMetrics}
	default:
		return streamInfo{name: "", kind: ""}
	}
}

// syncSchedule pairs a schedule name with its checkpointing kind.
type syncSchedule struct {
	schedule string
	kind     string
}

// providerSyncSchedule returns the schedule that shares its provider's name.
// It backs config-level management API reads and is the schedule legacy
// pre-schedule sync rows are adopted as.
func providerSyncSchedule(provider string) syncSchedule {
	switch provider {
	case ProviderAnthropicCompliance:
		return syncSchedule{schedule: ScheduleAnthropicCompliance, kind: SyncKindCursor}
	case ProviderCodexCompliance:
		return syncSchedule{schedule: ScheduleCodexCompliance, kind: SyncKindTime}
	default:
		return syncSchedule{schedule: ScheduleCursor, kind: SyncKindTime}
	}
}

// syncSchedulesFor lists every independent sync schedule a provider's configs
// run. All schedules are peers; one of them shares the provider's name (see
// providerSyncSchedule).
func syncSchedulesFor(provider string) []syncSchedule {
	switch provider {
	case ProviderAnthropicCompliance:
		return []syncSchedule{
			providerSyncSchedule(provider),
			{schedule: ScheduleAnthropicAnalyticsUsage, kind: SyncKindTime},
			{schedule: ScheduleAnthropicAnalyticsCost, kind: SyncKindTime},
		}
	case ProviderCodexCompliance:
		return []syncSchedule{providerSyncSchedule(provider)}
	default:
		return []syncSchedule{providerSyncSchedule(provider)}
	}
}

const (
	initialUsagePollLookback             = time.Hour * 24
	cursorUsagePollInterval              = time.Hour
	anthropicComplianceUsagePollInterval = 5 * time.Minute
	codexComplianceUsagePollInterval     = 5 * time.Minute
	maxUsagePollErrorMessage             = 4000

	// codexComplianceInitialLookback bounds the first Codex compliance
	// ingest. Unlike Cursor/Anthropic, a long backfill is cheap here — COSTS
	// files are hourly per-user aggregates, a handful of small files per day —
	// and real Codex activity typically predates the key being configured, so
	// a 24h window would miss most of an org's history.
	codexComplianceInitialLookback = 30 * 24 * time.Hour

	// anthropicAnalyticsPollInterval is the delay between Admin Analytics API
	// polls. The analytics export is refreshed roughly every 4 hours, so
	// polling more often only re-reads the same watermark.
	anthropicAnalyticsPollInterval = 4 * time.Hour
	// anthropicAnalyticsInitialLookback bounds the first analytics ingest for
	// a config that has never synced.
	anthropicAnalyticsInitialLookback = 24 * time.Hour
)

const (
	InitialUsagePollLookback          = initialUsagePollLookback
	CodexComplianceInitialLookback    = codexComplianceInitialLookback
	AnthropicAnalyticsInitialLookback = anthropicAnalyticsInitialLookback
)

// initialPollLookbackForProvider returns how far back a provider's first poll
// reaches: on config creation the poller falls back to it when no watermark is
// set, and on API key/org changes the watermark is reseeded this far in the
// past.
func initialPollLookbackForProvider(provider string) time.Duration {
	if provider == ProviderCodexCompliance {
		return codexComplianceInitialLookback
	}
	return initialUsagePollLookback
}

// pollIntervalForSchedule returns the delay between runs of one independent
// sync schedule. Unknown schedules fall back to the Cursor interval.
func pollIntervalForSchedule(schedule string) time.Duration {
	switch schedule {
	case ScheduleAnthropicCompliance:
		return anthropicComplianceUsagePollInterval
	case ScheduleAnthropicAnalyticsUsage, ScheduleAnthropicAnalyticsCost:
		return anthropicAnalyticsPollInterval
	case ScheduleCodexCompliance:
		return codexComplianceUsagePollInterval
	default:
		return cursorUsagePollInterval
	}
}

type Store struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
}

type Config struct {
	ID                     uuid.UUID
	SyncID                 uuid.UUID
	OrganizationID         string
	Provider               string
	ProjectID              uuid.UUID
	ExternalOrganizationID *string
	BillingMode            string
	APIKey                 string
	Enabled                bool
	PollWatermarkAt        time.Time
	PollCheckpoint         timewindowpoller.PollCheckpoint
	NextPollAfter          time.Time
	LastPollError          string
	LastPollFailedAt       time.Time
	LastPollSuccessAt      time.Time
	ConsecutiveFailures    int32
	LastCursor             string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type UsagePollCandidate struct {
	SyncID           uuid.UUID
	OrganizationID   string
	OrganizationSlug string
	Provider         string
	Schedule         string
	Kind             string
}

type SyncSchedule struct {
	ID                  uuid.UUID
	Schedule            string
	Kind                string
	NextPollAfter       time.Time
	LastPollError       string
	LastPollFailedAt    time.Time
	LastPollSuccessAt   time.Time
	ConsecutiveFailures int32
	AutoPausedAt        time.Time
	DisabledAt          time.Time
}

type UpsertResult struct {
	Config               Config
	Row                  *repo.AiIntegrationConfig
	CreatedNewGeneration bool
}

func NewStore(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Store {
	return &Store{
		logger: logger.With(attr.SlogComponent("aiintegrations")),
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
	}
}

func normalizeProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "", oops.E(oops.CodeInvalid, nil, "provider is required")
	}
	switch provider {
	case ProviderCursor, ProviderAnthropicCompliance, ProviderCodexCompliance:
	default:
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider: %s", provider)
	}
	return provider, nil
}

func (s *Store) loadForOrgAndProviderRow(ctx context.Context, orgID string, provider string) (Config, *repo.GetConfigByOrgAndProviderRow, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}

	row, err := s.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{
		OrganizationID: orgID,
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyConfig(orgID, provider), nil, nil
	case err != nil:
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to load ai integration config")
	}

	cfg, err := s.configFromGetRow(row)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}
	return cfg, &row, nil
}

func (s *Store) upsertWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string, apiKey string, apiKeySupplied bool, enabled bool, externalOrganizationID *string, billingMode *string, resetPollWatermarkAt *time.Time) (UpsertResult, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return UpsertResult{}, err
	}

	if providerRequiresExternalOrganizationID(provider) && externalOrganizationID == nil {
		return UpsertResult{}, oops.E(oops.CodeInvalid, nil, "external_organization_id is required for %s", provider)
	}
	if provider == ProviderCodexCompliance && externalOrganizationID != nil {
		trimmedExternalOrganizationID := strings.TrimSpace(*externalOrganizationID)
		if !codexExternalOrganizationIDPattern.MatchString(trimmedExternalOrganizationID) {
			return UpsertResult{}, oops.E(oops.CodeInvalid, nil, "external_organization_id must be an OpenAI organization ID starting with org- for %s", provider)
		}
		externalOrganizationID = &trimmedExternalOrganizationID
	}

	q := repo.New(dbtx)
	projectID, err := q.GetFirstProjectByOrganization(ctx, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpsertResult{}, oops.E(oops.CodeInvalid, err, "organization has no project for ai integration attribution")
		}
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to resolve ai integration project")
	}

	var row repo.AiIntegrationConfig
	createdNewGeneration := apiKeySupplied
	if apiKeySupplied {
		if err := q.SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
			OrganizationID: orgID,
			Provider:       provider,
		}); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to replace ai integration config")
		}
		encrypted, err := s.encryptAPIKey(apiKey)
		if err != nil {
			return UpsertResult{}, err
		}
		row, err = q.InsertConfig(ctx, repo.InsertConfigParams{
			OrganizationID:         orgID,
			Provider:               provider,
			ProjectID:              projectID,
			ExternalOrganizationID: conv.PtrToPGTextEmpty(externalOrganizationID),
			ApiKeyEncrypted:        encrypted,
			Enabled:                enabled,
			BillingMode:            conv.PtrToPGTextEmpty(billingMode),
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration config")
		}
	} else {
		row, err = q.UpdateConfigSettings(ctx, repo.UpdateConfigSettingsParams{
			OrganizationID:         orgID,
			Provider:               provider,
			ProjectID:              projectID,
			ExternalOrganizationID: conv.PtrToPGTextEmpty(externalOrganizationID),
			Enabled:                enabled,
			BillingMode:            conv.PtrToPGTextEmpty(billingMode),
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration config")
		}
	}

	providerSched := providerSyncSchedule(provider)
	// Every provider schedule gets its own sync row, due immediately. The
	// initial watermark depends on the schedule's kind: time-kind schedules
	// start at epoch, the never-synced sentinel that has the time-window
	// poller begin with its initial lookback; cursor-kind schedules
	// checkpoint through last_cursor_id, so their watermark starts at now.
	var syncRow repo.EnsureSyncRow
	for _, sched := range syncSchedulesFor(provider) {
		initialAt := time.Now().UTC()
		if sched.kind == SyncKindTime {
			initialAt = epochTime()
		}
		r, err := q.EnsureSync(ctx, repo.EnsureSyncParams{
			AiIntegrationConfigID: row.ID,
			Schedule:              sched.schedule,
			Kind:                  sched.kind,
			PollWatermarkAt:       conv.ToPGTimestamptz(initialAt),
			NextPollAfter:         conv.ToPGTimestamptz(initialAt),
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration sync")
		}
		if sched.schedule == providerSched.schedule {
			syncRow = r
		}
	}
	// Saving the integration is the user's "try again" signal: lift any
	// automatic pauses so schedules stopped over a rejected configuration
	// start polling again with a fresh failure budget.
	if err := q.ClearSyncSchedulePauses(ctx, row.ID); err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to clear ai integration sync pauses")
	}
	if resetPollWatermarkAt != nil {
		syncRow.PollWatermarkAt = conv.ToPGTimestamptz(*resetPollWatermarkAt)
		syncRow.NextPollAfter = conv.ToPGTimestamptz(resetPollWatermarkAt.UTC().Add(pollIntervalForSchedule(providerSched.schedule)))
		syncRow.LastPollError = pgtype.Text{String: "", Valid: false}
		syncRow.LastPollFailedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.LastPollSuccessAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.ConsecutiveFailures = 0
		syncRow.PollCheckpoint = pgtype.Text{String: "", Valid: false}
		syncRow.LastCursorID = pgtype.Text{String: "", Valid: false}
		if err := q.ResetUsagePollState(ctx, repo.ResetUsagePollStateParams{
			AiIntegrationConfigID: row.ID,
			Schedule:              provider,
			PollWatermarkAt:       syncRow.PollWatermarkAt,
			NextPollAfter:         syncRow.NextPollAfter,
		}); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to reset ai integration sync watermark")
		}
	}

	cfg, err := s.configFromRows(row, syncRow)
	if err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{
		Config:               cfg,
		Row:                  &row,
		CreatedNewGeneration: createdNewGeneration,
	}, nil
}

func providerRequiresExternalOrganizationID(provider string) bool {
	return provider == ProviderAnthropicCompliance || provider == ProviderCodexCompliance
}

func (s *Store) softDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string) error {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return err
	}
	if err := repo.New(dbtx).SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
		OrganizationID: orgID,
		Provider:       provider,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete ai integration config")
	}
	return nil
}

func (s *Store) ListEnabledConfigsByProvider(ctx context.Context, provider string) ([]Config, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.ListEnabledConfigsByProvider(ctx, provider)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration configs")
	}

	configs := make([]Config, 0, len(rows))
	for _, row := range rows {
		cfg, err := s.configFromListRow(row)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to decrypt ai integration config",
				attr.SlogError(err),
				attr.SlogOrganizationID(row.OrganizationID),
				attr.SlogProjectID(row.ProjectID.String()),
			)
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (s *Store) ListUsagePollCandidates(ctx context.Context, pollDueBefore time.Time, limit int32) ([]UsagePollCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}

	if err := s.ensureActiveSyncSchedules(ctx); err != nil {
		return nil, err
	}

	params := repo.ListUsagePollCandidatesParams{
		PollDueBefore: conv.ToPGTimestamptz(pollDueBefore),
		LimitCount:    limit,
	}

	rows, err := s.repo.ListUsagePollCandidates(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration usage poll candidates")
	}

	candidates := make([]UsagePollCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, UsagePollCandidate{
			SyncID:           row.SyncID,
			OrganizationID:   row.OrganizationID,
			OrganizationSlug: row.OrganizationSlug,
			Provider:         row.Provider,
			Schedule:         row.Schedule,
			Kind:             row.Kind,
		})
	}
	return candidates, nil
}

// ensureActiveSyncSchedules asserts that every active config (enabled, not
// deleted, holding an API key) has a sync row for each of its provider's
// schedules, creating missing ones due immediately. Inactive configs are
// deliberately never touched; this is a forward-looking process, not a backfill.
func (s *Store) ensureActiveSyncSchedules(ctx context.Context) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		for _, provider := range []string{ProviderCursor, ProviderAnthropicCompliance, ProviderCodexCompliance} {
			for _, sched := range syncSchedulesFor(provider) {
				// Same initial watermark policy as upsertWithTx: epoch for
				// time-kind schedules, now for cursor-kind ones.
				initialAt := time.Now().UTC()
				if sched.kind == SyncKindTime {
					initialAt = epochTime()
				}
				if err := q.EnsureProviderSyncSchedules(ctx, repo.EnsureProviderSyncSchedulesParams{
					Provider:        provider,
					Schedule:        sched.schedule,
					Kind:            sched.kind,
					PollWatermarkAt: conv.ToPGTimestamptz(initialAt),
					NextPollAfter:   conv.ToPGTimestamptz(initialAt),
				}); err != nil {
					return fmt.Errorf("ensure %s sync schedule: %w", sched.schedule, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to ensure ai integration sync schedules")
	}
	return nil
}

func (s *Store) ListSyncSchedules(ctx context.Context, configID uuid.UUID) ([]SyncSchedule, error) {
	rows, err := s.repo.ListSyncSchedules(ctx, configID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration sync schedules")
	}
	schedules := make([]SyncSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, SyncSchedule{
			ID:                  row.ID,
			Schedule:            row.Schedule,
			Kind:                row.Kind,
			NextPollAfter:       row.NextPollAfter.Time,
			LastPollError:       row.LastPollError.String,
			LastPollFailedAt:    row.LastPollFailedAt.Time,
			LastPollSuccessAt:   row.LastPollSuccessAt.Time,
			ConsecutiveFailures: row.ConsecutiveFailures,
			AutoPausedAt:        row.AutoPausedAt.Time,
			DisabledAt:          row.DisabledAt.Time,
		})
	}
	return schedules, nil
}

func syncScheduleFromModel(row repo.AiIntegrationSync) SyncSchedule {
	return SyncSchedule{
		ID:                  row.ID,
		Schedule:            row.Schedule,
		Kind:                row.Kind,
		NextPollAfter:       row.NextPollAfter.Time,
		LastPollError:       row.LastPollError.String,
		LastPollFailedAt:    row.LastPollFailedAt.Time,
		LastPollSuccessAt:   row.LastPollSuccessAt.Time,
		ConsecutiveFailures: row.ConsecutiveFailures,
		AutoPausedAt:        row.AutoPausedAt.Time,
		DisabledAt:          row.DisabledAt.Time,
	}
}

// setSyncScheduleDisabledWithTx records a user's explicit pause (or unpause)
// of one sync schedule. Runs in the caller's transaction so the audit entry
// commits atomically with the flag change.
func (s *Store) setSyncScheduleDisabledWithTx(ctx context.Context, dbtx repo.DBTX, configID uuid.UUID, schedule string, disabled bool) (SyncSchedule, error) {
	row, err := repo.New(dbtx).SetSyncScheduleDisabled(ctx, repo.SetSyncScheduleDisabledParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
		Disabled:              disabled,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return SyncSchedule{}, oops.E(oops.CodeNotFound, err, "ai integration sync schedule not found")
	case err != nil:
		return SyncSchedule{}, oops.E(oops.CodeUnexpected, err, "failed to update ai integration sync schedule")
	}
	return syncScheduleFromModel(row), nil
}

// retrySyncScheduleWithTx makes one schedule due immediately, lifting any
// automatic pause and resetting its failure streak. The scheduler picks it up
// on its next tick.
func (s *Store) retrySyncScheduleWithTx(ctx context.Context, dbtx repo.DBTX, configID uuid.UUID, schedule string) (SyncSchedule, error) {
	row, err := repo.New(dbtx).RetrySyncSchedule(ctx, repo.RetrySyncScheduleParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return SyncSchedule{}, oops.E(oops.CodeNotFound, err, "ai integration sync schedule not found")
	case err != nil:
		return SyncSchedule{}, oops.E(oops.CodeUnexpected, err, "failed to retry ai integration sync schedule")
	}
	return syncScheduleFromModel(row), nil
}

func (s *Store) GetUsagePollConfigBySyncID(ctx context.Context, syncID uuid.UUID) (Config, string, error) {
	row, err := s.repo.GetUsagePollConfigBySyncID(ctx, syncID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Config{}, "", oops.E(oops.CodeNotFound, err, "ai integration usage poll sync not found")
	case err != nil:
		return Config{}, "", oops.E(oops.CodeUnexpected, err, "failed to load ai integration usage poll sync")
	}
	cfg, err := s.configFromUsagePollConfigBySyncIDRow(row)
	if err != nil {
		return Config{}, "", err
	}
	if row.Kind == SyncKindTime {
		normalizeNeverSyncedTimeCheckpoint(&cfg)
	}
	return cfg, row.Schedule, nil
}

func (s *Store) GetProviderUsagePollConfig(ctx context.Context, configID uuid.UUID) (Config, string, error) {
	row, err := s.repo.GetProviderUsagePollConfigByID(ctx, configID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Config{}, "", oops.E(oops.CodeNotFound, err, "ai integration usage poll config not found")
	case err != nil:
		return Config{}, "", oops.E(oops.CodeUnexpected, err, "failed to load ai integration usage poll config")
	}
	cfg, err := s.configFromProviderUsagePollConfigByIDRow(row)
	if err != nil {
		return Config{}, "", err
	}
	if row.Kind == SyncKindTime {
		normalizeNeverSyncedTimeCheckpoint(&cfg)
	}
	return cfg, row.Schedule, nil
}

func (s *Store) GetUsagePollConfig(ctx context.Context, configID uuid.UUID, schedule string) (Config, error) {
	row, err := s.repo.GetUsagePollConfigByID(ctx, repo.GetUsagePollConfigByIDParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Config{}, oops.E(oops.CodeNotFound, err, "ai integration usage poll config not found")
	case err != nil:
		return Config{}, oops.E(oops.CodeUnexpected, err, "failed to load ai integration usage poll config")
	}
	cfg, err := s.configFromUsagePollConfigRow(row)
	if err != nil {
		return Config{}, err
	}
	if schedule != ScheduleAnthropicCompliance {
		normalizeNeverSyncedTimeCheckpoint(&cfg)
	}
	return cfg, nil
}

func (s *Store) RecordUsagePollSuccess(ctx context.Context, syncID uuid.UUID, schedule string, t time.Time, lastCursor string) error {
	if err := s.repo.RecordUsagePollSuccess(ctx, repo.RecordUsagePollSuccessParams{
		SyncID:          syncID,
		PollWatermarkAt: conv.ToPGTimestamptz(t),
		NextPollAfter:   conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(schedule))),
		LastCursorID:    conv.ToPGTextEmpty(lastCursor),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration usage poll success")
	}
	return nil
}

// SyncScheduleState is the scheduler state for one sync schedule row.
// WatermarkAt is the exclusive end of the last fully ingested range; its zero
// value means the schedule never completed a window.
type SyncScheduleState struct {
	SyncID              uuid.UUID
	ConfigID            uuid.UUID
	Schedule            string
	WatermarkAt         time.Time
	Checkpoint          timewindowpoller.PollCheckpoint
	NextPollAfter       time.Time
	LastPollError       string
	ConsecutiveFailures int32
}

// EnsureTimeSyncSchedule returns a time-kind schedule's state for a config,
// creating the row (due immediately, no watermark) on first use. It exists
// alongside the eager creation in upsertWithTx so configs that predate a
// schedule pick it up on their next poll.
func (s *Store) EnsureTimeSyncSchedule(ctx context.Context, configID uuid.UUID, schedule string) (SyncScheduleState, error) {
	epoch := epochTime()
	row, err := s.repo.EnsureSync(ctx, repo.EnsureSyncParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
		Kind:                  SyncKindTime,
		PollWatermarkAt:       conv.ToPGTimestamptz(epoch),
		NextPollAfter:         conv.ToPGTimestamptz(epoch),
	})
	if err != nil {
		return SyncScheduleState{}, oops.E(oops.CodeUnexpected, err, "failed to load ai integration sync schedule")
	}

	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return SyncScheduleState{}, oops.E(oops.CodeUnexpected, err, "failed to decode ai integration sync checkpoint")
	}
	// The epoch watermark is the never-synced sentinel for legacy rows; surface
	// it as zero so callers keep a single "no watermark yet" check.
	if !checkpoint.Partial() && !checkpoint.Watermark.After(epoch) {
		checkpoint.Watermark = time.Time{}
	}
	return SyncScheduleState{
		SyncID:              row.ID,
		ConfigID:            row.AiIntegrationConfigID,
		Schedule:            row.Schedule,
		WatermarkAt:         checkpoint.Watermark,
		Checkpoint:          checkpoint,
		NextPollAfter:       row.NextPollAfter.Time,
		LastPollError:       row.LastPollError.String,
		ConsecutiveFailures: row.ConsecutiveFailures,
	}, nil
}

// AdvanceWatermark durably records a schedule checkpoint. Completed checkpoints
// move the time watermark forward; partial checkpoints keep the last completed
// watermark and store the provider's next page token.
func (s *Store) AdvanceWatermark(ctx context.Context, syncID uuid.UUID, checkpoint timewindowpoller.PollCheckpoint) error {
	encoded, err := checkpoint.MarshalText()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to encode ai integration sync checkpoint")
	}
	shadowWatermark := checkpoint.Watermark
	if shadowWatermark.IsZero() {
		shadowWatermark = epochTime()
	}
	if err := s.repo.AdvanceWatermark(ctx, repo.AdvanceWatermarkParams{
		PollWatermarkAt: conv.ToPGTimestamptz(shadowWatermark),
		PollCheckpoint:  conv.ToPGText(string(encoded)),
		SyncID:          syncID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to advance ai integration sync schedule watermark")
	}
	return nil
}

// RecordSchedulePollSuccess reschedules a sync and clears failure state
// without touching the watermark, which advances incrementally mid-sync.
func (s *Store) RecordSchedulePollSuccess(ctx context.Context, configID uuid.UUID, schedule string, t time.Time) error {
	if err := s.repo.RecordPollSuccessKeepWatermark(ctx, repo.RecordPollSuccessKeepWatermarkParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
		NextPollAfter:         conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(schedule))),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration sync schedule poll success")
	}
	return nil
}

// AutoPauseAfterRejectedPolls is the consecutive-failure count at which a
// schedule whose provider keeps rejecting the configuration (revoked api key,
// missing api access) is automatically paused. Rejections are permanent until
// the user fixes the integration, so a small threshold is enough; saving the
// integration clears the pause and the failure streak.
const AutoPauseAfterRejectedPolls = 3

// RecordSchedulePollFailure records a final poll failure on a schedule. A
// positive pauseAfter automatically pauses the schedule once its consecutive
// failure count reaches that threshold; zero never pauses, for failures that
// retrying at the normal cadence can plausibly fix.
func (s *Store) RecordSchedulePollFailure(ctx context.Context, configID uuid.UUID, schedule string, t time.Time, cause error, pauseAfter int32) error {
	var errStr string
	if cause != nil {
		errStr = cause.Error()
		var shareable *oops.ShareableError
		if errors.As(cause, &shareable) {
			errStr = shareable.String()
		}
	}

	if err := s.repo.RecordUsagePollFailure(ctx, repo.RecordUsagePollFailureParams{
		AiIntegrationConfigID: configID,
		Schedule:              schedule,
		NextPollAfter:         conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(schedule))),
		LastPollError:         conv.ToPGTextEmpty(conv.TruncateString(errStr, maxUsagePollErrorMessage)),
		PauseAfter:            pauseAfter,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration sync schedule poll failure")
	}
	return nil
}

func (s *Store) RecordUsagePollFailure(ctx context.Context, configID uuid.UUID, provider string, t time.Time, cause error) error {
	return s.RecordSchedulePollFailure(ctx, configID, provider, t, cause, 0)
}

// epochTime is the never-synced watermark sentinel for time-kind schedules
// and the "due immediately" next_poll_after for newly created ones.
func epochTime() time.Time {
	return time.Unix(0, 0).UTC()
}

func normalizeNeverSyncedTimeCheckpoint(cfg *Config) {
	if cfg.PollCheckpoint.Partial() || cfg.PollCheckpoint.Watermark.After(epochTime()) {
		return
	}
	cfg.PollCheckpoint.Watermark = time.Time{}
	cfg.PollWatermarkAt = time.Time{}
}

func decodeRowPollCheckpoint(encoded pgtype.Text, legacy pgtype.Timestamptz) (timewindowpoller.PollCheckpoint, error) {
	legacyWatermark := time.Time{}
	if legacy.Valid {
		legacyWatermark = legacy.Time
	}
	if !encoded.Valid {
		checkpoint, err := timewindowpoller.DecodeCheckpoint("", legacyWatermark)
		if err != nil {
			return timewindowpoller.CompletedCheckpoint(time.Time{}), fmt.Errorf("decode legacy poll checkpoint: %w", err)
		}
		return checkpoint, nil
	}
	checkpoint, err := timewindowpoller.DecodeCheckpoint(encoded.String, legacyWatermark)
	if err != nil {
		return timewindowpoller.CompletedCheckpoint(time.Time{}), fmt.Errorf("decode poll checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (s *Store) configFromGetRow(row repo.GetConfigByOrgAndProviderRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 row.SyncID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromListRow(row repo.ListEnabledConfigsByProviderRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 row.SyncID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromUsagePollConfigRow(row repo.GetUsagePollConfigByIDRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 row.SyncID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromUsagePollConfigBySyncIDRow(row repo.GetUsagePollConfigBySyncIDRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 row.SyncID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromProviderUsagePollConfigByIDRow(row repo.GetProviderUsagePollConfigByIDRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(row.PollCheckpoint, row.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 row.SyncID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromRows(row repo.AiIntegrationConfig, syncRow repo.EnsureSyncRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	checkpoint, err := decodeRowPollCheckpoint(syncRow.PollCheckpoint, syncRow.PollWatermarkAt)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		SyncID:                 syncRow.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        checkpoint.Watermark,
		PollCheckpoint:         checkpoint,
		NextPollAfter:          syncRow.NextPollAfter.Time,
		LastPollError:          syncRow.LastPollError.String,
		LastPollFailedAt:       syncRow.LastPollFailedAt.Time,
		LastPollSuccessAt:      syncRow.LastPollSuccessAt.Time,
		ConsecutiveFailures:    syncRow.ConsecutiveFailures,
		LastCursor:             syncRow.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) encryptAPIKey(apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", oops.E(oops.CodeInvalid, nil, "api_key is required")
	}
	ct, err := s.enc.Encrypt([]byte(apiKey))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "encrypt ai integration api key")
	}
	return ct, nil
}

func (s *Store) decryptAPIKey(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	plaintext, err := s.enc.Decrypt(stored)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "decrypt ai integration api key")
	}
	return plaintext, nil
}

func emptyConfig(orgID string, provider string) Config {
	return Config{
		ID:                     uuid.Nil,
		SyncID:                 uuid.Nil,
		OrganizationID:         orgID,
		Provider:               provider,
		ProjectID:              uuid.Nil,
		ExternalOrganizationID: nil,
		BillingMode:            "",
		APIKey:                 "",
		Enabled:                false,
		PollWatermarkAt:        time.Time{},
		PollCheckpoint:         timewindowpoller.CompletedCheckpoint(time.Time{}),
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}
}
