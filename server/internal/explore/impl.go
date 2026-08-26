// Package explore implements authority-aware analytics over semantic event and
// usage datasets.
package explore

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
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
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	ch     clickhouse.Conn
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
	chConn clickhouse.Conn,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("explore"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/explore"),
		logger: logger,
		db:     db,
		ch:     chConn,
		auth:   auth.New(logger, db, sessionManager, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
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

// orgScope is the resolved, authorized input of the org-scoped handlers.
type orgScope struct {
	authCtx    *contextvalues.AuthContext
	orgID      string
	projectIDs []uuid.UUID
}

// resolveOrgScope authorizes the caller and resolves the organization's
// projects, the only tenancy keys stored in ClickHouse.
func (s *Service) resolveOrgScope(ctx context.Context, requiredScope authz.Scope) (orgScope, error) {
	var scope orgScope

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return scope, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: requiredScope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return scope, err
	}
	scope.authCtx = authCtx
	scope.orgID = authCtx.ActiveOrganizationID

	projects, err := projectsrepo.New(s.db).ListProjectsByOrganization(ctx, scope.orgID)
	if err != nil {
		return scope, oops.E(oops.CodeUnexpected, err, "failed to list organization projects").LogError(ctx, s.logger)
	}
	scope.projectIDs = make([]uuid.UUID, 0, len(projects))
	for _, p := range projects {
		scope.projectIDs = append(scope.projectIDs, p.ID)
	}
	return scope, nil
}

// parseWindow parses the inclusive-start, exclusive-end query window into
// inclusive unix-nano bounds. The -1ns end counts boundary rows exactly once.
func parseWindow(from, to string) (int64, int64, error) {
	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return 0, 0, oops.E(oops.CodeBadRequest, err, "invalid 'from' time format, expected ISO 8601")
	}
	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return 0, 0, oops.E(oops.CodeBadRequest, err, "invalid 'to' time format, expected ISO 8601")
	}
	timeStart := fromTime.UnixNano()
	timeEnd := toTime.UnixNano() - 1
	if timeStart > timeEnd {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "'from' time must be before 'to' time")
	}
	return timeStart, timeEnd, nil
}

