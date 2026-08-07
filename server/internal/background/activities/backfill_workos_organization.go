package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/directorysync"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
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
	ListDirectories(ctx context.Context, organizationID string) ([]workos.Directory, error)
	ListDirectoryUsers(ctx context.Context, directoryID string) ([]workos.DirectoryUser, error)
	UpdateUserExternalID(ctx context.Context, workosUserID, externalID string) error
	UpdateOrganizationExternalID(ctx context.Context, workosOrgID, externalID string) error
}

type BackfillWorkOSOrganizationParams struct {
	WorkOSOrganizationID string `json:"workos_organization_id"`
}

type BackfillWorkOSOrganization struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	workos WorkOSClient
	// userInfoCache mirrors the identity resolver's cached user info (same
	// key shape and suffix as the resolver wiring in cmd/gram) so that
	// snapshot-driven deprovisioning can invalidate a user's cached org
	// memberships instead of waiting out the cache TTL.
	userInfoCache cache.TypedCacheObject[sessions.CachedUserInfo]
}

type backfillWorkOSMember struct {
	member    workos.Member
	updatedAt time.Time
}

// backfillWorkOSDirectoryUser pairs a directory user snapshot with its parsed
// updated_at timestamp.
type backfillWorkOSDirectoryUser struct {
	user      workos.DirectoryUser
	updatedAt time.Time
}

func NewBackfillWorkOSOrganization(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient, cacheAdapter cache.Cache) *BackfillWorkOSOrganization {
	return &BackfillWorkOSOrganization{
		logger:        logger.With(attr.SlogComponent("backfill_workos_organization")),
		db:            db,
		workos:        workosClient,
		userInfoCache: cache.NewTypedObjectCache[sessions.CachedUserInfo](logger.With(attr.SlogCacheNamespace("user_info")), cacheAdapter, cache.SuffixNone),
	}
}

func (b *BackfillWorkOSOrganization) Do(ctx context.Context, params BackfillWorkOSOrganizationParams) error {
	logger := b.logger.With(attr.SlogWorkOSOrganizationID(params.WorkOSOrganizationID))

	workosOrg, err := b.workos.GetOrganization(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get WorkOS organization").LogError(ctx, logger)
	}
	orgUpdatedAt, err := parseWorkOSTime(workosOrg.UpdatedAt)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse WorkOS organization updated_at").LogError(ctx, logger)
	}

	roles, err := b.workos.ListRoles(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS organization roles").LogError(ctx, logger)
	}

	members, err := b.workos.ListOrgMemberships(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS organization memberships").LogError(ctx, logger)
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

	directories, err := b.workos.ListDirectories(ctx, params.WorkOSOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS directories").LogError(ctx, logger)
	}
	var directoryUsers []backfillWorkOSDirectoryUser
	for _, directory := range directories {
		users, err := b.workos.ListDirectoryUsers(ctx, directory.ID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list WorkOS directory users").LogError(ctx, logger)
		}
		for _, user := range users {
			updatedAt, err := parseWorkOSTime(user.UpdatedAt)
			if err != nil {
				logger.WarnContext(ctx, "skipping WorkOS directory user with invalid updated_at",
					attr.SlogWorkOSDirectoryUserID(user.ID),
					attr.SlogError(err),
				)
				continue
			}
			directoryUsers = append(directoryUsers, backfillWorkOSDirectoryUser{user: user, updatedAt: updatedAt})
		}
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

	var effects []postCommitEffects
	for _, directoryUser := range directoryUsers {
		userEffects, err := backfillDirectoryUser(ctx, logger, tx, org.ID, directoryUser)
		if err != nil {
			return err
		}
		effects = append(effects, userEffects)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	for _, userEffects := range effects {
		runPostCommitEffects(ctx, logger, b.workos, b.userInfoCache, userEffects)
	}

	return nil
}

// backfillDirectoryUser reconciles one directory user snapshot, mirroring
// what the dsync.user.* event handlers do: an active user upserts (and may
// restore a soft-deleted row), a non-active user is soft-deleted and has any
// live organization access deprovisioned.
//
// Unlike the event handlers, guards compare timestamps only and ignore
// workos_last_event_id. The event path historically advanced the cursor while
// ignoring the user's state, so a cursor-based guard would skip exactly the
// rows this backfill exists to repair. The snapshot is a fresh read of
// current WorkOS state, so the staleness that cursors defend against does not
// apply.
func backfillDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, organizationID string, rec backfillWorkOSDirectoryUser) (postCommitEffects, error) {
	var none postCommitEffects
	repo := workosrepo.New(dbtx)

	existing, err := repo.GetDirectoryUserSyncStateByWorkOSID(ctx, rec.user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, fmt.Errorf("get directory user sync state for %q: %w", rec.user.ID, err)
	}
	if err == nil && existing.WorkosUpdatedAt.Valid && rec.updatedAt.Before(existing.WorkosUpdatedAt.Time) {
		return none, nil
	}

	if rec.user.State != "" && rec.user.State != string(directorysync.Active) {
		return backfillDeactivatedDirectoryUser(ctx, logger, dbtx, organizationID, rec)
	}

	var userID pgtype.Text
	email := conv.NormalizeEmail(rec.user.Email)
	if email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			userID = conv.ToPGText(user.ID)
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return none, fmt.Errorf("get user by directory email: %w", err)
		}
	}

	attributes := rec.user.CustomAttributes
	if len(attributes) == 0 || string(attributes) == "null" {
		attributes = []byte("{}")
	}
	createdAt, err := parseWorkOSTime(rec.user.CreatedAt)
	if err != nil {
		createdAt = rec.updatedAt
	}
	if _, err := repo.UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                userID,
		WorkosDirectoryUserID: rec.user.ID,
		Email:                 conv.ToPGText(email),
		Attributes:            attributes,
		// Only an explicitly active snapshot state may resurrect a
		// soft-deleted row, mirroring the event upsert path.
		RestoreDeleted:    rec.user.State == string(directorysync.Active),
		WorkosCreatedAt:   conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(rec.updatedAt),
		WorkosLastEventID: conv.ToPGText(""),
	}); err != nil {
		return none, fmt.Errorf("upsert directory user %q from WorkOS snapshot: %w", rec.user.ID, err)
	}

	return none, nil
}

