package auth_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/users"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestUpsertUserFromIDP_ReusesExistingIDOnEmailMatch(t *testing.T) {
	t.Parallel()

	// The mock WorkOS server returns userInfo.UserID as the WorkOS user ID
	// (resp.User.ID). We set it to a WorkOS-format ID to simulate production.
	workosUserID := "user_01WORKOS_FORMAT_ID"
	legacyUUID := uuid.New().String()

	userInfo := &MockUserInfo{
		UserID: workosUserID,
		Email:  "migration-test@example.com",
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// Step 1: Pre-seed a user with a UUID-format ID (simulating legacy Speakeasy IDP data).
	usersQueries := usersRepo.New(instance.conn)
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          legacyUUID,
		Email:       userInfo.Email,
		DisplayName: "Legacy User",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	// Step 2: Call ExchangeCodeForTokens + UpsertUserFromIDP — this is the
	// production login path. The mock server returns workosUserID as the sub.
	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)
	require.Equal(t, workosUserID, idpUser.Sub, "mock should return WorkOS-format ID")

	returnedID, err := instance.identityResolver.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)

	// Step 3: Assert the returned ID is the legacy UUID, NOT the WorkOS ID.
	require.Equal(t, legacyUUID, returnedID, "should reuse existing UUID-format ID, not WorkOS ID")

	// Step 4: Verify the user row still has the legacy UUID as primary key.
	dbUser, err := usersQueries.GetUserByEmail(ctx, userInfo.Email)
	require.NoError(t, err)
	require.Equal(t, legacyUUID, dbUser.ID)

	// Step 5: Verify workos_id was backfilled.
	require.True(t, dbUser.WorkosID.Valid, "workos_id should be populated after login")
	require.Equal(t, workosUserID, dbUser.WorkosID.String)
}

func TestUpsertUserFromIDP_NewUserGetsDeterministicUUIDv5(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01BRAND_NEW_USER"

	userInfo := &MockUserInfo{
		UserID: workosUserID,
		Email:  "brand-new@example.com",
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// No pre-existing user — first login ever.
	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	returnedID, err := instance.identityResolver.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)

	// New users get a deterministic UUIDv5 derived from the WorkOS user ID.
	expectedID := users.UserIDFromWorkOSID(workosUserID)
	require.Equal(t, expectedID, returnedID)

	usersQueries := usersRepo.New(instance.conn)
	dbUser, err := usersQueries.GetUser(ctx, expectedID)
	require.NoError(t, err)
	require.Equal(t, expectedID, dbUser.ID)
	require.Equal(t, userInfo.Email, dbUser.Email)
}

func TestUpsertUserFromIDP_PreservesAdminStatus(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01ADMIN_MIGRATION"
	legacyUUID := uuid.New().String()

	userInfo := &MockUserInfo{
		UserID: workosUserID,
		Email:  "admin-migrate@example.com",
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// Pre-seed an admin user with legacy UUID.
	usersQueries := usersRepo.New(instance.conn)
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          legacyUUID,
		Email:       userInfo.Email,
		DisplayName: "Admin User",
		PhotoUrl:    pgtype.Text{},
		Admin:       true,
	})
	require.NoError(t, err)

	idpUser, err := instance.identityResolver.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	returnedID, err := instance.identityResolver.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)
	require.Equal(t, legacyUUID, returnedID)

	// Admin status must be preserved across the IDP migration.
	dbUser, err := usersQueries.GetUser(ctx, legacyUUID)
	require.NoError(t, err)
	require.True(t, dbUser.Admin, "admin flag must survive the ID migration")
}
