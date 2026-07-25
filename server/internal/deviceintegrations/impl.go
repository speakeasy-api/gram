// Package deviceintegrations implements the management API for org-level
// device integrations: connecting MDM inventory sources and compliance
// evidence sinks, managing their sync schedules, and reading agent coverage
// across the synced fleet.
package deviceintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	srv "github.com/speakeasy-api/gram/server/gen/http/device_integrations/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// testConnectionTimeout bounds the synchronous connection probe so a
	// hostile or broken target cannot hang the management API (same budget as
	// remotemcp's URL verification).
	testConnectionTimeout = 10 * time.Second

	// activeWindow is the freshness window for the coverage buckets: an
	// assigned user counts as agent-active when their heartbeat is within
	// it. The agent polls every 60s, so an hour is generous — anything
	// staler is a stopped or disabled agent, not a slow one.
	activeWindow = time.Hour
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	audit    *audit.Logger
	store    *Store
	repo     *repo.Queries
	guardian *guardian.Policy
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	encryptionClient *encryption.Client,
	guardianPolicy *guardian.Policy,
) *Service {
	logger = logger.With(attr.SlogComponent("deviceintegrations.api"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/deviceintegrations"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		audit:    auditLogger,
		store:    NewStore(logger, db, encryptionClient),
		repo:     repo.New(db),
		guardian: guardianPolicy,
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

func lookupProvider(provider string) (providers.Descriptor, error) {
	desc, ok := providers.Lookup(provider)
	if !ok {
		return providers.Descriptor{}, oops.E(oops.CodeInvalid, nil, "unknown device integration provider %q", provider)
	}
	return desc, nil
}

func (s *Service) ListProviders(ctx context.Context, payload *gen.ListProvidersPayload) (*gen.ListDeviceIntegrationProvidersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	all := providers.All()
	views := make([]*gen.DeviceIntegrationProvider, 0, len(all))
	for _, d := range all {
		views = append(views, providerView(d))
	}
	return &gen.ListDeviceIntegrationProvidersResult{Providers: views}, nil
}

func (s *Service) GetConfig(ctx context.Context, payload *gen.GetConfigPayload) (*gen.DeviceIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, err := lookupProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	cfg, err := s.store.LoadConfig(ctx, authCtx.ActiveOrganizationID, desc.ID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return emptyConfigView(authCtx.ActiveOrganizationID, desc.ID), nil
	}
	return buildConfigView(*cfg), nil
}

func (s *Service) UpsertConfig(ctx context.Context, payload *gen.UpsertConfigPayload) (*gen.DeviceIntegrationConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, err := lookupProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	creds := providers.Credentials(payload.Credentials)
	settings := providers.Settings(payload.Settings)
	if settings == nil {
		settings = providers.Settings{}
	}

	// Reject malformed request shapes before taking the transaction and the
	// config-row lock.
	if err := PreValidateSupplied(desc, creds, settings); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	result, err := s.store.upsertWithTx(ctx, dbtx, desc, authCtx.ActiveOrganizationID, creds, settings, payload.Enabled)
	if err != nil {
		return nil, err
	}
	cfg := result.Config

	// The before snapshot comes from the same transaction that performed the
	// write, so the audit trail cannot record a state the tx never observed.
	var beforeSnap *audit.DeviceIntegrationSnapshot
	if result.Before != nil {
		snap := snapshotFromConfig(*result.Before)
		beforeSnap = &snap
	}
	afterSnap := snapshotFromConfig(cfg)

	if err := s.audit.LogDeviceIntegrationUpsert(ctx, dbtx, audit.LogDeviceIntegrationUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewDeviceIntegrationConfig(cfg.ID),
		SnapshotBefore:   beforeSnap,
		SnapshotAfter:    &afterSnap,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log device integration upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit device integration upsert").LogError(ctx, logger)
	}

	return buildConfigView(cfg), nil
}

func (s *Service) DeleteConfig(ctx context.Context, payload *gen.DeleteConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	desc, err := lookupProvider(payload.Provider)
	if err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	cfg, err := s.store.LoadConfig(ctx, authCtx.ActiveOrganizationID, desc.ID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.store.softDeleteWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, desc.ID); err != nil {
		return err
	}

	if err := s.audit.LogDeviceIntegrationDelete(ctx, dbtx, audit.LogDeviceIntegrationDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewDeviceIntegrationConfig(cfg.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log device integration delete").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit device integration delete").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) TestConnection(ctx context.Context, payload *gen.TestConnectionPayload) (*gen.DeviceIntegrationTestConnectionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, err := lookupProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	cfg, creds, err := s.store.LoadConfigWithCredentials(ctx, authCtx.ActiveOrganizationID, desc.ID)
	if err != nil {
		return nil, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, testConnectionTimeout)
	defer cancel()

	// A probe failure is a result for the caller, not an API error: the
	// endpoint's job is to report whether the vendor accepted the stored
	// credentials. The message is scrubbed of credential values — vendor
	// transport errors can echo request URLs.
	if err := s.probeProvider(probeCtx, desc, creds, cfg.Settings); err != nil {
		message := sanitizeSyncError(err.Error(), creds)
		return &gen.DeviceIntegrationTestConnectionResult{
			OK:      false,
			Message: &message,
		}, nil
	}
	return &gen.DeviceIntegrationTestConnectionResult{
		OK:      true,
		Message: nil,
	}, nil
}

// probeProvider dispatches TestConnection through whichever capability the
// provider implements. All outbound HTTP uses the guardian SSRF-hardened
// client: instance URLs are customer-supplied.
func (s *Service) probeProvider(ctx context.Context, desc providers.Descriptor, creds providers.Credentials, settings providers.Settings) error {
	deps := providers.Deps{Client: boundedProviderClient(s.guardian)}
	switch {
	case desc.NewInventorySource != nil:
		if err := desc.NewInventorySource(deps).TestConnection(ctx, creds, settings); err != nil {
			return fmt.Errorf("test %s connection: %w", desc.ID, err)
		}
		return nil
	case desc.NewEvidenceSink != nil:
		if err := desc.NewEvidenceSink(deps).TestConnection(ctx, creds, settings); err != nil {
			return fmt.Errorf("test %s connection: %w", desc.ID, err)
		}
		return nil
	default:
		return oops.E(oops.CodeUnexpected, nil, "provider %s has no testable capability", desc.ID)
	}
}

func (s *Service) ListSchedules(ctx context.Context, payload *gen.ListSchedulesPayload) (*gen.ListDeviceIntegrationSchedulesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, err := lookupProvider(payload.Provider)
	if err != nil {
		return nil, err
	}

	result := &gen.ListDeviceIntegrationSchedulesResult{
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       desc.ID,
		Schedules:      []*gen.DeviceIntegrationScheduleState{},
	}

	cfg, err := s.store.LoadConfig(ctx, authCtx.ActiveOrganizationID, desc.ID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return result, nil
	}

	rows, err := s.repo.ListSchedulesWithSync(ctx, cfg.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list device integration schedules")
	}

	// Present schedules in the provider's declared order so the dashboard
	// renders a stable, meaningful sequence; unknown (e.g. legacy) schedules
	// follow.
	byName := make(map[string]repo.ListSchedulesWithSyncRow, len(rows))
	for _, row := range rows {
		byName[row.Schedule] = row
	}
	for _, spec := range desc.Schedules {
		if row, ok := byName[spec.Schedule]; ok {
			result.Schedules = append(result.Schedules, scheduleView(stateFromListRow(row)))
			delete(byName, spec.Schedule)
		}
	}
	for _, row := range rows {
		if _, ok := byName[row.Schedule]; ok {
			result.Schedules = append(result.Schedules, scheduleView(stateFromListRow(row)))
		}
	}
	return result, nil
}

// resolveScheduleTarget validates a provider/schedule pair and loads the
// config they belong to.
func (s *Service) resolveScheduleTarget(ctx context.Context, orgID string, provider string, schedule string) (providers.Descriptor, Config, error) {
	desc, err := lookupProvider(provider)
	if err != nil {
		return providers.Descriptor{}, Config{}, err
	}
	known := false
	for _, spec := range desc.Schedules {
		if spec.Schedule == schedule {
			known = true
			break
		}
	}
	if !known {
		return providers.Descriptor{}, Config{}, oops.E(oops.CodeInvalid, nil, "unknown schedule %q for provider %s", schedule, desc.ID)
	}
	cfg, err := s.store.LoadConfig(ctx, orgID, desc.ID)
	if err != nil {
		return providers.Descriptor{}, Config{}, err
	}
	if cfg == nil {
		return providers.Descriptor{}, Config{}, oops.E(oops.CodeNotFound, nil, "no %s integration configured", desc.ID)
	}
	return desc, *cfg, nil
}

func (s *Service) SetScheduleEnabled(ctx context.Context, payload *gen.SetScheduleEnabledPayload) (*gen.DeviceIntegrationScheduleState, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, cfg, err := s.resolveScheduleTarget(ctx, authCtx.ActiveOrganizationID, payload.Provider, payload.Schedule)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	if _, err := q.SetScheduleDisabled(ctx, repo.SetScheduleDisabledParams{
		Disabled:                  !payload.Enabled,
		DeviceIntegrationConfigID: cfg.ID,
		Schedule:                  payload.Schedule,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "schedule %s not found", payload.Schedule)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update device integration schedule").LogError(ctx, logger)
	}

	if err := s.audit.LogDeviceIntegrationUpdateSchedule(ctx, dbtx, audit.LogDeviceIntegrationUpdateScheduleEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewDeviceIntegrationConfig(cfg.ID),
		Provider:         desc.ID,
		Schedule:         payload.Schedule,
		Enabled:          payload.Enabled,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log device integration schedule update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit device integration schedule update").LogError(ctx, logger)
	}

	row, err := s.repo.GetScheduleWithSync(ctx, repo.GetScheduleWithSyncParams{
		DeviceIntegrationConfigID: cfg.ID,
		Schedule:                  payload.Schedule,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load device integration schedule")
	}
	return scheduleView(stateFromGetRow(row)), nil
}

func (s *Service) RetrySchedule(ctx context.Context, payload *gen.RetrySchedulePayload) (*gen.DeviceIntegrationScheduleState, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	desc, cfg, err := s.resolveScheduleTarget(ctx, authCtx.ActiveOrganizationID, payload.Provider, payload.Schedule)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	if _, err := q.RetrySchedule(ctx, repo.RetryScheduleParams{
		DeviceIntegrationConfigID: cfg.ID,
		Schedule:                  payload.Schedule,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "schedule %s not found", payload.Schedule)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "retry device integration schedule").LogError(ctx, logger)
	}

	if err := s.audit.LogDeviceIntegrationRetrySchedule(ctx, dbtx, audit.LogDeviceIntegrationRetryScheduleEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewDeviceIntegrationConfig(cfg.ID),
		Provider:         desc.ID,
		Schedule:         payload.Schedule,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log device integration schedule retry").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit device integration schedule retry").LogError(ctx, logger)
	}

	row, err := s.repo.GetScheduleWithSync(ctx, repo.GetScheduleWithSyncParams{
		DeviceIntegrationConfigID: cfg.ID,
		Schedule:                  payload.Schedule,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load device integration schedule")
	}
	return scheduleView(stateFromGetRow(row)), nil
}

func (s *Service) ListManagedDevices(ctx context.Context, payload *gen.ListManagedDevicesPayload) (*gen.ListManagedDevicesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// The design supplies Default(50) and bounds [1,200]; Goa materializes
	// the default when the client omits the parameter.
	limit := payload.Limit

	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil && *payload.Cursor != "" {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid cursor")
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	rows, err := s.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       conv.PtrToPGTextEmpty(payload.Provider),
		CursorID:       cursor,
		Bucket:         conv.PtrToPGTextEmpty(payload.CoverageBucket),
		PageLimit:      int32(limit), //nolint:gosec // design bounds limit to [1,200]
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list managed devices")
	}

	devices := make([]*gen.ManagedDevice, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, deviceView(row))
	}
	var nextCursor *string
	if len(rows) == limit {
		last := rows[len(rows)-1].ID.String()
		nextCursor = &last
	}
	return &gen.ListManagedDevicesResult{
		Devices:    devices,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetCoverage(ctx context.Context, payload *gen.GetCoveragePayload) (*gen.DeviceIntegrationCoverage, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	counts, err := s.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute device integration coverage")
	}
	unmanaged, err := s.repo.CountUnmanagedAgentUsers(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count unmanaged agent users")
	}

	return &gen.DeviceIntegrationCoverage{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ActiveWindowMinutes: int(activeWindow / time.Minute),
		AgentActive:         counts.AgentActive,
		AgentStale:          counts.AgentStale,
		NoAgent:             counts.NoAgent,
		NoEmail:             counts.NoEmail,
		UnresolvedEmail:     counts.UnresolvedEmail,
		Missing:             counts.Missing,
		TotalDevices:        counts.Total,
		UnmanagedAgentUsers: unmanaged,
	}, nil
}

func snapshotFromConfig(cfg Config) audit.DeviceIntegrationSnapshot {
	return audit.DeviceIntegrationSnapshot{
		Provider:       cfg.Provider,
		Enabled:        cfg.Enabled,
		HasCredentials: true,
		Settings:       cfg.Settings,
	}
}
