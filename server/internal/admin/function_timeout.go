package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var ErrOrganizationFunctionTimeoutNotFound = errors.New("organization function timeout target not found")

// OrganizationFunctionTimeoutState is an organization's function timeout, read
// under a metadata row lock.
type OrganizationFunctionTimeoutState struct {
	// OrganizationID is the canonical target.
	OrganizationID string `json:"organization_id"`

	// Name identifies the organization.
	Name string `json:"name"`

	// Slug identifies the organization.
	Slug string `json:"slug"`

	// Seconds is the limit in effect on one function invocation.
	Seconds int `json:"function_timeout_seconds"`

	// IsDefault is true when no staff-set limit exists and Seconds is the
	// platform default.
	IsDefault bool `json:"function_timeout_is_default"`
}

// FunctionTimeoutValue turns a requested limit into the stored value: nil
// restores the platform default, anything else must be within the bounds.
func FunctionTimeoutValue(seconds *int) (pgtype.Int4, error) {
	if seconds == nil {
		return pgtype.Int4{Int32: 0, Valid: false}, nil
	}
	if !constants.IsFunctionTimeoutSeconds(*seconds) {
		return pgtype.Int4{Int32: 0, Valid: false}, oops.E(oops.CodeBadRequest, nil, "function timeout must be between %d and %d seconds", constants.MinFunctionTimeoutSeconds, constants.MaxFunctionTimeoutSeconds)
	}
	return pgtype.Int4{Int32: int32(*seconds), Valid: true}, nil // #nosec G115 -- bounded to [30, 600] above.
}

// LockOrganizationFunctionTimeoutTx reads an organization's function timeout
// and holds its metadata row until the transaction ends.
func LockOrganizationFunctionTimeoutTx(ctx context.Context, tx pgx.Tx, organizationID string) (OrganizationFunctionTimeoutState, error) {
	row, err := repo.New(tx).LockOrganizationFunctionTimeout(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.ID != organizationID) {
		return OrganizationFunctionTimeoutState{}, ErrOrganizationFunctionTimeoutNotFound
	}
	if err != nil {
		return OrganizationFunctionTimeoutState{}, fmt.Errorf("lock organization function timeout: %w", err)
	}
	seconds, isDefault := constants.EffectiveFunctionTimeoutSeconds(conv.FromPGInt4(row.FunctionTimeoutSeconds))
	return OrganizationFunctionTimeoutState{OrganizationID: row.ID, Name: row.Name, Slug: row.Slug, Seconds: seconds, IsDefault: isDefault}, nil
}

// SetOrganizationFunctionTimeoutTx stores a function timeout in the caller's
// transaction, which must already hold the row lock, and returns the state it
// leaves. nil restores the platform default.
func SetOrganizationFunctionTimeoutTx(ctx context.Context, tx pgx.Tx, before OrganizationFunctionTimeoutState, seconds *int) (OrganizationFunctionTimeoutState, error) {
	stored, err := FunctionTimeoutValue(seconds)
	if err != nil {
		return OrganizationFunctionTimeoutState{}, err
	}
	_, err = repo.New(tx).AdminSetOrganizationFunctionTimeout(ctx, repo.AdminSetOrganizationFunctionTimeoutParams{
		ID:                     before.OrganizationID,
		FunctionTimeoutSeconds: stored,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationFunctionTimeoutState{}, ErrOrganizationFunctionTimeoutNotFound
	}
	if err != nil {
		return OrganizationFunctionTimeoutState{}, fmt.Errorf("set organization function timeout: %w", err)
	}
	after := before
	after.Seconds, after.IsDefault = constants.EffectiveFunctionTimeoutSeconds(conv.FromPGInt4(stored))
	return after, nil
}

// SetFunctionTimeout stores the limit on how long one Speakeasy Function
// invocation may run for an organization, or clears it so the platform default
// applies. Like other staff-only settings it is recorded in the server log
// rather than the organization's own audit log, which its admins can read.
func (s *Service) SetFunctionTimeout(ctx context.Context, payload *gen.SetFunctionTimeoutPayload) (*gen.AdminOrganization, error) {
	// The design bounds this too, but generated validation only runs at the
	// HTTP boundary.
	stored, err := FunctionTimeoutValue(payload.FunctionTimeoutSeconds)
	if err != nil {
		return nil, err
	}

	_, err = repo.New(s.db).AdminSetOrganizationFunctionTimeout(ctx, repo.AdminSetOrganizationFunctionTimeoutParams{
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
