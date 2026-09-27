package explore

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
	srv "github.com/speakeasy-api/gram/server/gen/http/explore/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/explore/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Service is Explore's one server-side surface: the saved queries.
type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	auth    *auth.Auth
	authz   *authz.Engine
	audit   *audit.Logger
	catalog *analytics.Catalog
	now     func() time.Time
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("explore"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/explore"),
		logger:  logger,
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		audit:   auditLogger,
		catalog: analytics.Default,
		now:     time.Now,
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

// authorize resolves the project a request runs in and checks a scope on it.
// Membership is project:read, which every member holds: saving a chart is
// not an administrative act.
func (s *Service) authorize(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

// ListQueries lists the project's saved queries, most recently updated
// first, each validated against the catalog as it is read.
func (s *Service) ListQueries(ctx context.Context, _ *gen.ListQueriesPayload) (*gen.ExploreListQueriesResult, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListQueries(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list queries").LogError(ctx, s.logger)
	}

	now := s.now()
	result := &gen.ExploreListQueriesResult{Queries: make([]*gen.ExploreQuery, 0, len(rows))}
	for _, row := range rows {
		result.Queries = append(result.Queries, mv.BuildExploreQueryView(row, validateSpec(s.catalog, row.Dataset, row.Spec, now)))
	}
	return result, nil
}

// CreateQuery saves a query after planning its spec against the catalog.
func (s *Service) CreateQuery(ctx context.Context, payload *gen.CreateQueryPayload) (*gen.ExploreQuery, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "saving a query requires a user identity")
	}

	spec, err := s.checkSpec(payload.Dataset, payload.Spec)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin query creation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateQuery(ctx, repo.CreateQueryParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGTextEmpty(authCtx.UserID),
		Name:            payload.Name,
		Dataset:         payload.Dataset,
		Spec:            spec,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create query").LogError(ctx, s.logger)
	}

	view := mv.BuildExploreQueryView(row, "")
	if err := s.audit.LogQueryCreate(ctx, dbtx, audit.LogQueryCreateEvent{QueryEventBase: s.auditBase(authCtx, row), Snapshot: view}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit query creation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit query creation").LogError(ctx, s.logger)
	}
	return view, nil
}

// UpdateQuery replaces a saved query's name, dataset and spec.
func (s *Service) UpdateQuery(ctx context.Context, payload *gen.UpdateQueryPayload) (*gen.ExploreQuery, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid query id")
	}

	spec, err := s.checkSpec(payload.Dataset, payload.Spec)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin query update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetQueryForUpdate(ctx, repo.GetQueryForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "query not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load query").LogError(ctx, s.logger)
	}

	row, err := queries.UpdateQuery(ctx, repo.UpdateQueryParams{
		Name:      payload.Name,
		Dataset:   payload.Dataset,
		Spec:      spec,
		ProjectID: *authCtx.ProjectID,
		ID:        id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "query not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update query").LogError(ctx, s.logger)
	}

	now := s.now()
	beforeView := mv.BuildExploreQueryView(before, validateSpec(s.catalog, before.Dataset, before.Spec, now))
	view := mv.BuildExploreQueryView(row, "")
	if err := s.audit.LogQueryUpdate(ctx, dbtx, audit.LogQueryUpdateEvent{QueryEventBase: s.auditBase(authCtx, row), Before: beforeView, After: view}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit query update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit query update").LogError(ctx, s.logger)
	}
	return view, nil
}

// DeleteQuery soft-deletes a saved query. Its creator can always delete it;
// deleting someone else's needs project write access.
func (s *Service) DeleteQuery(ctx context.Context, payload *gen.DeleteQueryPayload) error {
	authCtx, err := s.authorize(ctx, authz.ScopeProjectRead)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid query id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin query deletion").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	existing, err := queries.GetQueryForUpdate(ctx, repo.GetQueryForUpdateParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "query not found")
		}
		return oops.E(oops.CodeUnexpected, err, "load query").LogError(ctx, s.logger)
	}
	if creator := conv.FromPGText[string](existing.CreatedByUserID); creator == nil || *creator != authCtx.UserID {
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
			return err
		}
	}

	row, err := queries.DeleteQuery(ctx, repo.DeleteQueryParams{ProjectID: *authCtx.ProjectID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "query not found")
		}
		return oops.E(oops.CodeUnexpected, err, "delete query").LogError(ctx, s.logger)
	}
	if err := s.audit.LogQueryDelete(ctx, dbtx, audit.LogQueryDeleteEvent{QueryEventBase: s.auditBase(authCtx, row)}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit query deletion").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit query deletion").LogError(ctx, s.logger)
	}
	return nil
}

// checkSpec validates the spec against the catalog and returns it encoded
// for storage.
func (s *Service) checkSpec(dataset string, spec map[string]any) ([]byte, error) {
	if _, ok := s.catalog.Dataset(dataset); !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown_dataset: dataset %q does not exist", dataset)
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "spec is not encodable as JSON")
	}
	if reason := validateSpec(s.catalog, dataset, encoded, s.now()); reason != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", reason)
	}
	return encoded, nil
}

func (s *Service) auditBase(authCtx *contextvalues.AuthContext, row repo.Query) audit.QueryEventBase {
	return audit.QueryEventBase{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        row.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		QueryURN:         urn.NewQuery(row.ID),
		Name:             row.Name,
	}
}