// backfillDeactivatedDirectoryUser applies a non-active snapshot state:
// soft-delete the directory user row and deprovision any live organization
// relationship, mirroring deactivateDirectoryUser. Directory state is
// authoritative for access here, and a fresh snapshot cannot be stale, so
// only an already-deleted relationship short-circuits the deprovision.
func backfillDeactivatedDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, organizationID string, rec backfillWorkOSDirectoryUser) (postCommitEffects, error) {
	var none postCommitEffects
	repo := workosrepo.New(dbtx)

	// Resolve the linked Gram user before soft-deleting the directory row:
	// email is the canonical linkage (mirroring the upsert path), with the
	// stored user_id as a fallback for directory users whose email changed.
	var gramUserID string
	if email := conv.NormalizeEmail(rec.user.Email); email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			gramUserID = user.ID
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return none, fmt.Errorf("get user by directory email: %w", err)
		}
	}
	if gramUserID == "" {
		directoryUser, err := repo.GetDirectoryUserByWorkOSID(ctx, rec.user.ID)
		switch {
		case err == nil && directoryUser.UserID.Valid:
			gramUserID = directoryUser.UserID.String
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return none, fmt.Errorf("get directory user by WorkOS ID: %w", err)
		}
	}

	if _, err := repo.DeleteDirectoryUserByWorkOSID(ctx, workosrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       conv.ToPGTimestamptz(rec.updatedAt),
		WorkosLastEventID:     conv.ToPGText(""),
		WorkosDirectoryUserID: rec.user.ID,
	}); err != nil {
		return none, fmt.Errorf("deactivate directory user %q from WorkOS snapshot: %w", rec.user.ID, err)
	}

	if gramUserID == "" {
		logger.WarnContext(ctx, "directory user deactivated but no linked Gram user found",
			attr.SlogWorkOSDirectoryUserID(rec.user.ID),
		)
		return none, nil
	}

	rel, err := orgrepo.New(dbtx).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(gramUserID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil
	case err != nil:
		return none, fmt.Errorf("get organization relationship for directory user: %w", err)
	}
	if rel.Deleted {
		return none, nil
	}

	// The deprovision is stamped with the snapshot time rather than the
	// user's updated_at so older queued events cannot overwrite it, while
	// genuinely newer events (e.g. a later re-add) still apply.
	deprovisionedAt := time.Now().UTC()
	effects, err := deprovisionOrganizationAccess(ctx, dbtx, deprovisionOrganizationAccessParams{
		organizationID:     organizationID,
		gramUserID:         gramUserID,
		workosUserID:       rel.WorkosUserID.String,
		workosMembershipID: rel.WorkosMembershipID.String,
		eventID:            "",
		eventUpdatedAt:     deprovisionedAt,
	})
	if err != nil {
		return none, fmt.Errorf("deprovision organization access for directory user %q: %w", rec.user.ID, err)
	}

	logger.InfoContext(ctx, "deprovisioned organization access for deactivated directory user",
		attr.SlogUserID(gramUserID),
		attr.SlogWorkOSDirectoryUserID(rec.user.ID),
	)
	return effects, nil
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
		if role.Type != "OrganizationRole" {
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

		if _, err := repo.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
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

	if err := orgQueries.UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosUserID:       conv.ToPGText(member.UserID),
		WorkosMembershipID: conv.ToPGText(member.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(parsed.updatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
	}); err != nil {
		return fmt.Errorf("upsert organization membership %q: %w", member.ID, err)
	}

	if err := orgQueries.SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       member.UserID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosMembershipID: conv.ToPGText(member.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(parsed.updatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
		WorkosRoleSlugs:    member.RoleSlugs,
	}); err != nil {
		return fmt.Errorf("sync organization role assignments for membership %q: %w", member.ID, err)
	}

	return nil
}

type membershipCursor struct {
	lastEventID *string
	updatedAt   *time.Time
}

// latestMembershipCursor returns the newest local WorkOS state for a membership
// before applying a snapshot. Membership backfill writes two local shapes:
// organization_user_relationships when the WorkOS user is linked to a Gram
// user, and organization_role_assignments even when the user is still unknown
// locally. Both can be updated by event processing, so the snapshot must compare
// against the freshest cursor/timestamp from both tables before it overwrites
// either table.
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

	// No relationship row exists when the WorkOS user is not linked to a Gram user.
	if gramUserID == "" {
		return cursor, nil
	}

	existing, err := repo.GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(gramUserID),
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

// moveMembershipCursor tracks per-field upper bounds rather than a coherent
// row state. Backfill only uses the cursor as a conservative skip signal, so any
// newer event ID or updated timestamp from either local membership shape should
// block an older snapshot from overwriting local state.
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
