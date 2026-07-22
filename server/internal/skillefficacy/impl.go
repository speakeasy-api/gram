package skillefficacy

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/skill_efficacy/server"
	gen "github.com/speakeasy-api/gram/server/gen/skill_efficacy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maxSamplingCap = 10000

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	features *productfeatures.Client
	audit    *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features *productfeatures.Client,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("skillefficacy.api"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skillefficacy"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		features: features,
		audit:    auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) requireAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:        scope,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, s.logger, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserID(authCtx.UserID),
	)
	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnexpected, err, "check skills feature").LogError(ctx, logger)
	}
	if !enabled {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "skills are not enabled for this organization")
	}

	return authCtx, logger, nil
}

func (s *Service) GetSettings(ctx context.Context, _ *gen.GetSettingsPayload) (*gen.SkillEfficacySettings, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	row, err := skillsrepo.New(s.db).GetSkillEfficacySettingsForOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultView(authCtx.ActiveOrganizationID), nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get skill efficacy settings").LogError(ctx, logger)
	default:
		return buildView(row, false), nil
	}
}

func (s *Service) UpsertSettings(ctx context.Context, payload *gen.UpsertSettingsPayload) (*gen.SkillEfficacySettings, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if err := validateCap("per_skill_daily_cap", payload.PerSkillDailyCap); err != nil {
		return nil, err
	}
	if err := validateCap("org_daily_cap", payload.OrgDailyCap); err != nil {
		return nil, err
	}
	if err := validateCap("new_version_burst", payload.NewVersionBurst); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin skill efficacy settings upsert").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := skillsrepo.New(dbtx)
	if err := queries.LockOrganizationSkillEfficacyBudget(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill efficacy settings").LogError(ctx, logger)
	}
	var beforeSnapshot *audit.SkillEfficacySettingsSnapshot
	before, err := queries.GetSkillEfficacySettingsForOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get existing skill efficacy settings").LogError(ctx, logger)
	default:
		snapshot := buildSnapshot(before)
		beforeSnapshot = &snapshot
	}

	row, err := queries.UpsertSkillEfficacySettingsForOrganization(ctx, skillsrepo.UpsertSkillEfficacySettingsForOrganizationParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Enabled:          payload.Enabled,
		PerSkillDailyCap: conv.SafeInt32(payload.PerSkillDailyCap),
		OrgDailyCap:      conv.SafeInt32(payload.OrgDailyCap),
		NewVersionBurst:  conv.SafeInt32(payload.NewVersionBurst),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert skill efficacy settings").LogError(ctx, logger)
	}
	afterSnapshot := buildSnapshot(row)

	if err := s.audit.LogSkillEfficacySettingsUpsert(ctx, dbtx, audit.LogSkillEfficacySettingsUpsertEvent{
		OrganizationID:                      authCtx.ActiveOrganizationID,
		Actor:                               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                    authCtx.Email,
		ActorSlug:                           nil,
		SkillEfficacySettingsURN:            urn.NewSkillEfficacySettings(authCtx.ActiveOrganizationID),
		SkillEfficacySettingsSnapshotBefore: beforeSnapshot,
		SkillEfficacySettingsSnapshotAfter:  &afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill efficacy settings upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit skill efficacy settings upsert").LogError(ctx, logger)
	}

	return buildView(row, false), nil
}

func validateCap(name string, value int) error {
	if value < 0 || value > maxSamplingCap {
		return oops.E(oops.CodeInvalid, nil, "%s must be between 0 and %d", name, maxSamplingCap)
	}
	return nil
}

func defaultView(organizationID string) *gen.SkillEfficacySettings {
	return &gen.SkillEfficacySettings{
		OrganizationID:   organizationID,
		Enabled:          efficacy.DefaultEnabled,
		PerSkillDailyCap: efficacy.DefaultPerSkillDailyCap,
		OrgDailyCap:      efficacy.DefaultOrgDailyCap,
		NewVersionBurst:  efficacy.DefaultNewVersionBurst,
		IsDefault:        true,
	}
}

func buildView(row skillsrepo.SkillEfficacySetting, isDefault bool) *gen.SkillEfficacySettings {
	return &gen.SkillEfficacySettings{
		OrganizationID:   row.OrganizationID,
		Enabled:          row.Enabled,
		PerSkillDailyCap: int(row.PerSkillDailyCap),
		OrgDailyCap:      int(row.OrgDailyCap),
		NewVersionBurst:  int(row.NewVersionBurst),
		IsDefault:        isDefault,
	}
}

func buildSnapshot(row skillsrepo.SkillEfficacySetting) audit.SkillEfficacySettingsSnapshot {
	return audit.SkillEfficacySettingsSnapshot{
		Enabled:          row.Enabled,
		PerSkillDailyCap: row.PerSkillDailyCap,
		OrgDailyCap:      row.OrgDailyCap,
		NewVersionBurst:  row.NewVersionBurst,
	}
}
