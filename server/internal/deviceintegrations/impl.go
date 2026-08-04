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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// testConnectionTimeout bounds the synchronous connection probe so a
	// hostile or broken target cannot hang the management API (same budget as
	// remotemcp's URL verification).
	testConnectionTimeout = 10 * time.Second

	// provisionTimeout bounds the synchronous connect-time provisioning call
	// (creating a vendor-side connection/resource). It runs inside the config
	// upsert while the per-config advisory lock is held, so it is deliberately
	// tight: a hung vendor must not pin that lock (and the Postgres transaction)
	// for long. It still allows the list-then-create round trips that a
	// find-or-create provisioner makes.
	provisionTimeout = 10 * time.Second

	// activeWindow is the freshness window for the coverage buckets: an
	// assigned user counts as agent-active when their heartbeat is within
	// it. The agent polls every 60s, so an hour is generous — anything
	// staler is a stopped or disabled agent, not a slow one.
	activeWindow = time.Hour
)

// SyncTrigger nudges the background sync machinery to run promptly instead
// of waiting for the coordinator's next tick. Implementations are
// best-effort: the periodic tick remains the reliability backstop, so a
// failed trigger only costs latency, never a sync.
type SyncTrigger interface {
	TriggerSyncNow(ctx context.Context) error
}

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	authz       *authz.Engine
	audit       *audit.Logger
	store       *Store
	repo        *repo.Queries
	guardian    *guardian.Policy
	syncTrigger SyncTrigger
	features    feature.Provider
}

