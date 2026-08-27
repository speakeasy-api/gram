package dataexports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	srv "github.com/speakeasy-api/gram/server/gen/http/data_exports/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       *auth.Auth
	authz      *authz.Engine
	audit      *audit.Logger
	encryption *encryption.Client
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	encryptionClient *encryption.Client,
) *Service {
	logger = logger.With(attr.SlogComponent("dataexports.api"))
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/dataexports"),
		logger:     logger,
		db:         db,
		auth:       auth.New(logger, db, sessionsManager, authzEngine),
		authz:      authzEngine,
		audit:      auditLogger,
		encryption: encryptionClient,
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

func (s *Service) ListOtelDestinations(ctx context.Context, _ *gen.ListOtelDestinationsPayload) (*gen.ListOtelDestinationsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	rows, err := repo.New(s.db).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list OTEL destinations").LogError(ctx, logger)
	}

	result := &gen.ListOtelDestinationsResult{Destinations: make([]*gen.OtelDestination, 0, len(rows))}
	for _, row := range rows {
		view, err := s.buildDestinationView(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode OTEL destination").LogError(ctx, logger)
		}
		result.Destinations = append(result.Destinations, view)
	}
	return result, nil
}

func (s *Service) CreateOtelDestination(ctx context.Context, payload *gen.CreateOtelDestinationPayload) (*gen.OtelDestination, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	name, err := validateDestinationName(payload.Name)
	if err != nil {
		return nil, err
	}

	endpointURL, err := validateDestinationURL(payload.EndpointURL)
	if err != nil {
		return nil, err
	}
	policy, err := parseSensitiveData(payload.SensitiveData)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid sensitive_data")
	}
	headerInputs := make([]destinationHeaderInput, len(payload.Headers))
	for i, input := range payload.Headers {
		if input != nil {
			headerInputs[i] = destinationHeaderInput{
				name:     input.Name,
				value:    input.Value,
				hasValue: true,
				valid:    true,
			}
		}
	}
	headers, err := normalizeHeaderInputs(headerInputs, nil)
	if err != nil {
		return nil, err
	}
	headersEncrypted, err := s.encryptHeaders(headers)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt OTEL destination headers").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin OTEL destination creation").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateOtelDestination(ctx, repo.CreateOtelDestinationParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Name:             name,
		EndpointUrl:      endpointURL,
		HeadersEncrypted: headersEncrypted,
		SensitiveData:    conv.ToPGText(string(policy)),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create OTEL destination").LogError(ctx, logger)
	}

	if err := s.audit.LogOtelDestinationCreate(ctx, dbtx, audit.LogOtelDestinationCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		DestinationURN:   urn.NewOtelDestination(row.ID),
		DestinationName:  row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log OTEL destination creation").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit OTEL destination creation").LogError(ctx, logger)
	}

	return mv.BuildOtelDestinationView(row, headers, string(policy)), nil
}

func (s *Service) UpdateOtelDestination(ctx context.Context, payload *gen.UpdateOtelDestinationPayload) (*gen.OtelDestination, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	destinationID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid destination id")
	}
	name, err := validateDestinationName(payload.Name)
	if err != nil {
		return nil, err
	}
	endpointURL, err := validateDestinationURL(payload.EndpointURL)
	if err != nil {
		return nil, err
	}
	policy, err := parseSensitiveData(payload.SensitiveData)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid sensitive_data")
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin OTEL destination update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetOtelDestinationForUpdate(ctx, repo.GetOtelDestinationForUpdateParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ID:             destinationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "OTEL destination not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock OTEL destination").LogError(ctx, logger)
	}

	existingHeaders, err := s.decryptHeaders(before.HeadersEncrypted)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode OTEL destination headers").LogError(ctx, logger)
	}
	beforePolicy, err := sensitiveDataFromRow(before.SensitiveData)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode OTEL destination sensitive-data policy").LogError(ctx, logger)
	}
	headerInputs := make([]destinationHeaderInput, len(payload.Headers))
	for i, input := range payload.Headers {
		if input == nil {
			continue
		}

		headerInputs[i] = destinationHeaderInput{name: input.Name, value: "", hasValue: false, valid: true}
		if input.Value != nil {
			headerInputs[i].value = *input.Value
			headerInputs[i].hasValue = true
		}
	}
	headers, err := normalizeHeaderInputs(headerInputs, existingHeaders)
	if err != nil {
		return nil, err
	}
	headersEncrypted, err := s.encryptHeaders(headers)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt OTEL destination headers").LogError(ctx, logger)
	}
	beforeSnapshot := destinationSnapshot(before.Name, before.EndpointUrl, existingHeaders, beforePolicy)

	after, err := queries.UpdateOtelDestination(ctx, repo.UpdateOtelDestinationParams{
		Name:             name,
		EndpointUrl:      endpointURL,
		HeadersEncrypted: headersEncrypted,
		SensitiveData:    conv.ToPGText(string(policy)),
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		ID:               destinationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update OTEL destination").LogError(ctx, logger)
	}
	afterSnapshot := destinationSnapshot(after.Name, after.EndpointUrl, headers, policy)

	if err := s.audit.LogOtelDestinationUpdate(ctx, dbtx, audit.LogOtelDestinationUpdateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		DestinationURN:            urn.NewOtelDestination(after.ID),
		DestinationName:           after.Name,
		DestinationSnapshotBefore: beforeSnapshot,
		DestinationSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log OTEL destination update").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit OTEL destination update").LogError(ctx, logger)
	}

	return mv.BuildOtelDestinationView(after, headers, string(policy)), nil
}

