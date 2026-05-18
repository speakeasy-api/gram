package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func backfillWorkOSUser(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, user workos.User) (string, bool, error) {
	createdAt, err := parseWorkOSTime(user.CreatedAt)
	if err != nil {
		return "", false, fmt.Errorf("parse user %q created_at: %w", user.ID, err)
	}
	updatedAt, err := parseWorkOSTime(user.UpdatedAt)
	if err != nil {
		return "", false, fmt.Errorf("parse user %q updated_at: %w", user.ID, err)
	}

	existingUser, found, err := getUserByWorkOSID(ctx, dbtx, user.ID)
	if err != nil {
		return "", false, err
	}

	gramUserID := user.ExternalID
	if found {
		gramUserID = existingUser.ID
	}
	if gramUserID == "" {
		logger.WarnContext(ctx, "skipping WorkOS user backfill without local user ID", attr.SlogWorkOSUserID(user.ID))
		return "", false, nil
	}

	if found && existingUser.WorkosUpdatedAt.Valid && !shouldProcessEvent(nil, &existingUser.WorkosUpdatedAt.Time, "", updatedAt) {
		return gramUserID, true, nil
	}

	if _, err := usersrepo.New(dbtx).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              gramUserID,
		Email:           user.Email,
		DisplayName:     displayNameFromWorkOSUser(user),
		PhotoUrl:        conv.ToPGTextEmpty(user.ProfilePictureURL),
		WorkosID:        conv.ToPGText(user.ID),
		WorkosCreatedAt: conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(updatedAt),
	}); err != nil {
		return "", false, fmt.Errorf("upsert synced user: %w", err)
	}

	if user.ExternalID == "" {
		logger.WarnContext(ctx, "WorkOS user missing external ID during backfill", attr.SlogWorkOSUserID(user.ID), attr.SlogUserID(gramUserID))
	}

	return gramUserID, true, nil
}

func getUserByWorkOSID(ctx context.Context, dbtx pgx.Tx, workosUserID string) (usersrepo.User, bool, error) {
	users, err := usersrepo.New(dbtx).GetUsersByWorkosIDs(ctx, []string{workosUserID})
	var zero usersrepo.User
	switch {
	case err != nil:
		return zero, false, fmt.Errorf("get user by WorkOS ID: %w", err)
	case len(users) == 0:
		return zero, false, nil
	default:
		return users[0], true, nil
	}
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func findUserByWorkOSID(ctx context.Context, db queryRower, workosUserID string) (usersrepo.User, bool, error) {
	var user usersrepo.User
	err := db.QueryRow(ctx, `
SELECT id, email, display_name, photo_url, admin, last_login, workos_id, workos_created_at, workos_updated_at, workos_deleted_at, deleted_at, created_at, updated_at
FROM users
WHERE workos_id = $1
LIMIT 1`, workosUserID).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.PhotoUrl,
		&user.Admin,
		&user.LastLogin,
		&user.WorkosID,
		&user.WorkosCreatedAt,
		&user.WorkosUpdatedAt,
		&user.WorkosDeletedAt,
		&user.DeletedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	var zero usersrepo.User
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("get user by WorkOS ID: %w", err)
	default:
		return user, true, nil
	}
}

func displayNameFromWorkOSUser(user workos.User) string {
	displayName := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if displayName != "" {
		return displayName
	}
	return user.Email
}
