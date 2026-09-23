package sigint

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/sigint/server"
	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	modeMultiLabel   = "multi_label"
	modeExclusive    = "exclusive"
	modeOrderedScore = "ordered_score"
)

type Service struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
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
) *Service {
	logger = logger.With(attr.SlogComponent("sigint"))
	return &Service{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/sigint"),
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
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

func (s *Service) CreateSignal(ctx context.Context, payload *gen.CreateSignalPayload) (*types.SigintSignal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal name").LogError(ctx, s.logger)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate signal id").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin signal create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	row, err := queries.CreateSignal(ctx, repo.CreateSignalParams{
		ID:                 id,
		ProjectID:          *authCtx.ProjectID,
		Name:               name,
		Description:        conv.PtrToPGTextEmpty(payload.Description),
		ClassifierCriteria: conv.PtrToPGTextEmpty(payload.ClassifierCriteria),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create signal").LogError(ctx, s.logger)
	}
	if err := s.audit.LogSigintSignalCreate(ctx, dbtx, audit.LogSigintSignalCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SignalURN:        urn.NewSigintSignal(row.ID),
		SignalName:       row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit signal create").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit signal create").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSignalView(row), nil
}

func (s *Service) GetSignal(ctx context.Context, payload *gen.GetSignalPayload) (*types.SigintSignal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal id").LogError(ctx, s.logger)
	}
	row, err := repo.New(s.db).GetSignal(ctx, repo.GetSignalParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get signal").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSignalView(row), nil
}

func (s *Service) ListSignals(ctx context.Context, payload *gen.ListSignalsPayload) (*gen.ListSigintSignalsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal cursor").LogError(ctx, s.logger)
	}
	rows, err := repo.New(s.db).ListSignals(ctx, repo.ListSignalsParams{ProjectID: *authCtx.ProjectID, Cursor: cursor, LimitValue: conv.SafeInt32(payload.Limit + 1)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list signals").LogError(ctx, s.logger)
	}
	var nextCursor *string
	if len(rows) > payload.Limit {
		rows = rows[:payload.Limit]
		nextCursor = new(rows[len(rows)-1].ID.String())
	}
	return &gen.ListSigintSignalsResult{Signals: mv.BuildSigintSignalListView(rows), NextCursor: nextCursor}, nil
}

func (s *Service) UpdateSignal(ctx context.Context, payload *gen.UpdateSignalPayload) (*types.SigintSignal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal id").LogError(ctx, s.logger)
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin signal update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	before, err := queries.GetSignal(ctx, repo.GetSignalParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get signal for update").LogError(ctx, s.logger)
	}
	params := repo.UpdateSignalParams{
		Name:               before.Name,
		Description:        before.Description,
		ClassifierCriteria: before.ClassifierCriteria,
		ID:                 id,
		ProjectID:          *authCtx.ProjectID,
	}
	if payload.Name != nil {
		params.Name, err = validateName(*payload.Name)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid signal name").LogError(ctx, s.logger)
		}
	}
	if payload.Description != nil {
		params.Description = conv.PtrToPGTextEmpty(payload.Description)
	}
	if payload.ClassifierCriteria != nil {
		params.ClassifierCriteria = conv.PtrToPGTextEmpty(payload.ClassifierCriteria)
	}
	after, err := queries.UpdateSignal(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update signal").LogError(ctx, s.logger)
	}
	beforeView := mv.BuildSigintSignalView(before)
	afterView := mv.BuildSigintSignalView(after)
	if err := s.audit.LogSigintSignalUpdate(ctx, dbtx, audit.LogSigintSignalUpdateEvent{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:     authCtx.Email,
		ActorSlug:            nil,
		SignalURN:            urn.NewSigintSignal(after.ID),
		SignalName:           after.Name,
		SignalSnapshotBefore: beforeView,
		SignalSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit signal update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit signal update").LogError(ctx, s.logger)
	}
	return afterView, nil
}

func (s *Service) DeleteSignal(ctx context.Context, payload *gen.DeleteSignalPayload) (*types.SigintSignal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal id").LogError(ctx, s.logger)
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin signal delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	signal, err := queries.GetSignal(ctx, repo.GetSignalParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get signal for delete").LogError(ctx, s.logger)
	}
	beforeRows, err := queries.ListSensorsForSignal(ctx, repo.ListSensorsForSignalParams{SignalID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list affected sensors").LogError(ctx, s.logger)
	}
	beforeViews := make(map[uuid.UUID]*types.SigintSensor, len(beforeRows))
	for _, row := range beforeRows {
		beforeViews[row.ID] = sensorViewFromAffected(row)
	}
	affectedIDs, err := queries.DeleteSignalMemberships(ctx, repo.DeleteSignalMembershipsParams{ProjectID: *authCtx.ProjectID, SignalID: id})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "detach deleted signal").LogError(ctx, s.logger)
	}
	if len(affectedIDs) > 0 {
		if err := queries.CompactSensorSignalOrder(ctx, repo.CompactSensorSignalOrderParams{ProjectID: *authCtx.ProjectID, SensorIds: affectedIDs}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "compact sensor signal order").LogError(ctx, s.logger)
		}
		if err := queries.TouchSensors(ctx, repo.TouchSensorsParams{ProjectID: *authCtx.ProjectID, SensorIds: affectedIDs}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "touch affected sensors").LogError(ctx, s.logger)
		}
		afterRows, err := queries.GetSensorsByIDs(ctx, repo.GetSensorsByIDsParams{ProjectID: *authCtx.ProjectID, Ids: affectedIDs})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "get affected sensors").LogError(ctx, s.logger)
		}
		for _, row := range afterRows {
			afterView := mv.BuildSigintSensorByIDsView(row)
			if err := s.audit.LogSigintSensorUpdate(ctx, dbtx, audit.LogSigintSensorUpdateEvent{
				OrganizationID:       authCtx.ActiveOrganizationID,
				ProjectID:            *authCtx.ProjectID,
				Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName:     authCtx.Email,
				ActorSlug:            nil,
				SensorURN:            urn.NewSigintSensor(row.ID),
				SensorName:           row.Name,
				SensorSnapshotBefore: beforeViews[row.ID],
				SensorSnapshotAfter:  afterView,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "audit sensor detach").LogError(ctx, s.logger)
			}
		}
	}
	deleted, err := queries.DeleteSignal(ctx, repo.DeleteSignalParams{ID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete signal").LogError(ctx, s.logger)
	}
	if err := s.audit.LogSigintSignalDelete(ctx, dbtx, audit.LogSigintSignalDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SignalURN:        urn.NewSigintSignal(deleted.ID),
		SignalName:       deleted.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit signal delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit signal delete").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSignalView(signal), nil
}

func (s *Service) CreateSensor(ctx context.Context, payload *gen.CreateSensorPayload) (*types.SigintSensor, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor name").LogError(ctx, s.logger)
	}
	mode := string(payload.Mode)
	if err := validateSensorConfiguration(mode, len(payload.SignalIds)); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor configuration").LogError(ctx, s.logger)
	}
	signalIDs, err := parseUniqueSignalIDs(payload.SignalIds)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signal ids").LogError(ctx, s.logger)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate sensor id").LogError(ctx, s.logger)
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin sensor create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	available, err := liveSignalsAvailable(ctx, queries, *authCtx.ProjectID, signalIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate signal ids").LogError(ctx, s.logger)
	}
	if !available {
		return nil, oops.E(oops.CodeNotFound, nil, "signal ids are unavailable").LogError(ctx, s.logger)
	}
	created, err := queries.CreateSensor(ctx, repo.CreateSensorParams{
		ID: id, ProjectID: *authCtx.ProjectID, Name: name,
		Description: conv.PtrToPGTextEmpty(payload.Description), Instructions: conv.PtrToPGTextEmpty(payload.Instructions), Mode: mode,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create sensor").LogError(ctx, s.logger)
	}
	if err := queries.ReplaceSensorSignals(ctx, repo.ReplaceSensorSignalsParams{ProjectID: *authCtx.ProjectID, SensorID: created.ID, SignalIds: signalIDs}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach sensor signals").LogError(ctx, s.logger)
	}
	viewRow, err := queries.GetSensor(ctx, repo.GetSensorParams{ID: created.ID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get created sensor").LogError(ctx, s.logger)
	}
	if err := s.audit.LogSigintSensorCreate(ctx, dbtx, audit.LogSigintSensorCreateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		SensorURN: urn.NewSigintSensor(created.ID), SensorName: created.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit sensor create").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit sensor create").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSensorView(viewRow), nil
}

func (s *Service) GetSensor(ctx context.Context, payload *gen.GetSensorPayload) (*types.SigintSensor, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor id").LogError(ctx, s.logger)
	}
	row, err := repo.New(s.db).GetSensor(ctx, repo.GetSensorParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get sensor").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSensorView(row), nil
}

func (s *Service) ListSensors(ctx context.Context, payload *gen.ListSensorsPayload) (*gen.ListSigintSensorsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor cursor").LogError(ctx, s.logger)
	}
	rows, err := repo.New(s.db).ListSensors(ctx, repo.ListSensorsParams{ProjectID: *authCtx.ProjectID, Cursor: cursor, LimitValue: conv.SafeInt32(payload.Limit + 1)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list sensors").LogError(ctx, s.logger)
	}
	var nextCursor *string
	if len(rows) > payload.Limit {
		rows = rows[:payload.Limit]
		nextCursor = new(rows[len(rows)-1].ID.String())
	}
	return &gen.ListSigintSensorsResult{Sensors: mv.BuildSigintSensorListView(rows), NextCursor: nextCursor}, nil
}

func (s *Service) UpdateSensor(ctx context.Context, payload *gen.UpdateSensorPayload) (*types.SigintSensor, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor id").LogError(ctx, s.logger)
	}
	var replacementIDs []uuid.UUID
	if payload.SignalIds != nil {
		replacementIDs, err = parseUniqueSignalIDs(payload.SignalIds)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid signal ids").LogError(ctx, s.logger)
		}
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin sensor update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	beforeRow, err := queries.GetSensor(ctx, repo.GetSensorParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get sensor for update").LogError(ctx, s.logger)
	}
	params := repo.UpdateSensorParams{
		Name: beforeRow.Name, Description: beforeRow.Description, Instructions: beforeRow.Instructions,
		Mode: beforeRow.Mode, ID: id, ProjectID: *authCtx.ProjectID,
	}
	if payload.Name != nil {
		params.Name, err = validateName(*payload.Name)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor name").LogError(ctx, s.logger)
		}
	}
	if payload.Description != nil {
		params.Description = conv.PtrToPGTextEmpty(payload.Description)
	}
	if payload.Instructions != nil {
		params.Instructions = conv.PtrToPGTextEmpty(payload.Instructions)
	}
	if payload.Mode != nil {
		params.Mode = string(*payload.Mode)
	}
	finalCount := len(beforeRow.SignalIds)
	if payload.SignalIds != nil {
		finalCount = len(replacementIDs)
		available, err := liveSignalsAvailable(ctx, queries, *authCtx.ProjectID, replacementIDs)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate signal ids").LogError(ctx, s.logger)
		}
		if !available {
			return nil, oops.E(oops.CodeNotFound, nil, "signal ids are unavailable").LogError(ctx, s.logger)
		}
	}
	if err := validateSensorConfiguration(params.Mode, finalCount); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor configuration").LogError(ctx, s.logger)
	}
	updated, err := queries.UpdateSensor(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update sensor").LogError(ctx, s.logger)
	}
	if payload.SignalIds != nil {
		if err := queries.ReplaceSensorSignals(ctx, repo.ReplaceSensorSignalsParams{ProjectID: *authCtx.ProjectID, SensorID: id, SignalIds: replacementIDs}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "replace sensor signals").LogError(ctx, s.logger)
		}
	}
	afterRow, err := queries.GetSensor(ctx, repo.GetSensorParams{ID: updated.ID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get updated sensor").LogError(ctx, s.logger)
	}
	beforeView := mv.BuildSigintSensorView(beforeRow)
	afterView := mv.BuildSigintSensorView(afterRow)
	if err := s.audit.LogSigintSensorUpdate(ctx, dbtx, audit.LogSigintSensorUpdateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		SensorURN: urn.NewSigintSensor(updated.ID), SensorName: updated.Name,
		SensorSnapshotBefore: beforeView, SensorSnapshotAfter: afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit sensor update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit sensor update").LogError(ctx, s.logger)
	}
	return afterView, nil
}

