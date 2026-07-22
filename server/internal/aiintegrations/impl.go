package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	srv "github.com/speakeasy-api/gram/server/gen/http/ai_integrations/server"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	auth         *auth.Auth
	authz        *authz.Engine
	audit        *audit.Logger
	store        *Store
	configPoller ConfigPoller
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

type ConfigPoller interface {
	Poll(ctx context.Context, organizationSlug string, syncID uuid.UUID) error
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	encryptionClient *encryption.Client,
	configPoller ConfigPoller,
) *Service {
	logger = logger.With(attr.SlogComponent("aiintegrations.api"))
	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/aiintegrations"),
		logger:       logger,
		db:           db,
		auth:         auth.New(logger, db, sessions, authzEngine),
		authz:        authzEngine,
		audit:        auditLogger,
		store:        NewStore(logger, db, encryptionClient),
		configPoller: configPoller,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) GetConfig(ctx context.Context, payload *gen.GetConfigPayload) (*gen.AIIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, err := normalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	cfg, row, err := s.store.loadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return emptyView(authCtx.ActiveOrganizationID, provider), nil
	}
	return buildView(cfg, row.ID), nil
}

func (s *Service) UpsertConfig(ctx context.Context, payload *gen.UpsertConfigPayload) (*gen.AIIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, err := normalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	before, beforeRow, err := s.store.loadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(payload.APIKey)
	apiKeySupplied := apiKey != ""
	if apiKey == "" {
		if beforeRow == nil || before.APIKey == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "api_key is required")
		}
		apiKey = before.APIKey
	}
	externalOrganizationID := payload.ExternalOrganizationID
	if externalOrganizationID != nil {
		trimmed := strings.TrimSpace(*externalOrganizationID)
		externalOrganizationID = conv.PtrEmpty(trimmed)
	}
	if payload.ExternalOrganizationID == nil && beforeRow != nil {
		externalOrganizationID = before.ExternalOrganizationID
	}
	externalOrgChanged := beforeRow != nil &&
		conv.PtrValOr(externalOrganizationID, "") != conv.PtrValOr(before.ExternalOrganizationID, "")

	// billing_mode is admin-declared and independent of the API key. Omit it to
	// preserve the existing value (settings-only updates and key rotations must
	// not wipe a declared mode).
	billingMode := payload.BillingMode
	if billingMode != nil {
		trimmed := strings.TrimSpace(*billingMode)
		billingMode = conv.PtrEmpty(trimmed)
	}
	if payload.BillingMode == nil && beforeRow != nil {
		billingMode = conv.PtrEmpty(before.BillingMode)
	}

	// Start the watermark one lookback period in the past so the first poll
	// backfills usage emitted just before the key was configured.
	watermark := time.Now().UTC().Add(-initialPollLookbackForProvider(provider))
	resetPollWatermarkAt := conv.Ternary(
		beforeRow != nil && (apiKeySupplied || externalOrgChanged),
		&watermark,
		nil,
	)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	result, err := s.store.upsertWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, provider, apiKey, apiKeySupplied, payload.Enabled, externalOrganizationID, billingMode, resetPollWatermarkAt)
	if err != nil {
		return nil, err
	}
	cfg := result.Config
	row := result.Row

	var beforeSnap *audit.AIIntegrationSnapshot
	if beforeRow != nil {
		snap := snapshotFromConfig(before)
		beforeSnap = &snap
	}
	afterSnap := snapshotFromConfig(cfg)

	if err := s.audit.LogAIIntegrationUpsert(ctx, dbtx, audit.LogAIIntegrationUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
		SnapshotBefore:   beforeSnap,
		SnapshotAfter:    &afterSnap,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai integration upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai integration upsert").LogError(ctx, logger)
	}

	if result.CreatedNewGeneration && cfg.Enabled {
		if err := s.startUsagePoll(ctx, authCtx.OrganizationSlug, cfg.ID, cfg.Provider); err != nil {
			logger.WarnContext(ctx, "failed to start ai integration usage poll workflow",
				attr.SlogError(err),
				attr.SlogAIIntegrationConfigID(cfg.ID.String()),
			)
		}
	}

	return buildView(cfg, row.ID), nil
}

