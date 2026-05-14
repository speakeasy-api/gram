package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

	existingUser, err := usersrepo.New(dbtx).GetUserByWorkosID(ctx, conv.ToPGText(user.ID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("get user by WorkOS ID: %w", err)
	}

	gramUserID := conv.Default(existingUser.ID, user.ExternalID)
	if gramUserID == "" {
		logger.WarnContext(ctx, "skipping WorkOS user backfill without local user ID", attr.SlogWorkOSUserID(user.ID))
		return "", false, nil
	}

	if existingUser.WorkosUpdatedAt.Valid && !ShouldProcessEvent(nil, &existingUser.WorkosUpdatedAt.Time, "", updatedAt) {
		return gramUserID, true, nil
	}

	payload := workosUserEventPayload{
		ID:                user.ID,
		ExternalID:        user.ExternalID,
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		ProfilePictureURL: user.ProfilePictureURL,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		DeletedAt:         time.Time{},
	}
	if _, err := usersrepo.New(dbtx).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              gramUserID,
		Email:           user.Email,
		DisplayName:     displayNameFromWorkOSUser(payload),
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
