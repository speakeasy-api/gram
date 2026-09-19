package analytics

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	srv "github.com/speakeasy-api/gram/server/gen/http/analytics/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Service is the analytics query API: query and describe over the catalog.
type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	auth    *auth.Auth
	authz   *authz.Engine
	ch      Querier
	catalog *Catalog
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, ch Querier, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("analytics"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry/analytics"),
		logger:  logger,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		ch:      ch,
		catalog: Default,
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

// authorize resolves the one project a request runs against. Organization
// comes from the session and project from the project header; neither is
// ever caller-supplied in the body.
func (s *Service) authorize(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

// Query compiles the request against the catalog and runs it.
func (s *Service) Query(ctx context.Context, payload *gen.QueryPayload) (*gen.AnalyticsQueryResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}

	req, err := requestFromPayload(payload)
	if err != nil {
		return nil, err
	}

	plan, err := Compile(s.catalog, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), req)
	if err != nil {
		if invalid, ok := errors.AsType[*Error](err); ok {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", invalid.Error())
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to compile analytics query").LogError(ctx, s.logger)
	}

	rows, err := plan.Run(ctx, s.ch)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to run analytics query").LogError(ctx, s.logger)
	}

	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return &gen.AnalyticsQueryResult{Dataset: plan.Dataset, Plan: plan.Name, Rows: out}, nil
}

// Describe serves the catalog: every dataset and what each field admits.
func (s *Service) Describe(ctx context.Context, _ *gen.DescribePayload) (*gen.AnalyticsDescribeResult, error) {
	if _, err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return &gen.AnalyticsDescribeResult{Datasets: describeDatasets(s.catalog)}, nil
}

// The int64 nanosecond range covers the years 1678 to 2262. UnixNano is
// undefined outside it, so a bound past it would reach the compiler as some
// other window; representable is checked before the conversion.
var (
	minUnixNanoTime = time.Unix(0, math.MinInt64)
	maxUnixNanoTime = time.Unix(0, math.MaxInt64)
)

func representable(t time.Time) bool {
	return !t.Before(minUnixNanoTime) && !t.After(maxUnixNanoTime)
}

func requestFromPayload(payload *gen.QueryPayload) (Request, error) {
	var zero Request

	from, err := time.Parse(time.RFC3339Nano, payload.From)
	if err != nil {
		return zero, oops.E(oops.CodeBadRequest, err, "invalid_time_range: from is not an RFC 3339 time")
	}
	to, err := time.Parse(time.RFC3339Nano, payload.To)
	if err != nil {
		return zero, oops.E(oops.CodeBadRequest, err, "invalid_time_range: to is not an RFC 3339 time")
	}
	if !representable(from) {
		return zero, oops.E(oops.CodeBadRequest, nil, "invalid_time_range: from is outside the years 1678 to 2262")
	}
	if !representable(to) {
		return zero, oops.E(oops.CodeBadRequest, nil, "invalid_time_range: to is outside the years 1678 to 2262")
	}

	req := Request{
		Dataset:      payload.Dataset,
		FromUnixNano: from.UnixNano(),
		ToUnixNano:   to.UnixNano(),
		Grain:        TimeGrainNone,
		Dimensions:   payload.Dimensions,
		Measures:     make([]Measure, 0, len(payload.Measures)),
		Filters:      make([]Filter, 0, len(payload.Filters)),
		OrderBy:      make([]OrderBy, 0, len(payload.OrderBy)),
		Limit:        0,
		Ungrouped:    payload.Ungrouped,
	}
	if payload.Grain != nil {
		req.Grain = TimeGrain(*payload.Grain)
	}
	if payload.Limit != nil {
		req.Limit = *payload.Limit
	}
	for _, m := range payload.Measures {
		if m == nil {
			return zero, oops.E(oops.CodeBadRequest, nil, "unsatisfiable: measures must not contain null entries")
		}
		req.Measures = append(req.Measures, Measure{Op: m.Op, Field: deref(m.Field), Alias: deref(m.Alias)})
	}
	for _, f := range payload.Filters {
		if f == nil {
			return zero, oops.E(oops.CodeBadRequest, nil, "unsatisfiable: filters must not contain null entries")
		}
		req.Filters = append(req.Filters, Filter{Field: f.Field, Operator: f.Operator, Values: f.Values})
	}
	for _, o := range payload.OrderBy {
		if o == nil {
			return zero, oops.E(oops.CodeBadRequest, nil, "unsatisfiable: order_by must not contain null entries")
		}
		req.OrderBy = append(req.OrderBy, OrderBy{Measure: o.Measure, Direction: o.Direction})
	}
	return req, nil
}

func describeDatasets(catalog *Catalog) []*gen.AnalyticsDataset {
	datasets := catalog.Datasets()
	out := make([]*gen.AnalyticsDataset, 0, len(datasets))
	for _, ds := range datasets {
		fields := make([]*gen.AnalyticsField, 0, len(ds.Fields))
		for _, f := range ds.Fields {
			field := &gen.AnalyticsField{
				Name:         f.Name,
				Type:         string(f.Type),
				Role:         string(f.Role),
				Unit:         nil,
				Operators:    nil,
				Aggregations: nil,
			}
			if f.Unit != "" {
				unit := f.Unit
				field.Unit = &unit
			}
			for _, op := range f.Operators {
				field.Operators = append(field.Operators, string(op))
			}
			for _, agg := range f.Aggregations {
				field.Aggregations = append(field.Aggregations, string(agg))
			}
			fields = append(fields, field)
		}
		out = append(out, &gen.AnalyticsDataset{
			Name:        ds.Name,
			Kind:        string(ds.Kind),
			Grain:       ds.Grain,
			Description: ds.Description,
			Fields:      fields,
		})
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
