package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/users/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/users"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// UsersService is the dev-idp /rpc/users.* implementation.
type UsersService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*UsersService)(nil)

func NewUsersService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *UsersService {
	return &UsersService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/users"),
		logger: logger.With(slog.String("component", "devidp.users")),
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
		ID:           uuid.New(),
		Email:        p.Email,
		DisplayName:  p.DisplayName,
		PhotoUrl:     conv.PtrToNullString(p.PhotoURL),
		GithubHandle: conv.PtrToNullString(p.GithubHandle),
		Admin:        conv.PtrBool(p.Admin, false),
		Whitelisted:  conv.PtrBool(p.Whitelisted, true),
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
		Email:        conv.PtrToNullString(p.Email),
		DisplayName:  conv.PtrToNullString(p.DisplayName),
		PhotoUrl:     conv.PtrToNullString(p.PhotoURL),
		GithubHandle: conv.PtrToNullString(p.GithubHandle),
		Admin:        conv.PtrBool(p.Admin, false),
		Whitelisted:  conv.PtrBool(p.Whitelisted, true),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
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
		MaxRows: int64(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
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

	dbtx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin delete-user tx").Log(ctx, s.logger)
	}
	defer func() { _ = dbtx.Rollback() }()

	q := repo.New(dbtx)
	if err := q.DeleteCurrentUsersBySubjectRef(ctx, id.String()); err != nil {
		return oops.E(oops.CodeUnexpected, err, "sweep current_users for user delete").Log(ctx, s.logger)
	}
	if err := q.DeleteUser(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete user").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit delete-user tx").Log(ctx, s.logger)
	}

	return nil
}

func userView(r repo.User) *gen.User {
	return &gen.User{
		ID:           r.ID.String(),
		Email:        r.Email,
		DisplayName:  r.DisplayName,
		PhotoURL:     conv.FromNullString(r.PhotoUrl),
		GithubHandle: conv.FromNullString(r.GithubHandle),
		Admin:        r.Admin,
		Whitelisted:  r.Whitelisted,
		CreatedAt:    r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:    r.UpdatedAt.UTC().Format(timeFormat),
	}
}
