package dataexports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"

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

func (s *Service) ListDestinations(ctx context.Context, _ *gen.ListDestinationsPayload) (*gen.ListDestinationsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	destinations, err := s.listOtelDestinations(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	sort.Slice(destinations, func(i, j int) bool {
		if destinations[i].CreatedAt == destinations[j].CreatedAt {
			return destinations[i].ID < destinations[j].ID
		}
		return destinations[i].CreatedAt < destinations[j].CreatedAt
	})

	return &gen.ListDestinationsResult{Destinations: destinations}, nil
}

func (s *Service) ListForOrg(ctx context.Context, _ *gen.ListForOrgPayload) (*gen.ListDataExportsForOrgResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	queries := repo.New(s.db)
	destinationRows, err := queries.ListOtelDestinationsByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization OTEL destinations").LogError(ctx, logger)
	}
	destinations, err := s.buildDestinationViews(destinationRows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode organization OTEL destinations").LogError(ctx, logger)
	}

	routeRows, err := queries.ListDataExportRoutesByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization data export routes").LogError(ctx, logger)
	}
	levelRows, err := queries.ListMCPConnectionLogRouteMinSeveritiesByOrganizationID(ctx, repo.ListMCPConnectionLogRouteMinSeveritiesByOrganizationIDParams{
		DefaultMinSeverity: DefaultMCPConnectionLogMinSeverity,
		OrganizationID:     authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization MCP connection log levels").LogError(ctx, logger)
	}
	levels := make(map[uuid.UUID]string, len(levelRows))
	for _, level := range levelRows {
		levels[level.DataExportRouteID] = level.MinSeverity
	}
	routes := make([]*gen.DataExportRoute, 0, len(routeRows))
	for _, row := range routeRows {
		routes = append(routes, buildDataExportRouteView(row, routeMinSeverity(row, levels)))
	}

	return &gen.ListDataExportsForOrgResult{
		Destinations: destinations,
		Routes:       routes,
	}, nil
}

func (s *Service) listOtelDestinations(ctx context.Context, authCtx *contextvalues.AuthContext) ([]*gen.Destination, error) {
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	rows, err := repo.New(s.db).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list OTEL destinations").LogError(ctx, logger)
	}

	destinations, err := s.buildDestinationViews(rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode OTEL destinations").LogError(ctx, logger)
	}

	return destinations, nil
}

func (s *Service) buildDestinationViews(rows []repo.OtelDestination) ([]*gen.Destination, error) {
	destinations := make([]*gen.Destination, 0, len(rows))
	for _, row := range rows {
		view, err := s.buildDestinationView(row)
		if err != nil {
			return nil, err
		}
		destinations = append(destinations, view)
	}
	return destinations, nil
}

func (s *Service) CreateDestination(ctx context.Context, payload *gen.CreateDestinationPayload) (*gen.Destination, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	destinationType, err := parseDestinationType(payload.DestinationType)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid destination_type")
	}
	switch destinationType {
	case destinationTypeOTEL:
		return s.createOtelDestination(ctx, authCtx, payload)
	default:
		return nil, oops.E(oops.CodeInvalid, fmt.Errorf("unsupported destination type %q", destinationType), "invalid destination_type")
	}
}

func (s *Service) createOtelDestination(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.CreateDestinationPayload) (*gen.Destination, error) {
	if payload.Otel == nil {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing OTEL configuration"), "otel configuration is required")
	}

	configuration, err := NormalizeDestinationConfiguration(payload.Name, payload.Otel.EndpointURL, payload.SensitiveData)
	if err != nil {
		return nil, err
	}
	headerInputs := make([]destinationHeaderInput, len(payload.Otel.Headers))
	for i, input := range payload.Otel.Headers {
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
		Name:             configuration.Name,
		EndpointUrl:      configuration.EndpointURL,
		HeadersEncrypted: headersEncrypted,
		SensitiveData:    conv.ToPGText(configuration.SensitiveData),
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

	return buildOtelDestinationView(row, headers, configuration.SensitiveData), nil
}

func (s *Service) UpdateDestination(ctx context.Context, payload *gen.UpdateDestinationPayload) (*gen.Destination, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	destinationType, err := parseDestinationType(payload.DestinationType)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid destination_type")
	}
	switch destinationType {
	case destinationTypeOTEL:
		return s.updateOtelDestination(ctx, authCtx, payload)
	default:
		return nil, oops.E(oops.CodeInvalid, fmt.Errorf("unsupported destination type %q", destinationType), "invalid destination_type")
	}
}