func (s *Service) DeleteConfig(ctx context.Context, payload *gen.DeleteConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	provider, err := normalizeProvider(payload.Provider)
	if err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	cfg, row, err := s.store.loadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.store.softDeleteWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, provider); err != nil {
		return err
	}

	if err := s.audit.LogAIIntegrationDelete(ctx, dbtx, audit.LogAIIntegrationDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log ai integration delete").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit ai integration delete").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) ListSchedules(ctx context.Context, payload *gen.ListSchedulesPayload) (*gen.ListAIIntegrationSchedulesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, err := normalizeProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	_, row, err := s.store.loadForOrgAndProviderRow(ctx, authCtx.ActiveOrganizationID, provider)
	if err != nil {
		return nil, err
	}
	result := &gen.ListAIIntegrationSchedulesResult{
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       provider,
		Schedules:      []*gen.AIIntegrationScheduleState{},
	}
	if row == nil {
		return result, nil
	}

	schedules, err := s.store.ListSyncSchedules(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	// Present schedules in registry order so the dashboard renders a stable,
	// meaningful sequence; unknown (e.g. legacy) schedules follow.
	byName := make(map[string]SyncSchedule, len(schedules))
	for _, schedule := range schedules {
		byName[schedule.Schedule] = schedule
	}
	for _, sched := range syncSchedulesFor(provider) {
		if schedule, ok := byName[sched.schedule]; ok {
			result.Schedules = append(result.Schedules, scheduleView(schedule))
			delete(byName, sched.schedule)
		}
	}
	for _, schedule := range schedules {
		if _, ok := byName[schedule.Schedule]; ok {
			result.Schedules = append(result.Schedules, scheduleView(schedule))
		}
	}
	return result, nil
}

func (s *Service) SetScheduleEnabled(ctx context.Context, payload *gen.SetScheduleEnabledPayload) (*gen.AIIntegrationScheduleState, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, schedule, cfg, row, err := s.resolveScheduleTarget(ctx, authCtx.ActiveOrganizationID, payload.Provider, payload.Schedule)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := s.store.setSyncScheduleDisabledWithTx(ctx, dbtx, row.ID, schedule, !payload.Enabled)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAIIntegrationUpdateSchedule(ctx, dbtx, audit.LogAIIntegrationUpdateScheduleEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
		Provider:         provider,
		Schedule:         schedule,
		Enabled:          payload.Enabled,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai integration schedule update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai integration schedule update").LogError(ctx, logger)
	}

	return scheduleView(updated), nil
}

func (s *Service) RetrySchedule(ctx context.Context, payload *gen.RetrySchedulePayload) (*gen.AIIntegrationScheduleState, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	provider, schedule, cfg, row, err := s.resolveScheduleTarget(ctx, authCtx.ActiveOrganizationID, payload.Provider, payload.Schedule)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := s.store.retrySyncScheduleWithTx(ctx, dbtx, row.ID, schedule)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAIIntegrationRetrySchedule(ctx, dbtx, audit.LogAIIntegrationRetryScheduleEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        cfg.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewAIIntegrationConfig(row.ID),
		Provider:         provider,
		Schedule:         schedule,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai integration schedule retry").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai integration schedule retry").LogError(ctx, logger)
	}

	return scheduleView(updated), nil
}

