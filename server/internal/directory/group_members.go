package directory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/workos/workos-go/v6/pkg/directorysync"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// GroupMembersReconcileResult counts the membership rows a reconcile changed.
type GroupMembersReconcileResult struct {
	// Opened is the number of memberships opened for listed members that had
	// none.
	Opened int

	// Closed is the number of open memberships closed because their user is
	// no longer listed in the group.
	Closed int
}

// ReconcileGroupMembers makes a group's open memberships match a complete
// WorkOS listing of its members, taken at listedAt.
//
// Membership events only describe changes, so members who joined before the
// organization's event cursor never get a row from them. This fills those
// gaps. listed must be the full listing: a partial one would close the
// memberships of real members and revoke the roles their group grants.
//
// Events stay the source of truth. A membership an event changed at or after
// listedAt is left alone, and a deleted group or directory user is never
// restored. Only active listed users gain a membership; an inactive one keeps
// whatever membership it has. The caller runs this in a transaction, and the
// group row is locked so membership events for the group wait for it.
func ReconcileGroupMembers(ctx context.Context, dbtx database.DBTX, organizationID, workosGroupID string, listed []workos.DirectoryUser, listedAt time.Time) (GroupMembersReconcileResult, error) {
	var result GroupMembersReconcileResult
	queries := repo.New(dbtx)

	groupID, err := queries.GetDirectoryGroupIDByWorkOSID(ctx, workosGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("lock directory group: %w", err)
	}

	openRows, err := queries.ListOpenDirectoryGroupMemberships(ctx, groupID)
	if err != nil {
		return result, fmt.Errorf("list open group memberships: %w", err)
	}
	open := make(map[string]time.Time, len(openRows))
	for _, row := range openRows {
		open[row.WorkosDirectoryUserID] = row.WorkosCreatedAt.Time
	}

	listedIDs := make(map[string]struct{}, len(listed))
	for _, user := range listed {
		listedIDs[user.ID] = struct{}{}
		if user.State != string(directorysync.Active) {
			continue
		}
		if _, ok := open[user.ID]; ok {
			continue
		}

		opened, err := openListedMembership(ctx, dbtx, organizationID, groupID, workosGroupID, user, listedAt)
		if err != nil {
			return result, err
		}
		if opened {
			result.Opened++
		}
	}

	for workosUserID, changedAt := range open {
		if _, ok := listedIDs[workosUserID]; ok {
			continue
		}
		if !changedAt.Before(listedAt) {
			continue
		}

		closed, err := queries.CloseDirectoryUserGroupMembershipByWorkOSIDs(ctx, repo.CloseDirectoryUserGroupMembershipByWorkOSIDsParams{
			WorkosCreatedAt:        conv.ToPGTimestamptz(listedAt),
			WorkosDirectoryUserID:  workosUserID,
			WorkosDirectoryGroupID: workosGroupID,
		})
		if err != nil {
			return result, fmt.Errorf("close unlisted group membership: %w", err)
		}
		result.Closed += int(closed)
	}

	return result, nil
}

// openListedMembership opens a membership for a listed member, first saving
// the directory user when no row exists for it. It reports false when an
// event already changed the membership at or after listedAt, or when the
// directory user was deleted.
func openListedMembership(ctx context.Context, dbtx database.DBTX, organizationID string, groupID uuid.UUID, workosGroupID string, user workos.DirectoryUser, listedAt time.Time) (bool, error) {
	queries := repo.New(dbtx)

	latest, err := queries.GetLatestDirectoryUserGroupMembershipByWorkOSIDs(ctx, repo.GetLatestDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  user.ID,
		WorkosDirectoryGroupID: workosGroupID,
	})
	switch {
	case err == nil && !latest.WorkosCreatedAt.Time.Before(listedAt):
		return false, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("get latest group membership: %w", err)
	}

	if err := insertListedUser(ctx, dbtx, organizationID, user, listedAt); err != nil {
		return false, err
	}

	directoryUserID, err := queries.GetDirectoryUserIDByWorkOSID(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get directory user ID: %w", err)
	}

	if _, err := queries.OpenDirectoryUserGroupMembership(ctx, repo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  user.ID,
		WorkosDirectoryGroupID: workosGroupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(listedAt),
	}); err != nil {
		return false, fmt.Errorf("open listed group membership: %w", err)
	}
	return true, nil
}

// insertListedUser saves a listed directory user that has no row yet, linked
// to the Speakeasy user with the same email when one exists.
func insertListedUser(ctx context.Context, dbtx database.DBTX, organizationID string, user workos.DirectoryUser, listedAt time.Time) error {
	var userID pgtype.Text
	email := conv.NormalizeEmail(user.Email)
	if email != "" {
		existing, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			userID = conv.ToPGText(existing.ID)
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("get user by directory email: %w", err)
		}
	}

	attributes := []byte(user.CustomAttributes)
	if len(attributes) == 0 || string(attributes) == "null" {
		attributes = []byte("{}")
	}
	if err := repo.New(dbtx).InsertListedDirectoryUser(ctx, repo.InsertListedDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                userID,
		WorkosDirectoryUserID: user.ID,
		Email:                 conv.ToPGTextEmpty(email),
		Attributes:            attributes,
		WorkosCreatedAt:       conv.ToPGTimestamptz(listedTimeOr(user.CreatedAt, listedAt)),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(listedTimeOr(user.UpdatedAt, listedAt)),
	}); err != nil {
		return fmt.Errorf("save listed directory user: %w", err)
	}
	return nil
}

// listedTimeOr parses a WorkOS RFC 3339 timestamp, falling back when it is
// empty or malformed.
func listedTimeOr(value string, fallback time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fallback
	}
	return parsed
}
