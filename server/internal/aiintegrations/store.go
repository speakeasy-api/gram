package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	ProviderCursor              = "cursor"
	ProviderAnthropicCompliance = "anthropic_compliance"
)

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
)

// Sync kinds record how a schedule checkpoints progress.
const (
	// SyncKindCursor schedules resume from an opaque pagination token
	// (last_cursor_id).
	SyncKindCursor = "cursor"
	// SyncKindTime schedules resume from poll_watermark_at.
	SyncKindTime = "time"
)

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
	default:
		return []syncSchedule{providerSyncSchedule(provider)}
	}
}

const (
	initialUsagePollLookback             = time.Hour * 24
	cursorUsagePollInterval              = time.Hour
	anthropicComplianceUsagePollInterval = 5 * time.Minute
	maxUsagePollErrorMessage             = 4000

	// anthropicAnalyticsPollInterval is the delay between Admin Analytics API
	// polls. The analytics export is refreshed roughly every 4 hours, so
	// polling more often only re-reads the same watermark.
	anthropicAnalyticsPollInterval = 4 * time.Hour
	// anthropicAnalyticsInitialLookback bounds the first analytics ingest for
	// a config that has never synced.
	anthropicAnalyticsInitialLookback = 24 * time.Hour
)

// pollIntervalForSchedule returns the delay between runs of one independent
// sync schedule. Unknown schedules fall back to the Cursor interval.
func pollIntervalForSchedule(schedule string) time.Duration {
	switch schedule {
	case ScheduleAnthropicCompliance:
		return anthropicComplianceUsagePollInterval
	case ScheduleAnthropicAnalyticsUsage, ScheduleAnthropicAnalyticsCost:
		return anthropicAnalyticsPollInterval
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
	OrganizationID         string
	Provider               string
	ProjectID              uuid.UUID
	ExternalOrganizationID *string
	BillingMode            string
	APIKey                 string
	Enabled                bool
	PollWatermarkAt        time.Time
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
	ID               uuid.UUID
	OrganizationID   string
	OrganizationSlug string
	Provider         string
	Schedule         string
	Kind             string
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
	case ProviderCursor, ProviderAnthropicCompliance:
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

	if provider == ProviderAnthropicCompliance && externalOrganizationID == nil {
		return UpsertResult{}, oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
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

	// A pre-schedule sync row (schedule IS NULL) must be adopted as the
	// provider-named schedule before EnsureSync runs: NULLs never conflict
	// in the (config_id, schedule) unique index, so an unlabeled row would
	// otherwise gain a duplicate provider-named sibling.
	providerSched := providerSyncSchedule(provider)
	if err := q.AdoptLegacySyncSchedule(ctx, repo.AdoptLegacySyncScheduleParams{
		AiIntegrationConfigID: row.ID,
		Schedule:              conv.ToPGText(providerSched.schedule),
		Kind:                  conv.ToPGText(providerSched.kind),
	}); err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to adopt ai integration sync row")
	}

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
			Schedule:              conv.ToPGText(sched.schedule),
			Kind:                  conv.ToPGText(sched.kind),
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
	if resetPollWatermarkAt != nil {
		syncRow.PollWatermarkAt = conv.ToPGTimestamptz(*resetPollWatermarkAt)
		syncRow.NextPollAfter = conv.ToPGTimestamptz(resetPollWatermarkAt.UTC().Add(pollIntervalForSchedule(provider)))
		syncRow.LastPollError = pgtype.Text{String: "", Valid: false}
		syncRow.LastPollFailedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.LastPollSuccessAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.ConsecutiveFailures = 0
		syncRow.LastCursorID = pgtype.Text{String: "", Valid: false}
		if err := q.ResetUsagePollState(ctx, repo.ResetUsagePollStateParams{
			AiIntegrationConfigID: row.ID,
			Schedule:              conv.ToPGText(provider),
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
		schedule := row.Schedule.String
		kind := row.Kind.String
		if !row.Schedule.Valid {
			// Active rows were adopted by ensureActiveSyncSchedules above;
			// this only covers a row inserted concurrently by pre-schedule
			// code between the ensure step and the listing.
			fallback := providerSyncSchedule(row.Provider)
			schedule, kind = fallback.schedule, fallback.kind
		}
		candidates = append(candidates, UsagePollCandidate{
			ID:               row.ID,
			OrganizationID:   row.OrganizationID,
			OrganizationSlug: row.OrganizationSlug,
			Provider:         row.Provider,
			Schedule:         schedule,
			Kind:             kind,
		})
	}
	return candidates, nil
}

// ensureActiveSyncSchedules asserts that every active config (enabled, not
// deleted, holding an API key) has a sync row for each of its provider's
// schedules, creating missing ones due immediately. Legacy rows written
// before the schedule columns existed are adopted as their provider-named
// schedule first. Inactive configs are deliberately never touched; this is a
// forward-looking process, not a backfill.
func (s *Store) ensureActiveSyncSchedules(ctx context.Context) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		for _, provider := range []string{ProviderCursor, ProviderAnthropicCompliance} {
			if err := q.AdoptLegacySyncSchedulesForProvider(ctx, repo.AdoptLegacySyncSchedulesForProviderParams{
				Provider: provider,
				Kind:     conv.ToPGText(providerSyncSchedule(provider).kind),
			}); err != nil {
				return fmt.Errorf("adopt legacy %s sync rows: %w", provider, err)
			}
			for _, sched := range syncSchedulesFor(provider) {
				// Same initial watermark policy as upsertWithTx: epoch for
				// time-kind schedules, now for cursor-kind ones.
				initialAt := time.Now().UTC()
				if sched.kind == SyncKindTime {
					initialAt = epochTime()
				}
				if err := q.EnsureProviderSyncSchedules(ctx, repo.EnsureProviderSyncSchedulesParams{
					Provider:        provider,
					Schedule:        conv.ToPGText(sched.schedule),
					Kind:            conv.ToPGText(sched.kind),
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

func (s *Store) GetUsagePollConfig(ctx context.Context, configID uuid.UUID, schedule string) (Config, error) {
	row, err := s.repo.GetUsagePollConfigByID(ctx, repo.GetUsagePollConfigByIDParams{
		AiIntegrationConfigID: configID,
		Schedule:              conv.ToPGText(schedule),
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
	if schedule != ScheduleAnthropicCompliance && !cfg.PollWatermarkAt.After(epochTime()) {
		cfg.PollWatermarkAt = time.Time{}
	}
	return cfg, nil
}

func (s *Store) RecordUsagePollSuccess(ctx context.Context, configID uuid.UUID, provider string, t time.Time, lastCursor string) error {
	if err := s.repo.RecordUsagePollSuccess(ctx, repo.RecordUsagePollSuccessParams{
		AiIntegrationConfigID: configID,
		Schedule:              conv.ToPGText(provider),
		PollWatermarkAt:       conv.ToPGTimestamptz(t),
		NextPollAfter:         conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(provider))),
		LastCursorID:          conv.ToPGTextEmpty(lastCursor),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration usage poll success")
	}
	return nil
}

// SyncScheduleState is the scheduler state for one sync schedule row.
// WatermarkAt is the exclusive end of the last ingested range; its zero value
// means the schedule never synced.
type SyncScheduleState struct {
	ConfigID            uuid.UUID
	Schedule            string
	WatermarkAt         time.Time
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
		Schedule:              conv.ToPGText(schedule),
		Kind:                  conv.ToPGText(SyncKindTime),
		PollWatermarkAt:       conv.ToPGTimestamptz(epoch),
		NextPollAfter:         conv.ToPGTimestamptz(epoch),
	})
	if err != nil {
		return SyncScheduleState{}, oops.E(oops.CodeUnexpected, err, "failed to load ai integration sync schedule")
	}

	// The epoch watermark is the never-synced sentinel; surface it as the
	// zero time so callers keep a single "no watermark yet" check.
	watermark := row.PollWatermarkAt.Time
	if !watermark.After(epoch) {
		watermark = time.Time{}
	}
	return SyncScheduleState{
		ConfigID:            row.AiIntegrationConfigID,
		Schedule:            row.Schedule.String,
		WatermarkAt:         watermark,
		NextPollAfter:       row.NextPollAfter.Time,
		LastPollError:       row.LastPollError.String,
		ConsecutiveFailures: row.ConsecutiveFailures,
	}, nil
}

// AdvanceSchedulePollWatermark durably records that data up to (but not
// including) watermark has been ingested for a schedule. It is called after
// each ingested window so a mid-sync crash re-fetches at most one window.
func (s *Store) AdvanceSchedulePollWatermark(ctx context.Context, configID uuid.UUID, schedule string, watermark time.Time) error {
	if err := s.repo.AdvancePollWatermark(ctx, repo.AdvancePollWatermarkParams{
		AiIntegrationConfigID: configID,
		Schedule:              conv.ToPGText(schedule),
		PollWatermarkAt:       conv.ToPGTimestamptz(watermark),
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
		Schedule:              conv.ToPGText(schedule),
		NextPollAfter:         conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(schedule))),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration sync schedule poll success")
	}
	return nil
}

func (s *Store) RecordSchedulePollFailure(ctx context.Context, configID uuid.UUID, schedule string, t time.Time, cause error) error {
	var errStr string
	if cause != nil {
		errStr = cause.Error()
	}

	if err := s.repo.RecordUsagePollFailure(ctx, repo.RecordUsagePollFailureParams{
		AiIntegrationConfigID: configID,
		Schedule:              conv.ToPGText(schedule),
		NextPollAfter:         conv.ToPGTimestamptz(t.UTC().Add(pollIntervalForSchedule(schedule))),
		LastPollError:         conv.ToPGTextEmpty(conv.TruncateString(errStr, maxUsagePollErrorMessage)),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration sync schedule poll failure")
	}
	return nil
}

func (s *Store) RecordUsagePollFailure(ctx context.Context, configID uuid.UUID, provider string, t time.Time, cause error) error {
	return s.RecordSchedulePollFailure(ctx, configID, provider, t, cause)
}

// epochTime is the never-synced watermark sentinel for time-kind schedules
// and the "due immediately" next_poll_after for newly created ones.
func epochTime() time.Time {
	return time.Unix(0, 0).UTC()
}

func (s *Store) configFromGetRow(row repo.GetConfigByOrgAndProviderRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
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
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
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
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
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
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: conv.FromPGText[string](row.ExternalOrganizationID),
		BillingMode:            conv.FromPGTextOrEmpty[string](row.BillingMode),
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        syncRow.PollWatermarkAt.Time,
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
		OrganizationID:         orgID,
		Provider:               provider,
		ProjectID:              uuid.Nil,
		ExternalOrganizationID: nil,
		BillingMode:            "",
		APIKey:                 "",
		Enabled:                false,
		PollWatermarkAt:        time.Time{},
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