func (s *Service) DeleteSensor(ctx context.Context, payload *gen.DeleteSensorPayload) (*types.SigintSensor, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sensor id").LogError(ctx, s.logger)
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin sensor delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockSigintProject(ctx, authCtx.ProjectID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock sigint project").LogError(ctx, s.logger)
	}
	before, err := queries.GetSensor(ctx, repo.GetSensorParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get sensor for delete").LogError(ctx, s.logger)
	}
	if err := queries.DeleteSensorSignals(ctx, repo.DeleteSensorSignalsParams{ProjectID: *authCtx.ProjectID, SensorID: id}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete sensor signals").LogError(ctx, s.logger)
	}
	deleted, err := queries.DeleteSensor(ctx, repo.DeleteSensorParams{ID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete sensor").LogError(ctx, s.logger)
	}
	if err := s.audit.LogSigintSensorDelete(ctx, dbtx, audit.LogSigintSensorDeleteEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil,
		SensorURN: urn.NewSigintSensor(deleted.ID), SensorName: deleted.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit sensor delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit sensor delete").LogError(ctx, s.logger)
	}
	return mv.BuildSigintSensorView(before), nil
}

func validateName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len([]rune(name)) > 200 {
		return "", fmt.Errorf("name must contain 1 to 200 characters")
	}
	return name, nil
}

