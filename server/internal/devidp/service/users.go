package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	srv "github.com/speakeasy-api/gram/server/internal/devidp/gen/http/users/server"
	gen "github.com/speakeasy-api/gram/server/internal/devidp/gen/users"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// UsersService is the dev-idp /rpc/users.* implementation.
type UsersService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
}

var _ gen.Service = (*UsersService)(nil)

func NewUsersService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *UsersService {
	return &UsersService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/service/users"),
		logger: logger.With(attr.SlogComponent("devidp.users")),
		db:     db,
	}
}

func AttachUsers(mux goahttp.Muxer, service *UsersService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *UsersService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.User, error) {
	row, err := repo.New(s.db).CreateUser(ctx, repo.CreateUserParams{
		Email:        p.Email,
		DisplayName:  p.DisplayName,
		PhotoUrl:     conv.PtrToPGTextEmpty(p.PhotoURL),
		GithubHandle: conv.PtrToPGTextEmpty(p.GithubHandle),
		Admin:        conv.PtrToPGBool(p.Admin),
		Whitelisted:  conv.PtrToPGBool(p.Whitelisted),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create user").Log(ctx, s.logger)
	}

	return userView(row), nil
}

func (s *UsersService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.User, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user id")
	}

	row, err := repo.New(s.db).UpdateUser(ctx, repo.UpdateUserParams{
		ID:           id,
		Email:        conv.PtrToPGTextEmpty(p.Email),
		DisplayName:  conv.PtrToPGTextEmpty(p.DisplayName),
		PhotoUrl:     conv.PtrToPGTextEmpty(p.PhotoURL),
		GithubHandle: conv.PtrToPGTextEmpty(p.GithubHandle),
		Admin:        conv.PtrToPGBool(p.Admin),
		Whitelisted:  conv.PtrToPGBool(p.Whitelisted),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "user not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update user").Log(ctx, s.logger)
	}

	return userView(row), nil
}

func (s *UsersService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListUsersResult, error) {
	after := uuid.Nil
	if p.Cursor != nil && *p.Cursor != "" {
		parsed, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		after = parsed
	}

	rows, err := repo.New(s.db).ListUsers(ctx, repo.ListUsersParams{
		After:   after,
		MaxRows: int32(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list users").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(u repo.User) string { return u.ID.String() })

	items := make([]*gen.User, 0, len(page))
	for _, r := range page {
		items = append(items, userView(r))
	}

	return &gen.ListUsersResult{Items: items, NextCursor: nextCursor}, nil
}

// Delete tombstones the user and sweeps any current_users row that names
// it. memberships, auth_codes, and tokens cascade automatically through their
// FK ON DELETE CASCADE constraints. current_users has no FK (workos
// subject_refs are external WorkOS subs, not UUIDs — idp-design.md §5), so
// the sweep is explicit and runs in the same transaction as the user delete.
func (s *UsersService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid user id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin delete-user tx").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	if err := q.DeleteCurrentUsersBySubjectRef(ctx, id.String()); err != nil {
		return oops.E(oops.CodeUnexpected, err, "sweep current_users for user delete").Log(ctx, s.logger)
	}
	if err := q.DeleteUser(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete user").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit delete-user tx").Log(ctx, s.logger)
	}

	return nil
}

func userView(r repo.User) *gen.User {
	return &gen.User{
		ID:           r.ID.String(),
		Email:        r.Email,
		DisplayName:  r.DisplayName,
		PhotoURL:     conv.FromPGText[string](r.PhotoUrl),
		GithubHandle: conv.FromPGText[string](r.GithubHandle),
		Admin:        r.Admin,
		Whitelisted:  r.Whitelisted,
		CreatedAt:    r.CreatedAt.Time.UTC().Format(timeFormat),
		UpdatedAt:    r.UpdatedAt.Time.UTC().Format(timeFormat),
	}
}