// resolveScheduleTarget validates a provider/schedule pair and loads the
// org's config for it. Both schedule mutations share this preamble.
func (s *Service) resolveScheduleTarget(ctx context.Context, orgID string, rawProvider string, rawSchedule string) (string, string, Config, *repo.GetConfigByOrgAndProviderRow, error) {
	provider, err := normalizeProvider(rawProvider)
	if err != nil {
		return "", "", Config{}, nil, err
	}

	schedule := strings.ToLower(strings.TrimSpace(rawSchedule))
	if !scheduleBelongsToProvider(provider, schedule) {
		return "", "", Config{}, nil, oops.E(oops.CodeInvalid, nil, "unknown schedule %q for provider %s", rawSchedule, provider)
	}

	cfg, row, err := s.store.loadForOrgAndProviderRow(ctx, orgID, provider)
	if err != nil {
		return "", "", Config{}, nil, err
	}
	if row == nil {
		return "", "", Config{}, nil, oops.E(oops.CodeNotFound, nil, "no ai integration config for provider %s", provider)
	}
	return provider, schedule, cfg, row, nil
}

func scheduleBelongsToProvider(provider string, schedule string) bool {
	for _, sched := range syncSchedulesFor(provider) {
		if sched.schedule == schedule {
			return true
		}
	}
	return false
}

func scheduleView(schedule SyncSchedule) *gen.AIIntegrationScheduleState {
	var lastPollSuccessAt *string
	if !schedule.LastPollSuccessAt.IsZero() {
		formatted := schedule.LastPollSuccessAt.Format(time.RFC3339)
		lastPollSuccessAt = &formatted
	}
	var lastPollFailedAt *string
	if !schedule.LastPollFailedAt.IsZero() {
		formatted := schedule.LastPollFailedAt.Format(time.RFC3339)
		lastPollFailedAt = &formatted
	}
	var lastPollError *string
	if schedule.LastPollError != "" {
		lastPollError = &schedule.LastPollError
	}
	var nextPollAfter *string
	if !schedule.NextPollAfter.IsZero() {
		formatted := schedule.NextPollAfter.Format(time.RFC3339)
		nextPollAfter = &formatted
	}
	var autoPausedAt *string
	if !schedule.AutoPausedAt.IsZero() {
		formatted := schedule.AutoPausedAt.Format(time.RFC3339)
		autoPausedAt = &formatted
	}
	stream := streamForSchedule(schedule.Schedule)
	return &gen.AIIntegrationScheduleState{
		Schedule:            schedule.Schedule,
		Stream:              conv.PtrEmpty(stream.name),
		StreamKind:          conv.PtrEmpty(stream.kind),
		Enabled:             schedule.DisabledAt.IsZero(),
		Status:              deriveScheduleStatus(schedule),
		LastPollSuccessAt:   lastPollSuccessAt,
		LastPollFailedAt:    lastPollFailedAt,
		LastPollError:       lastPollError,
		NextPollAfter:       nextPollAfter,
		ConsecutiveFailures: int(schedule.ConsecutiveFailures),
		AutoPausedAt:        autoPausedAt,
	}
}

func deriveScheduleStatus(schedule SyncSchedule) string {
	switch {
	case !schedule.DisabledAt.IsZero():
		return "disabled"
	case !schedule.AutoPausedAt.IsZero():
		return "auto_paused"
	case schedule.LastPollError != "" || !schedule.LastPollFailedAt.IsZero():
		return "failed"
	case !schedule.LastPollSuccessAt.IsZero():
		return "success"
	default:
		return "pending"
	}
}

func snapshotFromConfig(cfg Config) audit.AIIntegrationSnapshot {
	return audit.AIIntegrationSnapshot{
		Provider:    cfg.Provider,
		ProjectID:   cfg.ProjectID,
		Enabled:     cfg.Enabled,
		HasAPIKey:   cfg.APIKey != "",
		BillingMode: cfg.BillingMode,
	}
}

