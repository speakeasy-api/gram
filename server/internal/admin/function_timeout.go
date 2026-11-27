package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetFunctionTimeout stores the limit on how long one Speakeasy Function
// invocation may run for an organization, or clears it so the platform default
// applies. Like other staff-only settings it is recorded in the server log
// rather than the organization's own audit log, which its admins can read.
func (s *Service) SetFunctionTimeout(ctx context.Context, payload *gen.SetFunctionTimeoutPayload) (*gen.AdminOrganization, error) {
	stored := pgtype.Int4{Int32: 0, Valid: false}
	if payload.FunctionTimeoutSeconds != nil {
		seconds := *payload.FunctionTimeoutSeconds
		// The design bounds this too, but generated validation only runs at
		// the HTTP boundary.
		if !constants.IsFunctionTimeoutSeconds(seconds) {
			return nil, oops.E(oops.CodeBadRequest, nil, "function timeout must be between %d and %d seconds", constants.MinFunctionTimeoutSeconds, constants.MaxFunctionTimeoutSeconds)
		}
		stored = pgtype.Int4{Int32: int32(seconds), Valid: true} // #nosec G115 -- bounded to [30, 600] above.
	}

	_, err := repo.New(s.db).AdminSetOrganizationFunctionTimeout(ctx, repo.AdminSetOrganizationFunctionTimeoutParams{
		ID:                     payload.OrganizationID,
		FunctionTimeoutSeconds: stored,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set function timeout").LogError(ctx, s.logger)
	}

	actor, _, _ := adminActor(ctx)
	if stored.Valid {
		s.logger.InfoContext(ctx, "admin set organization function timeout",
			attr.SlogOrganizationID(payload.OrganizationID),
			attr.SlogUserID(actor.ID),
			attr.SlogValueInt(int(stored.Int32)),
		)
	} else {
		s.logger.InfoContext(ctx, "admin reset organization function timeout to default",
			attr.SlogOrganizationID(payload.OrganizationID),
			attr.SlogUserID(actor.ID),
		)
	}

	return s.readOrganizationAfterWrite(ctx, payload.OrganizationID, "read organization after setting function timeout")
}
