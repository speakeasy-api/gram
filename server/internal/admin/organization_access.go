package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var ErrOrganizationAccessNotFound = errors.New("organization access target not found")

// OrganizationAccessState is the identity and access state pinned by an
// organization metadata row lock.
type OrganizationAccessState struct {
	OrganizationID string
	Name           string
	Slug           string
	DisabledAt     *time.Time
}

// LockOrganizationAccessTx pins the canonical organization metadata row and
// returns its identity and current access state.
func LockOrganizationAccessTx(ctx context.Context, tx pgx.Tx, organizationID string) (OrganizationAccessState, error) {
	row, err := repo.New(tx).LockOrganizationAccess(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.ID != organizationID) {
		return OrganizationAccessState{}, ErrOrganizationAccessNotFound
	}
	if err != nil {
		return OrganizationAccessState{}, fmt.Errorf("lock organization access: %w", err)
	}
	return organizationAccessState(row.ID, row.Name, row.Slug, row.DisabledAt), nil
}

// SetOrganizationAccessTx changes access and writes its tenant audit event in
// the caller's transaction. The metadata row lock serializes dashboard and
// proposal writes; the SQL update retains the original disable timestamp and
// leaves the WorkOS event cursor untouched.
func SetOrganizationAccessTx(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, enabled bool, actor urn.Principal, actorDisplayName *string) (OrganizationAccessState, error) {
	before, err := LockOrganizationAccessTx(ctx, tx, organizationID)
	if err != nil {
		return OrganizationAccessState{}, err
	}
	updated, err := repo.New(tx).SetOrganizationAccess(ctx, repo.SetOrganizationAccessParams{
		ID: organizationID, Enabled: enabled, ExpectedDisabledAt: organizationAccessDisabledAt(before.DisabledAt),
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && updated.ID != organizationID) {
		return OrganizationAccessState{}, ErrOrganizationAccessNotFound
	}
	if err != nil {
		return OrganizationAccessState{}, fmt.Errorf("set organization access: %w", err)
	}
	after := organizationAccessState(updated.ID, updated.Name, updated.Slug, updated.DisabledAt)
	if accessEnabled(before) == enabled {
		return after, nil
	}
	if enabled {
		err = auditLogger.LogOrganizationEnabled(ctx, tx, audit.LogOrganizationAccessEvent{
			OrganizationID: organizationID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil,
			OrganizationName: after.Name, OrganizationSlug: after.Slug,
			OrganizationSnapshotBefore: &audit.OrganizationAccessSnapshot{DisabledAt: before.DisabledAt},
			OrganizationSnapshotAfter:  &audit.OrganizationAccessSnapshot{DisabledAt: after.DisabledAt},
		})
	} else {
		err = auditLogger.LogOrganizationDisabled(ctx, tx, audit.LogOrganizationAccessEvent{
			OrganizationID: organizationID, Actor: actor, ActorDisplayName: actorDisplayName, ActorSlug: nil,
			OrganizationName: after.Name, OrganizationSlug: after.Slug,
			OrganizationSnapshotBefore: &audit.OrganizationAccessSnapshot{DisabledAt: before.DisabledAt},
			OrganizationSnapshotAfter:  &audit.OrganizationAccessSnapshot{DisabledAt: after.DisabledAt},
		})
	}
	if err != nil {
		return OrganizationAccessState{}, fmt.Errorf("log organization access change: %w", err)
	}
	return after, nil
}

func accessEnabled(state OrganizationAccessState) bool {
	return state.DisabledAt == nil
}

func organizationAccessDisabledAt(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return pgtype.Timestamptz{Time: *value, InfinityModifier: pgtype.Finite, Valid: true}
}

func organizationAccessState(id, name, slug string, disabledAt pgtype.Timestamptz) OrganizationAccessState {
	state := OrganizationAccessState{OrganizationID: id, Name: name, Slug: slug, DisabledAt: nil}
	if disabledAt.Valid {
		state.DisabledAt = &disabledAt.Time
	}
	return state
}