// deviceLevelCoverage reports whether an org matches coverage on hardware
// serials rather than assigned-user emails. Shared by the management API and
// the sync runner so a pushed snapshot and the dashboard an admin is looking
// at can never disagree about the mode.
//
// Resolve it ONCE per request or run and pass the result to every coverage
// query in it: the counts, the device list, and its bucket filter must all
// report the same mode.
//
// Degrades to user-level on any error. The weaker claim is always safe to
// show or publish; failing an org's coverage page, or publishing an
// unprovable claim, is not.
func deviceLevelCoverage(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, features feature.Provider, orgID string) bool {
	if features == nil {
		return false
	}
	// Targeted by PostHog organization group (org slug), matching how the
	// dashboard evaluates it and how FlagBudgets is rolled out.
	// Evaluating the flag without the org group would silently change the
	// question asked: a rollout targeted by organization group cannot match,
	// yet a user- or global-level rule could still answer true and hand back
	// the STRONGER claim off the back of an error. Degrade instead — the
	// documented contract, and the safe direction.
	org, err := orgRepo.New(db).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		logger.WarnContext(ctx, "resolve organization slug for device-level coverage flag, falling back to user-level",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	groups := feature.OrgProjectGroups(org.Slug, "")

	enabled, flagErr := features.IsFlagEnabled(ctx, feature.FlagDeviceLevelCoverage, orgID, groups)
	if flagErr != nil {
		logger.WarnContext(ctx, "device-level coverage flag lookup failed, falling back to user-level",
			attr.SlogError(flagErr),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	return enabled
}

func (s *Service) deviceLevelCoverage(ctx context.Context, orgID string) bool {
	return deviceLevelCoverage(ctx, s.logger, s.db, s.features, orgID)
}

// coverageAttestation names the strongest claim that holds for EVERY active
// device in a response — deliberately not just the org's matching mode.
//
// agent_active is reachable through the email fallback even under device-level
// matching (an agent that predates hardware reporting, or hardware with no
// readable serial), so reporting "device" purely because the mode is on would
// tell the dashboard to print "N devices are running the agent" while some of
// that N is only "their assigned user is running it somewhere". One
// email-matched device downgrades the whole set, which is the same rule the
// evidence path applies per record.
func coverageAttestation(deviceLevel bool, active, deviceAttested int64) string {
	if deviceLevel && active > 0 && deviceAttested == active {
		return string(providers.AttestationDevice)
	}
	return string(providers.AttestationUser)
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
	syncTrigger SyncTrigger,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("deviceintegrations.api"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/deviceintegrations"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		audit:       auditLogger,
		store:       NewStore(logger, db, encryptionClient),
		repo:        repo.New(db),
		guardian:    guardianPolicy,
		syncTrigger: syncTrigger,
		features:    features,
	}
}

// kickSync asks Temporal to run the sync coordinator immediately, so a save
// that made work due (enable, credential fix, "Sync now") syncs in seconds
// instead of at the next tick. It runs on its own goroutine with a detached,
// bounded context: the response must not wait on Temporal's health, and a
// client disconnecting right after the commit must not lose the nudge.
// Best-effort by design — failures are logged and the periodic tick picks
// the due work up regardless.
func (s *Service) kickSync(ctx context.Context, logger *slog.Logger) {
	if s.syncTrigger == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		triggerCtx, cancel := context.WithTimeout(detached, 10*time.Second)
		defer cancel()
		if err := s.syncTrigger.TriggerSyncNow(triggerCtx); err != nil {
			logger.WarnContext(triggerCtx, "failed to trigger immediate device integration sync", attr.SlogError(err))
		}
	}()
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

	// Provisioning (creating the provider's vendor-side object, e.g. a Drata
	// Custom Connection) is handed to the store so it runs on the merged
	// effective config and inside the (org, provider) advisory lock — a partial
	// update provisions correctly, and concurrent first-time connects can't
	// double-create. Nil for providers with nothing to provision.
	provision := func(ctx context.Context, creds providers.Credentials, settings providers.Settings) (providers.Settings, error) {
		return s.provisionIfSupported(ctx, authCtx.ActiveOrganizationID, desc, creds, settings)
	}

	result, err := s.store.upsertWithTx(ctx, dbtx, desc, authCtx.ActiveOrganizationID, creds, settings, payload.Enabled, provision)
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

	// When the save left schedules due (creation, credential fix, enable
	// transition — the store reports it), run them now: all that stands
	// between the user and fresh data is the coordinator's next tick.
	if cfg.Enabled && result.SyncsMadeDue {
		s.kickSync(ctx, logger)
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

// provisionIfSupported lets a provider that implements providers.Provisioner
// create its vendor-side object (e.g. a Drata Custom Connection) during
// connect, returning settings to persist. It is invoked by the store's upsert
// on the merged effective config and under the (org, provider) advisory lock,
// so a partial update provisions correctly and concurrent first-time connects
// cannot double-create; the provider itself no-ops when nothing needs
// provisioning, so the common re-save makes no vendor call while holding the
// lock. A no-op passthrough for providers that don't provision. A provisioning
// failure surfaces as an actionable bad-request so the customer fixes the
// credentials/workspace instead of saving a config that can never sync.
func (s *Service) provisionIfSupported(ctx context.Context, orgID string, desc providers.Descriptor, creds providers.Credentials, settings providers.Settings) (providers.Settings, error) {
	deps := providers.Deps{Client: boundedProviderClient(s.guardian)}
	var prov providers.Provisioner
	switch {
	case desc.NewEvidenceSink != nil:
		if p, ok := desc.NewEvidenceSink(deps).(providers.Provisioner); ok {
			prov = p
		}
	case desc.NewInventorySource != nil:
		if p, ok := desc.NewInventorySource(deps).(providers.Provisioner); ok {
			prov = p
		}
	}
	if prov == nil {
		return settings, nil
	}

	provCtx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()
	out, err := prov.Provision(provCtx, orgID, creds, settings)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "provision %s connection", desc.ID)
	}
	return out, nil
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

	// A resumed schedule may already be past-due; run it now so the
	// schedule-level switch behaves like the connection-level one.
	if payload.Enabled {
		s.kickSync(ctx, logger)
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

	// "Sync now" means now: the schedule is due as of this commit, so run
	// the coordinator instead of waiting out its tick.
	s.kickSync(ctx, logger)

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
		DeviceLevel:    s.deviceLevelCoverage(ctx, authCtx.ActiveOrganizationID),
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

	// Resolved once and shared: the bucket counts and the unmanaged-users tile
	// sit side by side on the same page, so evaluating the flag twice could
	// render a device as covered next to a tile calling its user unmanaged.
	deviceLevel := s.deviceLevelCoverage(ctx, authCtx.ActiveOrganizationID)

	counts, err := s.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		DeviceLevel:    deviceLevel,
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       conv.PtrToPGTextEmpty(payload.Provider),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute device integration coverage")
	}
	unmanaged, err := s.repo.CountUnmanagedAgentUsers(ctx, repo.CountUnmanagedAgentUsersParams{
		DeviceLevel:    deviceLevel,
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       conv.PtrToPGTextEmpty(payload.Provider),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count unmanaged agent users")
	}

	return &gen.DeviceIntegrationCoverage{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ActiveWindowMinutes:       int(activeWindow / time.Minute),
		Attestation:               coverageAttestation(deviceLevel, counts.AgentActive, counts.AgentActiveDeviceAttested),
		AgentActive:               counts.AgentActive,
		AgentActiveDeviceAttested: counts.AgentActiveDeviceAttested,
		AgentStale:                counts.AgentStale,
		AgentOtherDevice:          counts.AgentOtherDevice,
		NoAgent:                   counts.NoAgent,
		NoEmail:                   counts.NoEmail,
		UnresolvedEmail:           counts.UnresolvedEmail,
		Missing:                   counts.Missing,
		TotalDevices:              counts.Total,
		UnmanagedAgentUsers:       unmanaged,
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
