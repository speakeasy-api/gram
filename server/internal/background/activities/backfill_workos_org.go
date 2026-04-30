package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	workospkg "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// WorkOSBackfillClient is the subset of workos.Client used by the backfill activity.
type WorkOSBackfillClient interface {
	ListRoles(ctx context.Context, orgID string) ([]workospkg.Role, error)
	ListOrgMemberships(ctx context.Context, orgID string) ([]workospkg.Member, error)
}

// BackfillWorkOSOrg snapshots the current WorkOS state for a single org into the local DB.
// Unlike event processing, it does not advance the event cursor. Eventual consistency is
// maintained via workos_updated_at: existing rows are only overwritten when the snapshot
// is newer.
type BackfillWorkOSOrg struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	workosClient WorkOSBackfillClient
}

func NewBackfillWorkOSOrg(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSBackfillClient) *BackfillWorkOSOrg {
	return &BackfillWorkOSOrg{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
	}
}

type BackfillWorkOSOrgParams struct {
	WorkOSOrgID string `json:"workos_org_id"`
}

func (b *BackfillWorkOSOrg) Do(ctx context.Context, params BackfillWorkOSOrgParams) error {
	workosOrgID := params.WorkOSOrgID

	roles, err := b.workosClient.ListRoles(ctx, workosOrgID)
	if err != nil {
		return fmt.Errorf("list roles for workos org %q: %w", workosOrgID, err)
	}

	members, err := b.workosClient.ListOrgMemberships(ctx, workosOrgID)
	if err != nil {
		return fmt.Errorf("list memberships for workos org %q: %w", workosOrgID, err)
	}

	dbtx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	organizationID, err := orgrepo.New(dbtx).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(workosOrgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get organization by workos id %q: %w", workosOrgID, err)
	}

	for _, role := range roles {
		if role.Type == workosRoleTypeEnvironment {
			if err := accessrepo.New(dbtx).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
				WorkosSlug:        role.Slug,
				WorkosName:        role.Name,
				WorkosDescription: conv.ToPGTextEmpty(role.Description),
				WorkosCreatedAt:   conv.ToPGTimestamptzEmpty(role.CreatedAt),
				WorkosUpdatedAt:   conv.ToPGTimestamptzEmpty(role.UpdatedAt),
				WorkosLastEventID: conv.ToPGText(""),
			}); err != nil {
				return fmt.Errorf("upsert global role %q: %w", role.Slug, err)
			}
		} else {
			if err := accessrepo.New(dbtx).UpsertRole(ctx, accessrepo.UpsertRoleParams{
				OrganizationID:    organizationID,
				WorkosSlug:        role.Slug,
				WorkosName:        role.Name,
				WorkosDescription: conv.ToPGTextEmpty(role.Description),
				WorkosCreatedAt:   conv.ToPGTimestamptzEmpty(role.CreatedAt),
				WorkosUpdatedAt:   conv.ToPGTimestamptzEmpty(role.UpdatedAt),
				WorkosLastEventID: conv.ToPGText(""),
			}); err != nil {
				return fmt.Errorf("upsert org role %q: %w", role.Slug, err)
			}
		}
	}

	for _, member := range members {
		updatedAt, err := time.Parse(time.RFC3339, member.UpdatedAt)
		if err != nil {
			b.logger.WarnContext(ctx, "backfill: skipping member with unparseable updated_at",
				"workos_org_id", workosOrgID,
				"workos_user_id", member.UserID,
				"updated_at", member.UpdatedAt,
				"error", err,
			)
			continue
		}

		gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(member.UserID))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// proceed with null user_id
		case err != nil:
			return fmt.Errorf("get user by workos id %q: %w", member.UserID, err)
		default:
			if err := orgrepo.New(dbtx).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
				OrganizationID:     organizationID,
				UserID:             gramUserID,
				WorkosMembershipID: conv.ToPGText(member.ID),
			}); err != nil {
				return fmt.Errorf("upsert membership for workos user %q: %w", member.UserID, err)
			}
		}

		roleSlugs := dedupeStrings(member.RoleSlugs)
		userIDParam := conv.ToPGTextEmpty(gramUserID)

		if err := orgrepo.New(dbtx).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
			OrganizationID:     organizationID,
			WorkosUserID:       member.UserID,
			UserID:             userIDParam,
			WorkosRoleSlugs:    roleSlugs,
			WorkosMembershipID: conv.ToPGText(member.ID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(updatedAt),
			WorkosLastEventID:  conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("sync role assignments for workos user %q: %w", member.UserID, err)
		}
	}

	return dbtx.Commit(ctx)
}