// mapEngineError converts engine validation errors into bad-request oops
// with their customer-facing text; anything else is unexpected.
func (s *Service) mapEngineError(ctx context.Context, err error) error {
	var unknown *UnknownMemberError
	var invalid *QueryValidationError
	if errors.As(err, &unknown) || errors.As(err, &invalid) {
		return oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return oops.E(oops.CodeUnexpected, err, "error running explore query").LogError(ctx, s.logger)
}

func (s *Service) Meta(ctx context.Context, _ *gen.MetaPayload) (*gen.ExploreMetaResult, error) {
	if _, err := s.resolveOrgScope(ctx, authz.ScopeOrgRead); err != nil {
		return nil, err
	}

	schemas := make([]*gen.ExploreDatasetSchema, 0, len(datasets))
	for i := range datasets {
		ds := &datasets[i]
		fields := make([]*gen.ExploreFieldMeta, 0, len(ds.Fields))
		for j := range ds.Fields {
			f := &ds.Fields[j]
			fields = append(fields, &gen.ExploreFieldMeta{
				Name:        f.Name,
				Label:       f.Label,
				Type:        f.Type,
				Role:        f.Role,
				Unit:        f.Unit,
				Description: f.Description,
				FilterOps:   emptyIfNil(f.filterOps()),
			})
		}
		schemas = append(schemas, &gen.ExploreDatasetSchema{
			Name:        ds.Name,
			Label:       ds.Label,
			Category:    ds.Category,
			Description: ds.Description,
			Grain:       ds.Grain,
			Fields:      fields,
		})
	}

	return &gen.ExploreMetaResult{
		Datasets: schemas,
	}, nil
}

func (s *Service) Query(ctx context.Context, payload *gen.QueryPayload) (*gen.ExploreQueryResult, error) {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	timeStart, timeEnd, err := parseWindow(payload.From, payload.To)
	if err != nil {
		return nil, err
	}

	q := Query{
		Dataset:            payload.Dataset,
		Calculations:       calculationsFromPayload(payload.Calculations),
		GroupBy:            payload.GroupBy,
		GroupExpressions:   groupExpressionsFromPayload(payload.GroupExpressions),
		Filters:            filtersFromPayload(payload.Filters),
		TimeStart:          timeStart,
		TimeEnd:            timeEnd,
		GranularitySeconds: conv.PtrValOr(payload.GranularitySeconds, 0),
		ProjectIDs:         scope.projectIDs,
		SortBy:             conv.PtrValOr(payload.SortBy, ""),
		SortDesc:           payload.SortDesc,
		Limit:              payload.Limit,
	}

	p, err := plan(q)
	if err != nil {
		return nil, s.mapEngineError(ctx, err)
	}
	res, err := execute(ctx, s.ch, p)
	if err != nil {
		return nil, s.mapEngineError(ctx, err)
	}

	rows := make([]*gen.ExploreRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		bucket := ""
		if q.GranularitySeconds > 0 {
			bucket = time.Unix(0, r.BucketUnixNano).UTC().Format(time.RFC3339)
		}
		rows = append(rows, &gen.ExploreRow{
			Bucket: bucket,
			Group:  emptyIfNil(r.Group),
			Values: r.Values,
		})
	}

	return &gen.ExploreQueryResult{
		Calculations:       canonicalCalculationNames(q.Calculations),
		Dataset:            res.Dataset,
		GroupBy:            p.groupNames(),
		GranularitySeconds: q.GranularitySeconds,
		Rows:               rows,
	}, nil
}

func (s *Service) DimensionValues(ctx context.Context, payload *gen.DimensionValuesPayload) (*gen.DimensionValuesResult, error) {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	timeStart, timeEnd, err := parseWindow(payload.From, payload.To)
	if err != nil {
		return nil, err
	}

	values, err := dimensionValues(ctx, s.ch, scope.projectIDs, payload.Dataset, payload.Dimension, timeStart, timeEnd)
	if err != nil {
		return nil, s.mapEngineError(ctx, err)
	}
	return &gen.DimensionValuesResult{Values: emptyIfNil(values)}, nil
}

func (s *Service) ListSavedQueries(ctx context.Context, _ *gen.ListSavedQueriesPayload) (*gen.ListSavedQueriesResult, error) {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListExploreSavedQueries(ctx, scope.orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing saved queries").LogError(ctx, s.logger)
	}
	out := make([]*gen.ExploreSavedQuery, 0, len(rows))
	for _, row := range rows {
		query, err := savedQueryFromRow(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error reading saved query").LogError(ctx, s.logger)
		}
		out = append(out, buildSavedQueryView(query))
	}
	return &gen.ListSavedQueriesResult{Queries: out}, nil
}

func (s *Service) CreateSavedQuery(ctx context.Context, payload *gen.CreateSavedQueryPayload) (*gen.ExploreSavedQuery, error) {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	spec := savedQuerySpec{
		Dataset:            payload.Dataset,
		Calculations:       calculationsFromPayload(payload.Calculations),
		GroupBy:            emptyIfNil(payload.GroupBy),
		GroupExpressions:   groupExpressionsFromPayload(payload.GroupExpressions),
		Filters:            filtersFromPayload(payload.Filters),
		GranularitySeconds: conv.PtrValOr(payload.GranularitySeconds, 0),
		SortBy:             conv.PtrValOr(payload.SortBy, ""),
		SortDesc:           payload.SortDesc,
		Limit:              payload.Limit,
	}
	if err := s.validateSpec(spec); err != nil {
		return nil, err
	}
	specJSON, err := encodeSavedQuerySpec(spec)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding saved query").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting saved query create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateExploreSavedQuery(ctx, repo.CreateExploreSavedQueryParams{
		OrganizationID: scope.orgID,
		Name:           payload.Name,
		ChartType:      payload.ChartType,
		TimeWindow:     payload.Window,
		Spec:           specJSON,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving query").LogError(ctx, s.logger)
	}
	if err := s.audit.LogExploreSavedQueryCreate(ctx, dbtx, audit.LogExploreSavedQueryCreateEvent{
		OrganizationID:        scope.orgID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, scope.authCtx.UserID),
		ActorDisplayName:      scope.authCtx.Email,
		ActorSlug:             nil,
		ExploreSavedQueryURN:  urn.NewExploreSavedQuery(row.ID),
		ExploreSavedQueryName: row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error auditing saved query create").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing saved query create").LogError(ctx, s.logger)
	}

	query, err := savedQueryFromRow(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading saved query").LogError(ctx, s.logger)
	}
	return buildSavedQueryView(query), nil
}