func (s *Service) updateOtelDestination(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.UpdateDestinationPayload) (*gen.Destination, error) {
	if payload.Otel == nil {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing OTEL configuration"), "otel configuration is required")
	}

	destinationID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid destination id")
	}
	configuration, err := NormalizeDestinationConfiguration(payload.Name, payload.Otel.EndpointURL, payload.SensitiveData)
	if err != nil {
		return nil, err
	}
	policy := sensitiveData(configuration.SensitiveData)

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
	headerInputs := make([]destinationHeaderInput, len(payload.Otel.Headers))
	for i, input := range payload.Otel.Headers {
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
	headersEncrypted := before.HeadersEncrypted
	if !maps.Equal(headers, existingHeaders) {
		headersEncrypted, err = s.encryptHeaders(headers)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encrypt OTEL destination headers").LogError(ctx, logger)
		}
	}
	beforeSnapshot := destinationSnapshot(before.Name, before.EndpointUrl, existingHeaders, beforePolicy)

	after, err := queries.UpdateOtelDestination(ctx, repo.UpdateOtelDestinationParams{
		Name:             configuration.Name,
		EndpointUrl:      configuration.EndpointURL,
		HeadersEncrypted: headersEncrypted,
		SensitiveData:    conv.ToPGText(configuration.SensitiveData),
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

	return buildOtelDestinationView(after, headers, configuration.SensitiveData), nil
}

func (s *Service) DeleteDestination(ctx context.Context, payload *gen.DeleteDestinationPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	destinationType, err := parseDestinationType(payload.DestinationType)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid destination_type")
	}
	switch destinationType {
	case destinationTypeOTEL:
		return s.deleteOtelDestination(ctx, authCtx, payload.ID)
	default:
		return oops.E(oops.CodeInvalid, fmt.Errorf("unsupported destination type %q", destinationType), "invalid destination_type")
	}
}

func (s *Service) deleteOtelDestination(ctx context.Context, authCtx *contextvalues.AuthContext, id string) error {
	destinationID, err := uuid.Parse(id)
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

func (s *Service) buildDestinationView(row repo.OtelDestination) (*gen.Destination, error) {
	headers, err := s.decryptHeaders(row.HeadersEncrypted)
	if err != nil {
		return nil, err
	}
	policy, err := sensitiveDataFromRow(row.SensitiveData)
	if err != nil {
		return nil, err
	}
	return buildOtelDestinationView(row, headers, string(policy)), nil
}

type destinationType string

const destinationTypeOTEL destinationType = "otel"

func parseDestinationType(value string) (destinationType, error) {
	destination := destinationType(value)
	if destination != destinationTypeOTEL {
		return "", fmt.Errorf("unsupported destination type %q", value)
	}
	return destination, nil
}

func (s *Service) ListRoutes(ctx context.Context, _ *gen.ListRoutesPayload) (*gen.ListDataExportRoutesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	queries := repo.New(s.db)
	rows, err := queries.ListDataExportRoutes(ctx, repo.ListDataExportRoutesParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list data export routes").LogError(ctx, logger)
	}
	levelRows, err := queries.ListMCPConnectionLogRouteMinSeverities(ctx, repo.ListMCPConnectionLogRouteMinSeveritiesParams{
		DefaultMinSeverity: DefaultMCPConnectionLogMinSeverity,
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list MCP connection log levels").LogError(ctx, logger)
	}
	levels := make(map[uuid.UUID]string, len(levelRows))
	for _, level := range levelRows {
		levels[level.DataExportRouteID] = level.MinSeverity
	}

	result := &gen.ListDataExportRoutesResult{Routes: make([]*gen.DataExportRoute, 0, len(rows))}
	for _, row := range rows {
		result.Routes = append(result.Routes, buildDataExportRouteView(row, routeMinSeverity(row, levels)))
	}
	return result, nil
}

func (s *Service) CreateRoute(ctx context.Context, payload *gen.CreateRoutePayload) (*gen.DataExportRoute, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	source, err := NormalizeDataSource(payload.DataSource)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid data_source")
	}
	minSeverity, err := normalizeRouteMinSeverity(source, payload.MinSeverity)
	if err != nil {
		return nil, err
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
		DataSource:        source,
		Enabled:           payload.Enabled,
		OtelDestinationID: destinationID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "data_export_routes_project_source_key" {
			return nil, oops.E(oops.CodeConflict, err, "a route already exists for this data source")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create data export route").LogError(ctx, logger)
	}
	routeLevel, err := s.writeRouteMinSeverity(ctx, queries, authCtx, row, minSeverity)
	if err != nil {
		return nil, err
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
	return buildDataExportRouteView(row, routeLevel), nil
}

func (s *Service) UpdateRoute(ctx context.Context, payload *gen.UpdateRoutePayload) (*gen.DataExportRoute, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	routeID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid route id")
	}
	source, err := NormalizeDataSource(payload.DataSource)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid data_source")
	}
	minSeverity, err := normalizeRouteMinSeverity(source, payload.MinSeverity)
	if err != nil {
		return nil, err
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
	beforeLevel, err := s.readRouteMinSeverity(ctx, queries, authCtx, before)
	if err != nil {
		return nil, err
	}
	// The settings foreign key pins the data source, so they go before the route moves off it.
	if before.DataSource == DataSourceMCPConnectionLogs && source != DataSourceMCPConnectionLogs {
		if err := queries.DeleteMCPConnectionLogRouteSettings(ctx, repo.DeleteMCPConnectionLogRouteSettingsParams{
			OrganizationID:    authCtx.ActiveOrganizationID,
			ProjectID:         *authCtx.ProjectID,
			DataExportRouteID: before.ID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "remove MCP connection log settings").LogError(ctx, logger)
		}
	}
	after, err := queries.UpdateDataExportRoute(ctx, repo.UpdateDataExportRouteParams{
		DataSource:        source,
		Enabled:           payload.Enabled,
		OtelDestinationID: destinationID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		ID:                routeID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "data_export_routes_project_source_key" {
			return nil, oops.E(oops.CodeConflict, err, "a route already exists for this data source")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update data export route").LogError(ctx, logger)
	}
	afterLevel, err := s.writeRouteMinSeverity(ctx, queries, authCtx, after, minSeverity)
	if err != nil {
		return nil, err
	}

	beforeSnapshot := routeSnapshot(before)
	beforeSnapshot.MinSeverity = beforeLevel
	afterSnapshot := routeSnapshot(after)
	afterSnapshot.MinSeverity = afterLevel
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
	return buildDataExportRouteView(after, afterLevel), nil
}

func (s *Service) DeleteRoute(ctx context.Context, payload *gen.DeleteRoutePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
	ref, err := conv.PtrToNullUUID(destinationID)
	if err != nil {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "invalid otel_destination_id")
	}
	switch err := checkRouteDestination(ctx, queries, s.encryption, organizationID, projectID, ref, enabled); {
	case err == nil:
	case errors.Is(err, ErrRouteDestinationRequired):
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "otel_destination_id is required when the route is enabled")
	case errors.Is(err, ErrRouteDestinationInactive):
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, err, "otel_destination_id must reference an active destination in this project")
	default:
		logger := s.logger.With(
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID.String()),
		)
		// A stored destination that no longer validates is a server-side
		// fault, reported with the reason naming the field that failed.
		message := "load route destination"
		if invalid, ok := errors.AsType[*routeDestinationInvalidError](err); ok {
			message = invalid.reason
		}
		return uuid.NullUUID{}, oops.E(oops.CodeUnexpected, err, "%s", message).LogError(ctx, logger)
	}
	return ref, nil
}