func (s *Service) DeleteOtelDestination(ctx context.Context, payload *gen.DeleteOtelDestinationPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	destinationID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid destination id")
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin OTEL destination deletion").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetOtelDestinationForUpdate(ctx, repo.GetOtelDestinationForUpdateParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ID:             destinationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "OTEL destination not found")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock OTEL destination").LogError(ctx, logger)
	}

	referenced, err := queries.OtelDestinationHasActiveRoutes(ctx, repo.OtelDestinationHasActiveRoutesParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		OtelDestinationID: uuid.NullUUID{UUID: destinationID, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check OTEL destination references").LogError(ctx, logger)
	}
	if referenced {
		return oops.E(oops.CodeConflict, nil, "OTEL destination is still referenced by an active data export route")
	}

	deleted, err := queries.SoftDeleteOtelDestination(ctx, repo.SoftDeleteOtelDestinationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ID:             destinationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete OTEL destination").LogError(ctx, logger)
	}
	if err := s.audit.LogOtelDestinationDelete(ctx, dbtx, audit.LogOtelDestinationDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		DestinationURN:   urn.NewOtelDestination(deleted.ID),
		DestinationName:  before.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log OTEL destination deletion").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit OTEL destination deletion").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) buildDestinationView(row repo.OtelDestination) (*gen.OtelDestination, error) {
	headers, err := s.decryptHeaders(row.HeadersEncrypted)
	if err != nil {
		return nil, err
	}
	policy, err := sensitiveDataFromRow(row.SensitiveData)
	if err != nil {
		return nil, err
	}
	return mv.BuildOtelDestinationView(row, headers, string(policy)), nil
}

type dataSource string

const dataSourceOTELForwarding dataSource = "otel_forwarding"

