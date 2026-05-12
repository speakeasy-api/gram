package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type WorkOSClient interface {
	GetOrganization(ctx context.Context, orgID string) (*workos.Organization, error)
	ListOrganizations(ctx context.Context) ([]workos.Organization, error)
	ListRoles(ctx context.Context, orgID string) ([]workos.Role, error)
	ListOrgMemberships(ctx context.Context, orgID string) ([]workos.Member, error)
	ListGlobalRoles(ctx context.Context) ([]workos.Role, error)
	ListEvents(ctx context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error)
}

type BackfillWorkOSOrganizationParams struct {
	WorkOSOrganizationID string `json:"workos_organization_id"`
}

type BackfillWorkOSOrganization struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	workos WorkOSClient
}

type backfillWorkOSMember struct {
	member    workos.Member
	updatedAt time.Time
}

func NewBackfillWorkOSOrganization(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient) *BackfillWorkOSOrganization {
	return &BackfillWorkOSOrganization{
		logger: logger.With(attr.SlogComponent("backfill_workos_organization")),
		db:     db,
		workos: workosClient,
	}
}

func (b *BackfillWorkOSOrganization) Do(ctx context.Context, params BackfillWorkOSOrganizationParams) error {
	logger := b.logger.With(attr.SlogWorkOSOrganizationID(params.WorkOSOrganizationID))

	workosOrg, err := b.workos.GetOrganization(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get WorkOS organization").Log(ctx, logger)
	}
	orgUpdatedAt, err := parseWorkOSTime(workosOrg.UpdatedAt)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse WorkOS organization updated_at").Log(ctx, logger)
	}

	roles, err := b.workos.ListRoles(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS organization roles").Log(ctx, logger)
	}

	members, err := b.workos.ListOrgMemberships(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS organization memberships").Log(ctx, logger)
	}
	parsedMembers := make([]backfillWorkOSMember, 0, len(members))
	for _, member := range members {
		updatedAt, err := parseWorkOSTime(member.UpdatedAt)
		if err != nil {
			logger.WarnContext(ctx, "skipping WorkOS membership with invalid updated_at",
				attr.SlogWorkOSUserID(member.UserID),
				attr.SlogError(err),
			)
			continue
		}
		parsedMembers = append(parsedMembers, backfillWorkOSMember{member: member, updatedAt: updatedAt})
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	orgQueries := orgrepo.New(tx)
	org, err := orgQueries.GetOrganizationByWorkosID(ctx, conv.ToPGText(params.WorkOSOrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if workosOrg.ExternalID == "" {
			logger.DebugContext(ctx, "skipping WorkOS organization backfill for unlinked organization with no external_id")
			return nil
		}
		org.ID = workosOrg.ExternalID
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", params.WorkOSOrganizationID, err)
	}

	org, err = backfillOrganizationMetadata(ctx, orgQueries, org, *workosOrg, orgUpdatedAt)
	if err != nil {
		return err
	}
	if err := backfillOrganizationRoles(ctx, logger, tx, org.ID, roles); err != nil {
		return err
	}
	for _, member := range parsedMembers {
		if err := backfillOrganizationMember(ctx, tx, org.ID, member); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func backfillOrganizationMetadata(ctx context.Context, repo *orgrepo.Queries, org orgrepo.OrganizationMetadatum, workosOrg workos.Organization, updatedAt time.Time) (orgrepo.OrganizationMetadatum, error) {
	var lastEventID *string
	if org.WorkosLastEventID.Valid {
		lastEventID = &org.WorkosLastEventID.String
	}
	var rowUpdatedAt *time.Time
	if org.WorkosUpdatedAt.Valid {
		rowUpdatedAt = &org.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(lastEventID, rowUpdatedAt, "", updatedAt) {
		return org, nil
	}

	updatedOrg, err := repo.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                org.ID,
		Name:              workosOrg.Name,
		Slug:              conv.ToSlug(workosOrg.Name),
		WorkosID:          conv.ToPGText(workosOrg.ID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID: conv.ToPGText(""),
	})
	if err != nil {
		return orgrepo.OrganizationMetadatum{}, fmt.Errorf("upsert organization %q from WorkOS snapshot: %w", workosOrg.ID, err)
	}

	return updatedOrg, nil
}

func backfillOrganizationRoles(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, organizationID string, roles []workos.Role) error {
	repo := accessrepo.New(dbtx)
	snapshotSlugs := make(map[string]time.Time)

	for _, role := range roles {
		if !isWorkOSOrganizationRole(role) {
			continue
		}
		createdAt, err := parseWorkOSTime(role.CreatedAt)
		if err != nil {
			return fmt.Errorf("parse role %q created_at: %w", role.Slug, err)
		}
		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return fmt.Errorf("parse role %q updated_at: %w", role.Slug, err)
		}
		snapshotSlugs[role.Slug] = updatedAt

		existing, err := repo.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
			OrganizationID: organizationID,
			WorkosSlug:     role.Slug,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get organization role %q: %w", role.Slug, err)
		}

		var lastEventID *string
		if existing.WorkosLastEventID.Valid {
			lastEventID = &existing.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, "", updatedAt) {
			continue
		}

		if err := repo.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
			OrganizationID:    organizationID,
			WorkosSlug:        role.Slug,
			WorkosName:        role.Name,
			WorkosDescription: conv.ToPGTextEmpty(role.Description),
			WorkosCreatedAt:   conv.ToPGTimestamptz(createdAt),
			WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
			WorkosLastEventID: conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("upsert organization role %q: %w", role.Slug, err)
		}
	}

	localRoles, err := repo.ListOrganizationRolesByOrg(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list organization roles for %q: %w", organizationID, err)
	}
	for _, localRole := range localRoles {
		if _, ok := snapshotSlugs[localRole.WorkosSlug]; ok {
			continue
		}

		var lastEventID *string
		if localRole.WorkosLastEventID.Valid {
			lastEventID = &localRole.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if localRole.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &localRole.WorkosUpdatedAt.Time
		}
		deletedAt := time.Now().UTC()
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, "", deletedAt) {
			continue
		}

		if _, err := repo.MarkOrganizationRoleDeleted(ctx, accessrepo.MarkOrganizationRoleDeletedParams{
			OrganizationID:    organizationID,
			WorkosSlug:        localRole.WorkosSlug,
			WorkosDeletedAt:   conv.ToPGTimestamptz(deletedAt),
			WorkosLastEventID: conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("mark organization role %q deleted: %w", localRole.WorkosSlug, err)
		}
		if _, err := repo.DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
			OrganizationID: organizationID,
			PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, localRole.WorkosSlug),
		}); err != nil {
			return fmt.Errorf("delete grants for organization role %q: %w", localRole.WorkosSlug, err)
		}
		logger.DebugContext(ctx, "soft-deleted WorkOS organization role missing from snapshot", attr.SlogAccessRoleSlug(localRole.WorkosSlug))
	}

	return nil
}

