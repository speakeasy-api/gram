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
	gen "github.com/speakeasy-api/gram/server/internal/devidp/gen/dev_idp"
	srv "github.com/speakeasy-api/gram/server/internal/devidp/gen/http/dev_idp/server"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Per-mode currentUser values. Mirrors the design's enum
// (idp-design.md §3): the three "local" modes resolve subject_ref as a UUID
// into the local users table; workos resolves it as an external WorkOS sub.
const (
	modeMockSpeakeasy = "local-speakeasy"
	modeOAuth21       = "oauth2-1"
	modeOAuth2        = "oauth2"
	modeWorkos        = "workos"
)

func isLocalMode(mode string) bool {
	return mode == modeMockSpeakeasy || mode == modeOAuth21 || mode == modeOAuth2
}

// DevIdpService is the dev-idp /rpc/devIdp.* implementation.
type DevIdpService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
}

var _ gen.Service = (*DevIdpService)(nil)

func NewDevIdpService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *DevIdpService {
	return &DevIdpService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/service/devidp"),
		logger: logger.With(attr.SlogComponent("devidp.devIdp")),
		db:     db,
	}
}

func AttachDevIdp(mux goahttp.Muxer, service *DevIdpService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *DevIdpService) GetCurrentUser(ctx context.Context, p *gen.GetCurrentUserPayload) (*gen.CurrentUser, error) {
	queries := repo.New(s.db)

	row, err := queries.GetCurrentUser(ctx, p.Mode)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "no currentUser set for mode %q", p.Mode)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read currentUser").Log(ctx, s.logger)
	}

	return s.buildCurrentUserView(ctx, queries, row.Mode, row.SubjectRef)
}

func (s *DevIdpService) SetCurrentUser(ctx context.Context, p *gen.SetCurrentUserPayload) (*gen.CurrentUser, error) {
	subjectRef, err := s.subjectRefForSet(ctx, p)
	if err != nil {
		return nil, err
	}

	queries := repo.New(s.db)
	row, err := queries.UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
		Mode:       p.Mode,
		SubjectRef: subjectRef,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert currentUser").Log(ctx, s.logger)
	}

	return s.buildCurrentUserView(ctx, queries, row.Mode, row.SubjectRef)
}

func (s *DevIdpService) ClearCurrentUser(ctx context.Context, p *gen.ClearCurrentUserPayload) error {
	if err := repo.New(s.db).DeleteCurrentUser(ctx, p.Mode); err != nil {
		return oops.E(oops.CodeUnexpected, err, "clear currentUser").Log(ctx, s.logger)
	}
	return nil
}

// subjectRefForSet validates the per-mode body shape (idp-design.md §6.2):
// local modes require user_id (a UUID that names an existing users row);
// workos requires workos_sub (an arbitrary string the dev-idp does not
// validate against WorkOS at write time). Returns the canonical subject_ref
// string to persist.
func (s *DevIdpService) subjectRefForSet(ctx context.Context, p *gen.SetCurrentUserPayload) (string, error) {
	if p.Mode == modeWorkos {
		if p.WorkosSub == nil || *p.WorkosSub == "" {
			return "", oops.E(oops.CodeBadRequest, nil, "workos_sub is required for mode \"workos\"")
		}
		return *p.WorkosSub, nil
	}

	if !isLocalMode(p.Mode) {
		return "", oops.E(oops.CodeBadRequest, nil, "unknown mode %q", p.Mode)
	}

	if p.UserID == nil || *p.UserID == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "user_id is required for mode %q", p.Mode)
	}
	id, err := uuid.Parse(*p.UserID)
	if err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "invalid user_id")
	}

	// Pre-validate the user exists. Without this, a typo would silently set a
	// stale currentUser that local-speakeasy /validate would later refuse.
	if _, err := repo.New(s.db).GetUser(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", oops.E(oops.CodeNotFound, nil, "user %s not found", id)
		}
		return "", oops.E(oops.CodeUnexpected, err, "look up user for currentUser set").Log(ctx, s.logger)
	}

	return id.String(), nil
}

// buildCurrentUserView renders the discriminated CurrentUser response. For
// local modes it dereferences the subject_ref to the live users row so the
// caller gets the full profile. For workos mode it returns just the
// workos_sub for now — the live WorkOS profile round-trip lands with the
// `workos` mode ticket (idp-design.md §7.2).
func (s *DevIdpService) buildCurrentUserView(ctx context.Context, queries *repo.Queries, mode, subjectRef string) (*gen.CurrentUser, error) {
	if mode == modeWorkos {
		return &gen.CurrentUser{
			Mode: mode,
			User: nil,
			Workos: &gen.WorkosCurrentUser{
				WorkosSub:         subjectRef,
				Email:             nil,
				FirstName:         nil,
				LastName:          nil,
				ProfilePictureURL: nil,
				OrganizationID:    nil,
			},
		}, nil
	}

	id, err := uuid.Parse(subjectRef)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "stored subject_ref for mode %q is not a UUID", mode).Log(ctx, s.logger)
	}

	user, err := queries.GetUser(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "currentUser for mode %q references missing user %s", mode, id)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "look up user for currentUser").Log(ctx, s.logger)
	}

	return &gen.CurrentUser{
		Mode: mode,
		User: &gen.User{
			ID:           user.ID.String(),
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			PhotoURL:     conv.FromPGText[string](user.PhotoUrl),
			GithubHandle: conv.FromPGText[string](user.GithubHandle),
			Admin:        user.Admin,
			Whitelisted:  user.Whitelisted,
			CreatedAt:    user.CreatedAt.Time.UTC().Format(timeFormat),
			UpdatedAt:    user.UpdatedAt.Time.UTC().Format(timeFormat),
		},
		Workos: nil,
	}, nil
}