func validateSensorConfiguration(mode string, memberCount int) error {
	switch mode {
	case modeMultiLabel:
		return nil
	case modeExclusive:
		if memberCount > 255 {
			return fmt.Errorf("exclusive sensors permit at most 255 signals")
		}
		return nil
	case modeOrderedScore:
		if memberCount > 10 {
			return fmt.Errorf("ordered_score sensors permit at most 10 signals")
		}
		return nil
	default:
		return fmt.Errorf("unknown sensor mode %q", mode)
	}
}

func parseUniqueSignalIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for i, value := range values {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("parse signal id at index %d: %w", i, err)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("signal id %s is repeated", id)
		}
		seen[id] = struct{}{}
		ids[i] = id
	}
	return ids, nil
}

func liveSignalsAvailable(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, ids []uuid.UUID) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}
	found, err := queries.ListLiveSignalIDs(ctx, repo.ListLiveSignalIDsParams{ProjectID: projectID, Ids: ids})
	if err != nil {
		return false, fmt.Errorf("list live signals: %w", err)
	}
	return len(found) == len(ids), nil
}

func parseCursor(value *string) (uuid.NullUUID, error) {
	if value == nil || *value == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("parse cursor: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func sensorViewFromAffected(sensor repo.ListSensorsForSignalRow) *types.SigintSensor {
	signalIDs := make([]string, len(sensor.SignalIds))
	for i, id := range sensor.SignalIds {
		signalIDs[i] = id.String()
	}
	return &types.SigintSensor{
		ID: sensor.ID.String(), ProjectID: sensor.ProjectID.String(), Name: sensor.Name,
		Description: conv.FromPGText[string](sensor.Description), Instructions: conv.FromPGText[string](sensor.Instructions),
		Mode: types.SigintSensorMode(sensor.Mode), SignalIds: signalIDs,
		CreatedAt: sensor.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: sensor.UpdatedAt.Time.Format(time.RFC3339),
	}
}
