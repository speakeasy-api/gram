package main

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

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// directoryUserRecord pairs a directory user snapshot with its parsed
// updated_at timestamp.
type directoryUserRecord struct {
	user      workos.DirectoryUser
	updatedAt time.Time
}

// listDirectoryUserRecords fetches every directory user across all of the
// organization's directories, dropping records with unparseable timestamps.
func listDirectoryUserRecords(ctx context.Context, logger *slog.Logger, client Client, workosOrgID string) ([]directoryUserRecord, error) {
	directories, err := client.ListDirectories(ctx, workosOrgID)
	if err != nil {
		return nil, fmt.Errorf("list WorkOS directories for %s: %w", workosOrgID, err)
	}

	var records []directoryUserRecord
	for _, directory := range directories {
		users, err := client.ListDirectoryUsers(ctx, directory.ID)
		if err != nil {
			return nil, fmt.Errorf("list WorkOS directory users for %s: %w", directory.ID, err)
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
			records = append(records, directoryUserRecord{user: user, updatedAt: updatedAt})
		}
	}

	return records, nil
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
//
// Writes clear the cursor for the same reason. The row's state is now
// snapshot-derived rather than event-derived, and ShouldProcessEvent reads an
// empty cursor as "not yet touched by an event", falling back to the
// workos_updated_at baseline written here — which is the honest guard for a
// row whose provenance is a snapshot.
func backfillDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, organizationID string, rec directoryUserRecord, snapshotAt time.Time) error {
	repo := directoryrepo.New(dbtx)

	existing, err := repo.GetDirectoryUserSyncStateByWorkOSID(ctx, rec.user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get directory user sync state for %q: %w", rec.user.ID, err)
	}
	if err == nil && existing.WorkosUpdatedAt.Valid && rec.updatedAt.Before(existing.WorkosUpdatedAt.Time) {
		return nil
	}

	if rec.user.State != "" && rec.user.State != string(directorysync.Active) {
		return backfillDeactivatedDirectoryUser(ctx, logger, dbtx, organizationID, rec, existing.UserID, snapshotAt)
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
			return fmt.Errorf("get user by directory email: %w", err)
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
	if _, err := repo.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
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
		return fmt.Errorf("upsert directory user %q from WorkOS snapshot: %w", rec.user.ID, err)
	}

	return nil
}

// backfillDeactivatedDirectoryUser applies a non-active snapshot state:
// soft-delete the directory user row and deprovision any live organization
// relationship, mirroring deactivateDirectoryUser in the event pipeline.
// Directory state is authoritative for access here, so only an
// already-deleted relationship short-circuits the deprovision.
func backfillDeactivatedDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, organizationID string, rec directoryUserRecord, storedUserID pgtype.Text, snapshotAt time.Time) error {
	repo := directoryrepo.New(dbtx)

	gramUserID, err := resolveDirectoryGramUser(ctx, dbtx, rec.user.Email, storedUserID)
	if err != nil {
		return err
	}

	if _, err := repo.DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       conv.ToPGTimestamptz(rec.updatedAt),
		WorkosLastEventID:     conv.ToPGText(""),
		WorkosDirectoryUserID: rec.user.ID,
	}); err != nil {
		return fmt.Errorf("deactivate directory user %q from WorkOS snapshot: %w", rec.user.ID, err)
	}

	if gramUserID == "" {
		logger.WarnContext(ctx, "directory user deactivated but no linked Gram user found",
			attr.SlogWorkOSDirectoryUserID(rec.user.ID),
		)
		return nil
	}

	// Lock the relationship before reading it. Without the lock a membership
	// event landing between the guard below and the deprovision write would be
	// silently overwritten: the guard is evaluated in Go and neither
	// MarkWorkOSMembershipDeleted nor MarkRoleAssignmentsDeleted re-checks the
	// timestamp in SQL. Under READ COMMITTED the lock makes the read observe a
	// concurrent writer's committed state, so a re-add newer than the snapshot
	// still blocks the revoke.
	if err := lockOrganizationRelationship(ctx, dbtx, organizationID, gramUserID); err != nil {
		return err
	}

	rel, live, err := liveRelationshipForDeprovision(ctx, dbtx, organizationID, gramUserID, snapshotAt)
	if err != nil {
		return err
	}
	if !live {
		return nil
	}

	// The deprovision is stamped with the directory user's updated_at, not
	// wall-clock time: a membership event that lands between the snapshot
	// fetch and this write (e.g. a re-add) carries a newer WorkOS timestamp
	// and must still apply, while events older than the deactivation stay
	// blocked.
	if err := deprovisionOrganizationAccess(ctx, dbtx, organizationID, gramUserID, rel, rec.updatedAt); err != nil {
		return fmt.Errorf("deprovision organization access for directory user %q: %w", rec.user.ID, err)
	}

	logger.InfoContext(ctx, "deprovisioned organization access for deactivated directory user",
		attr.SlogUserID(gramUserID),
		attr.SlogWorkOSDirectoryUserID(rec.user.ID),
	)
	return nil
}