func (s *Service) UpdateSavedQuery(ctx context.Context, payload *gen.UpdateSavedQueryPayload) (*gen.ExploreSavedQuery, error) {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid saved query id")
	}

	spec := savedQuerySpec{
		Dataset:            payload.Dataset,
		Calculations:       calculationsFromPayload(payload.Calculations),
		GroupBy:            emptyIfNil(payload.GroupBy),
		GroupExpressions:   groupExpressionsFromPayload(payload.GroupExpressions),
		Filters:            filtersFromPayload(payload.Filters),
		GranularitySeconds: conv.PtrValOr(payload.GranularitySeconds, 0),
		SortBy:             conv.PtrValOr(payload.SortBy, ""),
		SortDesc:           payload.SortDesc,
		Limit:              payload.Limit,
	}
	if err := s.validateSpec(spec); err != nil {
		return nil, err
	}
	specJSON, err := encodeSavedQuerySpec(spec)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding saved query").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting saved query update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	existing, err := queries.LockExploreSavedQuery(ctx, repo.LockExploreSavedQueryParams{
		OrganizationID: scope.orgID,
		ID:             id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, nil, "saved query not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading saved query").LogError(ctx, s.logger)
	}

	row, err := queries.UpdateExploreSavedQuery(ctx, repo.UpdateExploreSavedQueryParams{
		Name:           payload.Name,
		ChartType:      payload.ChartType,
		TimeWindow:     payload.Window,
		Spec:           specJSON,
		OrganizationID: scope.orgID,
		ID:             id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating saved query").LogError(ctx, s.logger)
	}
	beforeSnapshot := savedQueryAuditSnapshot(existing)
	afterSnapshot := savedQueryAuditSnapshot(row)
	if err := s.audit.LogExploreSavedQueryUpdate(ctx, dbtx, audit.LogExploreSavedQueryUpdateEvent{
		OrganizationID:                  scope.orgID,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, scope.authCtx.UserID),
		ActorDisplayName:                scope.authCtx.Email,
		ActorSlug:                       nil,
		ExploreSavedQueryURN:            urn.NewExploreSavedQuery(row.ID),
		ExploreSavedQueryName:           row.Name,
		ExploreSavedQuerySnapshotBefore: &beforeSnapshot,
		ExploreSavedQuerySnapshotAfter:  &afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error auditing saved query update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing saved query update").LogError(ctx, s.logger)
	}

	query, err := savedQueryFromRow(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading saved query").LogError(ctx, s.logger)
	}
	return buildSavedQueryView(query), nil
}