func parseDataSource(value string) (dataSource, error) {
	source := dataSource(value)
	if source != dataSourceOTELForwarding {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return source, nil
}

func (s *Service) ListRoutes(ctx context.Context, _ *gen.ListRoutesPayload) (*gen.ListDataExportRoutesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	rows, err := repo.New(s.db).ListDataExportRoutes(ctx, repo.ListDataExportRoutesParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list data export routes").LogError(ctx, logger)
	}

	result := &gen.ListDataExportRoutesResult{Routes: make([]*gen.DataExportRoute, 0, len(rows))}
	for _, row := range rows {
		result.Routes = append(result.Routes, mv.BuildDataExportRouteView(row))
	}
	return result, nil
}

func (s *Service) CreateRoute(ctx context.Context, payload *gen.CreateRoutePayload) (*gen.DataExportRoute, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	source, err := parseDataSource(payload.DataSource)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid data_source")
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin data export route creation").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	destinationID, err := s.validateRouteDestination(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID, payload.OtelDestinationID, payload.Enabled)
	if err != nil {
		return nil, err
	}
	row, err := queries.CreateDataExportRoute(ctx, repo.CreateDataExportRouteParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		DataSource:        string(source),
		Enabled:           payload.Enabled,
		OtelDestinationID: destinationID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "data_export_routes_project_source_destination_key" {
			return nil, oops.E(oops.CodeConflict, err, "this destination is already routed from the data source")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create data export route").LogError(ctx, logger)
	}

	if err := s.audit.LogDataExportRouteCreate(ctx, dbtx, audit.LogDataExportRouteCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RouteURN:         urn.NewDataExportRoute(row.ID),
		DataSource:       row.DataSource,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log data export route creation").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit data export route creation").LogError(ctx, logger)
	}
	return mv.BuildDataExportRouteView(row), nil
}

func (s *Service) UpdateRoute(ctx context.Context, payload *gen.UpdateRoutePayload) (*gen.DataExportRoute, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	routeID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid route id")
	}
	source, err := parseDataSource(payload.DataSource)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid data_source")
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin data export route update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetDataExportRouteForUpdate(ctx, repo.GetDataExportRouteForUpdateParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ID:             routeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "data export route not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock data export route").LogError(ctx, logger)
	}

	destinationID, err := s.validateRouteDestination(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID, payload.OtelDestinationID, payload.Enabled)
	if err != nil {
		return nil, err
	}
	after, err := queries.UpdateDataExportRoute(ctx, repo.UpdateDataExportRouteParams{
		DataSource:        string(source),
		Enabled:           payload.Enabled,
		OtelDestinationID: destinationID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		ID:                routeID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "data_export_routes_project_source_destination_key" {
			return nil, oops.E(oops.CodeConflict, err, "this destination is already routed from the data source")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update data export route").LogError(ctx, logger)
	}

	beforeSnapshot := routeSnapshot(before)
	afterSnapshot := routeSnapshot(after)
	if err := s.audit.LogDataExportRouteUpdate(ctx, dbtx, audit.LogDataExportRouteUpdateEvent{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Actor:               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:    authCtx.Email,
		ActorSlug:           nil,
		RouteURN:            urn.NewDataExportRoute(after.ID),
		DataSource:          after.DataSource,
		RouteSnapshotBefore: beforeSnapshot,
		RouteSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log data export route update").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit data export route update").LogError(ctx, logger)
	}
	return mv.BuildDataExportRouteView(after), nil
}

func (s *Service) DeleteRoute(ctx context.Context, payload *gen.DeleteRoutePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	routeID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid route id")
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin data export route deletion").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	deleted, err := queries.SoftDeleteDataExportRoute(ctx, repo.SoftDeleteDataExportRouteParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ID:             routeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "data export route not found")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete data export route").LogError(ctx, logger)
	}
	if err := s.audit.LogDataExportRouteDelete(ctx, dbtx, audit.LogDataExportRouteDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RouteURN:         urn.NewDataExportRoute(deleted.ID),
		DataSource:       deleted.DataSource,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log data export route deletion").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit data export route deletion").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) validateRouteDestination(
	ctx context.Context,
	queries *repo.Queries,
	organizationID string,
	projectID uuid.UUID,
	destinationID *string,
	enabled bool,
) (uuid.NullUUID, error) {
	if destinationID == nil {
		if enabled {
			return uuid.NullUUID{}, oops.E(oops.CodeInvalid, nil, "otel_destination_id is required when the route is enabled")
		}
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	id, err := uuid.Parse(*destinationID)
	if err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "invalid otel_destination_id")
	}
	destination, err := queries.GetOtelDestinationForRoute(ctx, repo.GetOtelDestinationForRouteParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ID:             id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "otel_destination_id must reference an active destination in this project")
	}
	if err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeUnexpected, err, "load route destination").LogError(ctx, s.logger)
	}
	if _, err := validateDestinationURL(destination.EndpointUrl); err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "OTEL destination is not usable")
	}
	if _, err := sensitiveDataFromRow(destination.SensitiveData); err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "OTEL destination has an invalid sensitive-data policy")
	}
	if _, err := s.decryptHeaders(destination.HeadersEncrypted); err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, fmt.Errorf("decode stored headers: %w", err), "OTEL destination is not usable")
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func routeSnapshot(row repo.DataExportRoute) *audit.DataExportRouteSnapshot {
	return &audit.DataExportRouteSnapshot{
		DataSource:        row.DataSource,
		Enabled:           row.Enabled,
		OtelDestinationID: conv.FromNullableUUID(row.OtelDestinationID),
	}
}
