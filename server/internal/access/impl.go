package access

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
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

func (s *Service) ListGrants(ctx context.Context, payload *gen.ListGrantsPayload) (*gen.ListGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	rows, err := s.repo.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   conv.PtrValOr(payload.PrincipalUrn, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list principal grants").Log(ctx, s.logger)
	}

	grants := make([]*gen.Grant, len(rows))
	for i, row := range rows {
		grants[i] = grantFromRow(row)
	}

	return &gen.ListGrantsResult{Grants: grants}, nil
}

func (s *Service) UpsertGrants(ctx context.Context, payload *gen.UpsertGrantsPayload) (*gen.UpsertGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbTX, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbTX.Rollback(ctx) })

	tr := s.repo.WithTx(dbTX)

	grants := make([]*gen.Grant, 0, len(payload.Grants))

	for _, form := range payload.Grants {
		if form == nil {
			continue
		}

		row, err := tr.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   form.PrincipalUrn,
			Scope:          form.Scope,
			Resource:       form.Resource,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "upsert principal grant").Log(ctx, s.logger)
		}

		grants = append(grants, grantFromRow(row))
	}

	if err := dbTX.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit upsert grants").Log(ctx, s.logger)
	}

	return &gen.UpsertGrantsResult{Grants: grants}, nil
}

func (s *Service) RemoveGrants(ctx context.Context, payload *gen.RemoveGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	dbTX, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbTX.Rollback(ctx) })

	tr := s.repo.WithTx(dbTX)

	for _, entry := range payload.Grants {
		if entry == nil {
			continue
		}

		_, err = tr.DeletePrincipalGrantByTuple(ctx, repo.DeletePrincipalGrantByTupleParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   entry.PrincipalUrn,
			Scope:          entry.Scope,
			Resource:       entry.Resource,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "remove grant").Log(ctx, s.logger)
		}
	}

	if err := dbTX.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remove grants").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) RemovePrincipalGrants(ctx context.Context, payload *gen.RemovePrincipalGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	_, err := s.repo.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   payload.PrincipalUrn,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove principal grants").Log(ctx, s.logger)
	}

	return nil
}

func grantFromRow(row repo.PrincipalGrant) *gen.Grant {
	return &gen.Grant{
		ID:             row.ID.String(),
		OrganizationID: row.OrganizationID,
		PrincipalUrn:   row.PrincipalUrn.String(),
		PrincipalType:  row.PrincipalType,
		Scope:          row.Scope,
		Resource:       row.Resource,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