func routeSnapshot(row repo.DataExportRoute) *audit.DataExportRouteSnapshot {
	return &audit.DataExportRouteSnapshot{
		DataSource:        row.DataSource,
		Enabled:           row.Enabled,
		OtelDestinationID: conv.FromNullableUUID(row.OtelDestinationID),
		MinSeverity:       nil,
	}
}

// normalizeRouteMinSeverity validates a requested minimum level. Nil means none was given.
func normalizeRouteMinSeverity(source string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if source != DataSourceMCPConnectionLogs {
		return nil, oops.E(oops.CodeInvalid, fmt.Errorf("min_severity on %q route", source), "min_severity is only supported on mcp_connection_logs routes")
	}
	level, err := NormalizeMinSeverity(*value)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "min_severity must be info, warn or error")
	}
	return &level, nil
}

// readRouteMinSeverity returns the stored level of an MCP connection logs route, or nil for other sources.
func (s *Service) readRouteMinSeverity(ctx context.Context, queries *repo.Queries, authCtx *contextvalues.AuthContext, row repo.DataExportRoute) (*string, error) {
	if row.DataSource != DataSourceMCPConnectionLogs {
		return nil, nil
	}
	level, err := queries.GetMCPConnectionLogRouteMinSeverity(ctx, repo.GetMCPConnectionLogRouteMinSeverityParams{
		DefaultMinSeverity: DefaultMCPConnectionLogMinSeverity,
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		DataExportRouteID:  row.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read MCP connection log level").LogError(ctx, s.logger)
	}
	return &level, nil
}

// writeRouteMinSeverity stores a requested level and returns the route's level after the write.
func (s *Service) writeRouteMinSeverity(ctx context.Context, queries *repo.Queries, authCtx *contextvalues.AuthContext, row repo.DataExportRoute, minSeverity *string) (*string, error) {
	if minSeverity == nil || row.DataSource != DataSourceMCPConnectionLogs {
		return s.readRouteMinSeverity(ctx, queries, authCtx, row)
	}
	level, err := queries.UpsertMCPConnectionLogRouteSettings(ctx, repo.UpsertMCPConnectionLogRouteSettingsParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		MinSeverity:       *minSeverity,
		DataExportRouteID: row.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set MCP connection log level").LogError(ctx, s.logger)
	}
	return &level, nil
}