func backfillOrganizationMember(ctx context.Context, dbtx pgx.Tx, organizationID string, parsed backfillWorkOSMember) error {
	member := parsed.member
	orgQueries := orgrepo.New(dbtx)

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(member.UserID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get user by workos id %q: %w", member.UserID, err)
	}

	cursor, err := latestMembershipCursor(ctx, orgQueries, organizationID, gramUserID, member.UserID)
	if err != nil {
		return err
	}
	if !ShouldProcessEvent(cursor.lastEventID, cursor.updatedAt, "", parsed.updatedAt) {
		return nil
	}

	if gramUserID != "" {
		if err := orgQueries.UpsertOrganizationUserRelationshipFromWorkOS(ctx, orgrepo.UpsertOrganizationUserRelationshipFromWorkOSParams{
			OrganizationID:     organizationID,
			UserID:             gramUserID,
			WorkosMembershipID: conv.ToPGText(member.ID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(parsed.updatedAt),
			WorkosLastEventID:  conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("upsert organization membership %q: %w", member.ID, err)
		}
	}

	roleSlugs := []string{}
	if member.RoleSlug != "" {
		roleSlugs = []string{member.RoleSlug}
	}
	if err := orgQueries.SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       member.UserID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosMembershipID: conv.ToPGText(member.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(parsed.updatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
		WorkosRoleSlugs:    roleSlugs,
	}); err != nil {
		return fmt.Errorf("sync organization role assignments for membership %q: %w", member.ID, err)
	}

	return nil
}

type membershipCursor struct {
	lastEventID *string
	updatedAt   *time.Time
}

// latestMembershipCursor returns the newest local WorkOS state
// for a membership before applying a snapshot. Membership backfill writes two
// local shapes: organization_user_relationships when the WorkOS user is linked
// to a Gram user, and organization_role_assignments even when the user is still
// unknown locally. Both can be updated by event processing, so the snapshot must
// compare against the freshest cursor/timestamp from both tables before it
// overwrites either table.
func latestMembershipCursor(ctx context.Context, repo *orgrepo.Queries, organizationID, gramUserID, workosUserID string) (membershipCursor, error) {
	var cursor membershipCursor

	assignments, err := repo.ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	if err != nil {
		return membershipCursor{}, fmt.Errorf("list organization role assignments for WorkOS user %q: %w", workosUserID, err)
	}
	for _, assignment := range assignments {
		moveMembershipCursor(&cursor, assignment.WorkosLastEventID, assignment.WorkosUpdatedAt)
	}

	// we don't have a relationship for a user with no gram user ID, so return the membership cursor
	if gramUserID == "" {
		return cursor, nil
	}

	existing, err := repo.GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         gramUserID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cursor, nil
	case err != nil:
		return membershipCursor{}, fmt.Errorf("get organization membership for user %q: %w", gramUserID, err)
	}

	moveMembershipCursor(&cursor, existing.WorkosLastEventID, existing.WorkosUpdatedAt)

	return cursor, nil
}

func parseWorkOSTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse WorkOS timestamp %q: %w", raw, err)
	}
	return t, nil
}

func isWorkOSOrganizationRole(role workos.Role) bool {
	if role.Type != "" {
		return role.Type == "OrganizationRole"
	}
	return strings.HasPrefix(role.Slug, "org-")
}

func moveMembershipCursor(cursor *membershipCursor, eventID pgtype.Text, updatedAt pgtype.Timestamptz) {
	if eventID.Valid {
		if cursor.lastEventID == nil || eventID.String > *cursor.lastEventID {
			cursor.lastEventID = &eventID.String
		}
	}

	if updatedAt.Valid {
		if cursor.updatedAt == nil || updatedAt.Time.After(*cursor.updatedAt) {
			cursor.updatedAt = &updatedAt.Time
		}
	}
}
