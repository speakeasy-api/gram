package access

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
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
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	rows, err := repo.New(s.db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   conv.PtrValOr(payload.PrincipalUrn, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list principal grants").Log(ctx, s.logger)
	}

	grants := make([]*gen.Grant, len(rows))
	for i, row := range rows {
		grants[i] = grantFromRow(row)
	}

	return &gen.ListGrantsResult{Grants: grants}, nil
}

func (s *Service) UpsertGrants(ctx context.Context, payload *gen.UpsertGrantsPayload) (*gen.UpsertGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)
	existingGrants, err := listGrants(ctx, tr, authCtx.ActiveOrganizationID, payload.Grants)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list existing grants").Log(ctx, s.logger)
	}

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
			return nil, oops.E(oops.CodeUnexpected, err, "failed to add or update grant").Log(ctx, s.logger)
		}

		grant := grantFromRow(row)
		grants = append(grants, grant)

		key := principalGrantKey(form.PrincipalUrn.String(), form.Scope, form.Resource)
		existing, hadExisting := existingGrants[key]
		var beforeGrant *gen.Grant
		if hadExisting {
			beforeGrant = grantFromRow(existing)
		}

		if err := audit.LogAccessGrantUpsert(ctx, dbtx, audit.LogAccessGrantUpsertEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			GrantBefore:      beforeGrant,
			GrantAfter:       grant,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to create grant upsert audit log").Log(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to commit grant upsert").Log(ctx, s.logger)
	}

	return &gen.UpsertGrantsResult{Grants: grants}, nil
}

func (s *Service) RemoveGrants(ctx context.Context, payload *gen.RemoveGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)
	existingGrants, err := listGrants(ctx, tr, authCtx.ActiveOrganizationID, payload.Grants)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to list existing grants").Log(ctx, s.logger)
	}
	for _, entry := range payload.Grants {
		if entry == nil {
			continue
		}

		key := principalGrantKey(entry.PrincipalUrn.String(), entry.Scope, entry.Resource)
		var existingGrant *gen.Grant
		if existing, ok := existingGrants[key]; ok {
			existingGrant = grantFromRow(existing)
			delete(existingGrants, key)
		}

		_, err = tr.DeletePrincipalGrantByTuple(ctx, repo.DeletePrincipalGrantByTupleParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   entry.PrincipalUrn,
			Scope:          entry.Scope,
			Resource:       entry.Resource,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to remove grant").Log(ctx, s.logger)
		}

		if existingGrant != nil {
			if err := audit.LogAccessGrantRemove(ctx, dbtx, audit.LogAccessGrantRemoveEvent{
				OrganizationID:   authCtx.ActiveOrganizationID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				ActorSlug:        nil,
				Grant:            existingGrant,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to create grant removal audit log").Log(ctx, s.logger)
			}
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit grant removals").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) RemovePrincipalGrants(ctx context.Context, payload *gen.RemovePrincipalGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)
	existingRows, err := tr.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   payload.PrincipalUrn.String(),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to list principal grants").Log(ctx, s.logger)
	}

	_, err = tr.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   payload.PrincipalUrn,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to remove principal grants").Log(ctx, s.logger)
	}

	for _, row := range existingRows {
		if err := audit.LogAccessGrantRemovePrincipal(ctx, dbtx, audit.LogAccessGrantRemovePrincipalEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			Grant:            grantFromRow(row),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create principal grant removal audit log").Log(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit principal grant removals").Log(ctx, s.logger)
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

func listGrants(ctx context.Context, tr *repo.Queries, organizationID string, entries []*gen.GrantEntry) (map[string]repo.PrincipalGrant, error) {
	grantsByKey := make(map[string]repo.PrincipalGrant, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}

		rows, err := tr.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
			OrganizationID: organizationID,
			PrincipalUrn:   entry.PrincipalUrn.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("list grants for principal %q: %w", entry.PrincipalUrn, err)
		}

		for _, row := range rows {
			grantsByKey[principalGrantKey(row.PrincipalUrn.String(), row.Scope, row.Resource)] = row
		}
	}

	return grantsByKey, nil
}

func principalGrantKey(principalURN, scope, resource string) string {
	return fmt.Sprintf("%s|%s|%s", principalURN, scope, resource)
}