func (s *Service) DeleteSavedQuery(ctx context.Context, payload *gen.DeleteSavedQueryPayload) error {
	scope, err := s.resolveOrgScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid saved query id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error starting saved query delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	existing, err := queries.LockExploreSavedQuery(ctx, repo.LockExploreSavedQueryParams{
		OrganizationID: scope.orgID,
		ID:             id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "saved query not found")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading saved query").LogError(ctx, s.logger)
	}

	if _, err := queries.SoftDeleteExploreSavedQuery(ctx, repo.SoftDeleteExploreSavedQueryParams{
		OrganizationID: scope.orgID,
		ID:             id,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting saved query").LogError(ctx, s.logger)
	}
	if err := s.audit.LogExploreSavedQueryDelete(ctx, dbtx, audit.LogExploreSavedQueryDeleteEvent{
		OrganizationID:        scope.orgID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, scope.authCtx.UserID),
		ActorDisplayName:      scope.authCtx.Email,
		ActorSlug:             nil,
		ExploreSavedQueryURN:  urn.NewExploreSavedQuery(existing.ID),
		ExploreSavedQueryName: existing.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error auditing saved query delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing saved query delete").LogError(ctx, s.logger)
	}
	return nil
}

func savedQueryAuditSnapshot(row repo.ExploreSavedQuery) audit.ExploreSavedQuerySnapshot {
	return audit.ExploreSavedQuerySnapshot{
		Name:       row.Name,
		ChartType:  row.ChartType,
		TimeWindow: row.TimeWindow,
		Spec:       append([]byte(nil), row.Spec...),
	}
}

// validateSpec plans the saved spec over a synthetic window so member typos
// are rejected at save time, not when the widget first renders.
func (s *Service) validateSpec(spec savedQuerySpec) error {
	_, err := plan(Query{
		Dataset:            spec.Dataset,
		Calculations:       spec.Calculations,
		GroupBy:            spec.GroupBy,
		GroupExpressions:   spec.GroupExpressions,
		Filters:            spec.Filters,
		TimeStart:          0,
		TimeEnd:            time.Now().UnixNano(),
		GranularitySeconds: spec.GranularitySeconds,
		ProjectIDs:         nil,
		SortBy:             spec.SortBy,
		SortDesc:           spec.SortDesc,
		Limit:              spec.Limit,
	})
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return nil
}

// buildSavedQueryView converts a stored saved query into its API shape.
func buildSavedQueryView(q savedQuery) *gen.ExploreSavedQuery {
	filters := make([]*gen.ExploreFilter, 0, len(q.Spec.Filters))
	for _, f := range q.Spec.Filters {
		// Specs saved before operators existed omit op; empty means "in".
		filters = append(filters, &gen.ExploreFilter{Dimension: f.Dimension, Op: conv.Default(f.Op, "in"), Values: f.Values})
	}
	calculations := make([]*gen.ExploreCalculation, 0, len(q.Spec.Calculations))
	for _, calculation := range q.Spec.Calculations {
		calculations = append(calculations, &gen.ExploreCalculation{
			Op:     calculation.Op,
			Column: conv.PtrEmpty(calculation.Column),
		})
	}
	return &gen.ExploreSavedQuery{
		ID:                 q.ID,
		Name:               q.Name,
		ChartType:          q.ChartType,
		Window:             q.Window,
		Dataset:            q.Spec.Dataset,
		Calculations:       calculations,
		GroupBy:            emptyIfNil(q.Spec.GroupBy),
		GroupExpressions:   groupExpressionViews(q.Spec.GroupExpressions),
		Filters:            filters,
		GranularitySeconds: q.Spec.GranularitySeconds,
		SortBy:             q.Spec.SortBy,
		SortDesc:           q.Spec.SortDesc,
		Limit:              q.Spec.Limit,
		CreatedAt:          q.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          q.UpdatedAt.Format(time.RFC3339),
	}
}

// calculationsFromPayload converts payload calculations into engine
// calculations, skipping nil entries.
func calculationsFromPayload(in []*gen.ExploreCalculation) []Calculation {
	out := make([]Calculation, 0, len(in))
	for _, c := range in {
		if c == nil {
			continue
		}
		out = append(out, Calculation{Op: c.Op, Column: conv.PtrValOr(c.Column, "")})
	}
	return out
}

func groupExpressionsFromPayload(in []*gen.ExploreGroupExpression) []GroupExpression {
	out := make([]GroupExpression, 0, len(in))
	for _, expression := range in {
		if expression == nil {
			continue
		}
		out = append(out, GroupExpression{
			Name:      expression.Name,
			Dimension: expression.Dimension,
			Op:        expression.Op,
			Values:    expression.Values,
		})
	}
	return out
}

func groupExpressionViews(in []GroupExpression) []*gen.ExploreGroupExpression {
	out := make([]*gen.ExploreGroupExpression, 0, len(in))
	for _, expression := range in {
		out = append(out, &gen.ExploreGroupExpression{
			Name:      expression.Name,
			Dimension: expression.Dimension,
			Op:        conv.Default(expression.Op, "in"),
			Values:    emptyIfNil(expression.Values),
		})
	}
	return out
}

// canonicalCalculationNames lists calculations in request order, deduplicated
// to mirror the planner.
func canonicalCalculationNames(calculations []Calculation) []string {
	out := make([]string, 0, len(calculations))
	seen := make(map[string]bool, len(calculations))
	add := func(c string) {
		if seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, c := range calculations {
		add(c.Canonical())
	}
	return out
}

// filtersFromPayload converts payload filters into engine filters, skipping
// nil entries.
func filtersFromPayload(in []*gen.ExploreFilter) []Filter {
	out := make([]Filter, 0, len(in))
	for _, f := range in {
		if f == nil {
			continue
		}
		out = append(out, Filter{Dimension: f.Dimension, Op: f.Op, Values: f.Values})
	}
	return out
}

// emptyIfNil normalizes nil slices to empty so required response arrays
// never serialize as null.
func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
