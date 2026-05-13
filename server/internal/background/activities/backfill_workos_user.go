package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/users"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func backfillWorkOSOrganizationUsers(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient, workosOrgID string) error {
	users, err := workosClient.ListOrgUsers(ctx, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS organization users").Log(ctx, logger)
	}

	for _, user := range users {
		if err := backfillWorkOSUser(ctx, logger, db, workosClient, user); err != nil {
			return oops.E(oops.CodeUnexpected, err, "backfill WorkOS organization user").Log(ctx, logger.With(attr.SlogWorkOSUserID(user.ID)))
		}
	}

	return nil
}

func backfillWorkOSUser(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient, user workos.User) error {
	createdAt, err := parseWorkOSTime(user.CreatedAt)
	if err != nil {
		return fmt.Errorf("parse user %q created_at: %w", user.ID, err)
	}
	updatedAt, err := parseWorkOSTime(user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("parse user %q updated_at: %w", user.ID, err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

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
	existingUsers, err := usersrepo.New(tx).GetUsersByWorkosIDs(ctx, []string{user.ID})
	if err != nil {
		return fmt.Errorf("get users by WorkOS ID: %w", err)
	}

	gramUserID := user.ExternalID
	if len(existingUsers) > 0 {
		existing := existingUsers[0]
		if gramUserID == "" {
			gramUserID = existing.ID
		}
		if existing.WorkosUpdatedAt.Valid && !ShouldProcessEvent(nil, &existing.WorkosUpdatedAt.Time, "", updatedAt) {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit skipped user backfill transaction: %w", err)
			}
			return nil
		}
	}
	if gramUserID == "" {
		gramUserID = users.UserIDFromWorkOSID(user.ID)
	}

	if _, err := usersrepo.New(tx).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              gramUserID,
		Email:           user.Email,
		DisplayName:     displayNameFromWorkOSUser(payload),
		PhotoUrl:        conv.ToPGTextEmpty(user.ProfilePictureURL),
		WorkosID:        conv.ToPGText(user.ID),
		WorkosCreatedAt: conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(updatedAt),
	}); err != nil {
		return fmt.Errorf("upsert synced user: %w", err)
	}

	orgQueries := orgrepo.New(tx)
	if err := orgQueries.LinkRoleAssignmentsToUser(ctx, orgrepo.LinkRoleAssignmentsToUserParams{
		UserID:       conv.ToPGText(gramUserID),
		WorkosUserID: user.ID,
	}); err != nil {
		return fmt.Errorf("link role assignments to user: %w", err)
	}
	if err := orgQueries.LinkRelationshipsToUser(ctx, orgrepo.LinkRelationshipsToUserParams{
		UserID:       conv.ToPGText(gramUserID),
		WorkosUserID: conv.ToPGText(user.ID),
	}); err != nil {
		return fmt.Errorf("link organization relationships to user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	if user.ExternalID == "" {
		if err := workosClient.UpdateUserExternalID(ctx, user.ID, gramUserID); err != nil {
			logger.WarnContext(ctx, "failed to set WorkOS user external ID", attr.SlogWorkOSUserID(user.ID), attr.SlogError(err))
		}
	}

	return nil
}