// resolveDirectoryGramUser resolves the Gram user linked to a directory user:
// email is the canonical linkage (mirroring the upsert path), with the stored
// user_id as a fallback for directory users whose email changed. The stored
// linkage comes from the sync-state row, which includes soft-deleted rows, so
// a previously deactivated row that never got deprovisioned (e.g. an email
// mismatch at event time) can still be repaired.
func resolveDirectoryGramUser(ctx context.Context, dbtx database.DBTX, rawEmail string, storedUserID pgtype.Text) (string, error) {
	if email := conv.NormalizeEmail(rawEmail); email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			return user.ID, nil
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return "", fmt.Errorf("get user by directory email: %w", err)
		}
	}
	if storedUserID.Valid {
		return storedUserID.String, nil
	}
	return "", nil
}

// lockOrganizationRelationship takes a row lock on the user's live
// organization relationship so the deprovision guard and the write that
// follows it cannot straddle a concurrent membership event. A missing or
// already-deleted row locks nothing, which is fine: the caller skips those.
func lockOrganizationRelationship(ctx context.Context, dbtx database.DBTX, organizationID, gramUserID string) error {
	if _, err := dbtx.Exec(ctx, `
SELECT 1
FROM organization_user_relationships
WHERE organization_id = $1
  AND user_id = $2
  AND deleted_at IS NULL
FOR UPDATE`, organizationID, gramUserID); err != nil {
		return fmt.Errorf("lock organization relationship for user %q: %w", gramUserID, err)
	}
	return nil
}

// liveRelationshipForDeprovision returns the organization relationship for
// the user when it is live and eligible for deprovisioning. Relationship
// state written after the snapshot was fetched comes from a concurrent event
// (e.g. a re-add) the snapshot cannot have observed — that state is newer
// truth and must not be revoked. Older relationship timestamps do NOT block
// the deprovision: directory state wins over membership state for anything
// the snapshot did see.
func liveRelationshipForDeprovision(ctx context.Context, dbtx database.DBTX, organizationID, gramUserID string, snapshotAt time.Time) (orgrepo.OrganizationUserRelationship, bool, error) {
	var zero orgrepo.OrganizationUserRelationship
	rel, err := orgrepo.New(dbtx).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(gramUserID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("get organization relationship for directory user: %w", err)
	}
	if rel.Deleted {
		return zero, false, nil
	}
	if rel.WorkosUpdatedAt.Valid && rel.WorkosUpdatedAt.Time.After(snapshotAt) {
		return zero, false, nil
	}
	return rel, true, nil
}

