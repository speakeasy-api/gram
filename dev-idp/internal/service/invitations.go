package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/invitations/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/invitations"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// invitationLifetime is how long a freshly-created invitation stays
// pending before clients should consider it expired. Generous enough that
// long-running test suites don't trip over it.
const invitationLifetime = 30 * 24 * time.Hour

// InvitationsService implements /rpc/invitations.* — the Goa surface
// mirroring the WorkOS user_management invitation lifecycle. The accept
// path is dev-idp-only (no email infra to back a real-WorkOS shape) —
// the dashboard's "accept" button hits invitations.accept directly.
type InvitationsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*InvitationsService)(nil)

func NewInvitationsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *InvitationsService {
	return &InvitationsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/invitations"),
		logger: logger.With(slog.String("component", "devidp.invitations")),
		db:     db,
	}
}

func AttachInvitations(mux goahttp.Muxer, service *InvitationsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *InvitationsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.Invitation, error) {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	inviterNarg := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if p.InviterUserID != nil && *p.InviterUserID != "" {
		inv, err := uuid.Parse(*p.InviterUserID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid inviter_user_id")
		}
		inviterNarg = uuid.NullUUID{UUID: inv, Valid: true}
	}

	row, err := repo.New(s.db).CreateInvitation(ctx, repo.CreateInvitationParams{
		ID:             uuid.New(),
		Email:          p.Email,
		OrganizationID: orgID,
		Token:          newInvitationToken(),
		InviterUserID:  inviterNarg,
		ExpiresAt:      time.Now().Add(invitationLifetime),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create invitation").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

func (s *InvitationsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListInvitationsResult, error) {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	after := uuid.Nil
	if p.Cursor != nil && *p.Cursor != "" {
		parsed, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		after = parsed
	}

	rows, err := repo.New(s.db).ListInvitationsByOrg(ctx, repo.ListInvitationsByOrgParams{
		OrganizationID: orgID,
		After:          after,
		MaxRows:        int64(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list invitations").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(inv repo.Invitation) string { return inv.ID.String() })

	items := make([]*gen.Invitation, 0, len(page))
	for _, inv := range page {
		items = append(items, invitationView(inv))
	}
	return &gen.ListInvitationsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *InvitationsService) Get(ctx context.Context, p *gen.GetPayload) (*gen.Invitation, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid invitation id")
	}
	row, err := repo.New(s.db).GetInvitation(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "invitation not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get invitation").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

func (s *InvitationsService) FindByToken(ctx context.Context, p *gen.FindByTokenPayload) (*gen.Invitation, error) {
	row, err := repo.New(s.db).GetInvitationByToken(ctx, p.Token)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "invitation not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "find invitation by token").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

func (s *InvitationsService) Revoke(ctx context.Context, p *gen.RevokePayload) (*gen.Invitation, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid invitation id")
	}
	row, err := repo.New(s.db).RevokeInvitation(ctx, repo.RevokeInvitationParams{ID: id, Ts: time.Now()})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "invitation not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "revoke invitation").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

func (s *InvitationsService) Resend(ctx context.Context, p *gen.ResendPayload) (*gen.Invitation, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid invitation id")
	}
	row, err := repo.New(s.db).TouchInvitation(ctx, repo.TouchInvitationParams{ID: id, Ts: time.Now()})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "invitation not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resend invitation").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

// Accept flips the invitation state to `accepted`, find-or-creates the
// user for the invited email, and idempotently attaches a membership.
// Mirrors the cascade in the workos-shaped accept handler — duplicated
// rather than extracted because the shape is small and forcing a shared
// helper across `service/` and `modes/localspeakeasy/` would smear the
// boundary between management API and protocol surface.
func (s *InvitationsService) Accept(ctx context.Context, p *gen.AcceptPayload) (*gen.Invitation, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid invitation id")
	}

	queries := repo.New(s.db)
	inv, err := queries.GetInvitation(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "invitation not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load invitation").Log(ctx, s.logger)
	}
	if inv.State == "revoked" {
		return nil, oops.E(oops.CodeConflict, nil, "invitation has been revoked")
	}

	user, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		ID:          defaultuser.DeterministicUserID(inv.Email),
		Email:       inv.Email,
		DisplayName: emailLocalPart(inv.Email),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "provision invited user").Log(ctx, s.logger)
	}
	if _, err := queries.CreateMembership(ctx, repo.CreateMembershipParams{
		ID:             uuid.New(),
		UserID:         user.ID,
		OrganizationID: inv.OrganizationID,
		Role:           sql.NullString{String: "member", Valid: true},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach membership for invited user").Log(ctx, s.logger)
	}

	row, err := queries.AcceptInvitation(ctx, repo.AcceptInvitationParams{ID: id, Ts: time.Now()})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "accept invitation").Log(ctx, s.logger)
	}
	return invitationView(row), nil
}

// =============================================================================
// Helpers
// =============================================================================

func invitationView(inv repo.Invitation) *gen.Invitation {
	view := &gen.Invitation{
		ID:             inv.ID.String(),
		Email:          inv.Email,
		OrganizationID: inv.OrganizationID.String(),
		State:          inv.State,
		Token:          inv.Token,
		InviterUserID:  nil,
		AcceptedAt:     nil,
		RevokedAt:      nil,
		ExpiresAt:      inv.ExpiresAt.UTC().Format(timeFormat),
		CreatedAt:      inv.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:      inv.UpdatedAt.UTC().Format(timeFormat),
	}
	if inv.InviterUserID.Valid {
		s := inv.InviterUserID.UUID.String()
		view.InviterUserID = &s
	}
	if inv.AcceptedAt.Valid {
		s := inv.AcceptedAt.Time.UTC().Format(timeFormat)
		view.AcceptedAt = &s
	}
	if inv.RevokedAt.Valid {
		s := inv.RevokedAt.Time.UTC().Format(timeFormat)
		view.RevokedAt = &s
	}
	return view
}

func newInvitationToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand on a working host doesn't fail; if it does we want
		// a loud panic rather than a silently weak token.
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return hex.EncodeToString(b)
}

func emailLocalPart(email string) string {
	for i, c := range email {
		if c == '@' {
			return email[:i]
		}
	}
	return email
}
