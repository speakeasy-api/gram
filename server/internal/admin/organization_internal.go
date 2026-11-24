package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// setOrganizationInternal stores the staff-set internal label and returns the
// organization's slug. The label is internal staff metadata, so it is recorded
// in the server log rather than the organization's own audit log, which the
// organization's admins can read.
func (s *Service) setOrganizationInternal(ctx context.Context, id string, internal bool) (string, error) {
	slug, err := repo.New(s.db).AdminSetOrganizationInternal(ctx, repo.AdminSetOrganizationInternalParams{
		ID:       id,
		Internal: internal,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "set organization internal flag").LogError(ctx, s.logger)
	}

	actor, _, _ := adminActor(ctx)
	message := "admin marked organization internal"
	if !internal {
		message = "admin unmarked organization internal"
	}
	s.logger.InfoContext(ctx, message,
		attr.SlogOrganizationID(id),
		attr.SlogUserID(actor.ID),
	)

	return slug, nil
}