// deprovisionOrganizationAccess marks the organization relationship and the
// user's role assignments deleted. It mirrors the event pipeline's teardown
// routine except for the user-info cache invalidation: this script talks to
// Postgres only, so cached org access lingers until the cache TTL expires.
func deprovisionOrganizationAccess(ctx context.Context, dbtx database.DBTX, organizationID, gramUserID string, rel orgrepo.OrganizationUserRelationship, eventUpdatedAt time.Time) error {
	repo := orgrepo.New(dbtx)
	if err := repo.MarkWorkOSMembershipDeleted(ctx, orgrepo.MarkWorkOSMembershipDeletedParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosUserID:       conv.ToPGTextEmpty(rel.WorkosUserID.String),
		WorkosMembershipID: conv.ToPGTextEmpty(rel.WorkosMembershipID.String),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(eventUpdatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
	}); err != nil {
		return fmt.Errorf("mark organization membership deleted for user %q: %w", gramUserID, err)
	}

	workosUserID := rel.WorkosUserID.String
	if workosUserID == "" {
		user, err := usersrepo.New(dbtx).GetUser(ctx, gramUserID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("get user %q for deprovisioning: %w", gramUserID, err)
		case user.WorkosID.Valid:
			workosUserID = user.WorkosID.String
		}
	}
	if workosUserID != "" {
		if err := repo.MarkRoleAssignmentsDeleted(ctx, orgrepo.MarkRoleAssignmentsDeletedParams{
			OrganizationID:    organizationID,
			WorkosUserID:      workosUserID,
			WorkosUpdatedAt:   conv.ToPGTimestamptz(eventUpdatedAt),
			WorkosLastEventID: conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("mark role assignments deleted for workos user %q: %w", workosUserID, err)
		}
	}

	return nil
}

// validateDirectoryUsers checks that every non-active directory user in the
// snapshot ended up soft-deleted locally and, when a Gram user is linked, that
// the organization relationship was deprovisioned. A relationship whose
// WorkOS timestamp postdates the directory user's deactivation is not a
// failure: a concurrent membership event (e.g. a re-add) legitimately wins
// over the snapshot.
func validateDirectoryUsers(ctx context.Context, db *pgxpool.Pool, organizationID string, records []directoryUserRecord) error {
	repo := directoryrepo.New(db)
	for _, rec := range records {
		if rec.user.State == "" || rec.user.State == string(directorysync.Active) {
			continue
		}

		if _, err := repo.GetDirectoryUserByWorkOSID(ctx, rec.user.ID); err == nil {
			return fmt.Errorf("directory user %q is deactivated in WorkOS but its local row is not soft-deleted", rec.user.ID)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get directory user %q: %w", rec.user.ID, err)
		}

		syncState, err := repo.GetDirectoryUserSyncStateByWorkOSID(ctx, rec.user.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get directory user sync state for %q: %w", rec.user.ID, err)
		}
		gramUserID, err := resolveDirectoryGramUser(ctx, db, rec.user.Email, syncState.UserID)
		if err != nil {
			return err
		}
		if gramUserID == "" {
			continue
		}

		rel, err := orgrepo.New(db).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
			OrganizationID: organizationID,
			UserID:         conv.ToPGText(gramUserID),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return fmt.Errorf("get organization relationship for directory user %q: %w", rec.user.ID, err)
		}
		if rel.Deleted {
			continue
		}
		if rel.WorkosUpdatedAt.Valid && rel.WorkosUpdatedAt.Time.After(rec.updatedAt) {
			continue
		}
		return fmt.Errorf("directory user %q is deactivated in WorkOS but the organization relationship for user %q is still live", rec.user.ID, gramUserID)
	}
	return nil
}

// classifyDirectoryUserChanges predicts what the write phase will do with the
// organization's directory user snapshot. Directory-row outcomes land in the
// first set of counts; relationship deprovisions caused by non-active
// directory users land in the second so the preflight surfaces exactly which
// members would lose access.
func classifyDirectoryUserChanges(ctx context.Context, db *pgxpool.Pool, organizationID string, skipped bool, records []directoryUserRecord) (changeCounts, changeCounts, []changeDetail, error) {
	rowCounts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	deprovisionCounts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	details := make([]changeDetail, 0)
	if skipped {
		return rowCounts, deprovisionCounts, details, nil
	}

	repo := directoryrepo.New(db)
	for _, rec := range records {
		existing, err := repo.GetDirectoryUserSyncStateByWorkOSID(ctx, rec.user.ID)
		rowFound := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return rowCounts, deprovisionCounts, nil, fmt.Errorf("get directory user sync state for %q: %w", rec.user.ID, err)
		}
		if rowFound && existing.WorkosUpdatedAt.Valid && rec.updatedAt.Before(existing.WorkosUpdatedAt.Time) {
			rowCounts.StaleSkip++
			continue
		}

		active := rec.user.State == "" || rec.user.State == string(directorysync.Active)
		if active {
			if rowFound {
				rowCounts.Update++
			} else {
				rowCounts.Create++
			}
			continue
		}

		// Non-active snapshot state: the directory row is soft-deleted and a
		// live relationship is deprovisioned.
		if rowFound {
			rowCounts.Delete++
		} else {
			rowCounts.Noop++
		}

		gramUserID, err := resolveDirectoryGramUser(ctx, db, rec.user.Email, existing.UserID)
		if err != nil {
			return rowCounts, deprovisionCounts, nil, err
		}
		if gramUserID == "" {
			deprovisionCounts.Noop++
			continue
		}
		_, live, err := liveRelationshipForDeprovision(ctx, db, organizationID, gramUserID, time.Now().UTC())
		if err != nil {
			return rowCounts, deprovisionCounts, nil, err
		}
		if !live {
			deprovisionCounts.Noop++
			continue
		}
		deprovisionCounts.Delete++
		details = append(details, changeDetail{
			Entity: "directory_deprovision",
			ID:     rec.user.ID,
			Action: "delete",
			Fields: []fieldChange{
				{Name: "user_id", Before: gramUserID, After: "<deprovisioned>"},
				{Name: "relationship.deleted_at", Before: "<null>", After: "now"},
				{Name: "role_assignments.deleted_at", Before: "<null>", After: "now"},
			},
		})
	}

	return rowCounts, deprovisionCounts, details, nil
}