func (s *Service) startUsagePoll(ctx context.Context, organizationSlug string, configID uuid.UUID, provider string) error {
	if s.configPoller == nil {
		return nil
	}

	schedules, err := s.store.ListSyncSchedules(ctx, configID)
	if err != nil {
		return err
	}
	syncIDsBySchedule := make(map[string]uuid.UUID, len(schedules))
	disabledBySchedule := make(map[string]bool, len(schedules))
	for _, syncSchedule := range schedules {
		syncIDsBySchedule[syncSchedule.Schedule] = syncSchedule.ID
		disabledBySchedule[syncSchedule.Schedule] = !syncSchedule.DisabledAt.IsZero()
	}

	var startErr error
	for _, syncSchedule := range syncSchedulesFor(provider) {
		if disabledBySchedule[syncSchedule.schedule] {
			// The user explicitly paused this schedule; a config save must not
			// restart it.
			continue
		}
		syncID, ok := syncIDsBySchedule[syncSchedule.schedule]
		if !ok {
			startErr = errors.Join(startErr, fmt.Errorf("missing %s sync schedule", syncSchedule.schedule))
			continue
		}
		if err := s.configPoller.Poll(ctx, organizationSlug, syncID); err != nil {
			startErr = errors.Join(startErr, fmt.Errorf("start %s schedule: %w", syncSchedule.schedule, err))
		}
	}
	if startErr != nil {
		return oops.E(oops.CodeUnexpected, startErr, "start ai integration sync schedules")
	}
	return nil
}

func emptyView(orgID, provider string) *gen.AIIntegrationConfig {
	return &gen.AIIntegrationConfig{
		ID:                     nil,
		OrganizationID:         orgID,
		Provider:               provider,
		ProjectID:              nil,
		ExternalOrganizationID: nil,
		BillingMode:            nil,
		Enabled:                false,
		HasAPIKey:              false,
		LastPolledAt:           nil,
		LastPollStatus:         nil,
		LastPollError:          nil,
		LastPollFailedAt:       nil,
		NextPollAfter:          nil,
		CreatedAt:              nil,
		UpdatedAt:              nil,
	}
}

func buildView(cfg Config, idValue uuid.UUID) *gen.AIIntegrationConfig {
	id := idValue.String()
	projectID := cfg.ProjectID.String()
	createdAt := cfg.CreatedAt.Format(time.RFC3339)
	updatedAt := cfg.UpdatedAt.Format(time.RFC3339)
	var lastPolledAt *string
	if !cfg.LastPollSuccessAt.IsZero() {
		formatted := cfg.LastPollSuccessAt.Format(time.RFC3339)
		lastPolledAt = &formatted
	}
	lastPollStatus := deriveLastPollStatus(cfg)
	var lastPollError *string
	if cfg.LastPollError != "" {
		lastPollError = &cfg.LastPollError
	}
	var lastPollFailedAt *string
	if !cfg.LastPollFailedAt.IsZero() {
		formatted := cfg.LastPollFailedAt.Format(time.RFC3339)
		lastPollFailedAt = &formatted
	}
	var nextPollAfter *string
	if !cfg.NextPollAfter.IsZero() {
		formatted := cfg.NextPollAfter.Format(time.RFC3339)
		nextPollAfter = &formatted
	}
	return &gen.AIIntegrationConfig{
		ID:                     &id,
		OrganizationID:         cfg.OrganizationID,
		Provider:               cfg.Provider,
		ProjectID:              &projectID,
		ExternalOrganizationID: conv.PtrValOrEmpty(&cfg.ExternalOrganizationID, nil),
		BillingMode:            conv.PtrEmpty(cfg.BillingMode),
		Enabled:                cfg.Enabled,
		HasAPIKey:              cfg.APIKey != "",
		LastPolledAt:           lastPolledAt,
		LastPollStatus:         &lastPollStatus,
		LastPollError:          lastPollError,
		LastPollFailedAt:       lastPollFailedAt,
		NextPollAfter:          nextPollAfter,
		CreatedAt:              &createdAt,
		UpdatedAt:              &updatedAt,
	}
}

func deriveLastPollStatus(cfg Config) string {
	if cfg.LastPollError != "" || !cfg.LastPollFailedAt.IsZero() {
		return "failed"
	}
	if !cfg.LastPollSuccessAt.IsZero() {
		return "success"
	}
	return "pending"
}
