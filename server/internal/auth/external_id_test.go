package auth_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/users"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// TestExternalID_Priority2_WorkOSExternalID verifies that when no existing
// Gram user matches by email but WorkOS has an external_id (e.g. set by the
// Registry backfill), Gram uses that as the new user's primary key.
func TestExternalID_Priority2_WorkOSExternalID(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01EXTID_PRIORITY1"
	externalID := uuid.New().String() // simulates Registry-assigned ID

	userInfo := &MockUserInfo{
		UserID:     workosUserID,
		Email:      "priority1@example.com",
		ExternalID: externalID,
	}

	ctx, instance := newTestAuthService(t, userInfo)

	idpUser, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)
	require.Equal(t, externalID, idpUser.ExternalID, "mock should return external_id")

	returnedID, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)
	require.Equal(t, externalID, returnedID, "should use WorkOS external_id as Gram user ID")

	// Verify DB row uses the external_id as primary key.
	usersQueries := usersRepo.New(instance.conn)
	dbUser, err := usersQueries.GetUser(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, externalID, dbUser.ID)
	require.Equal(t, userInfo.Email, dbUser.Email)

	// Verify workos_id was stored.
	require.True(t, dbUser.WorkosID.Valid, "workos_id should be populated")
	require.Equal(t, workosUserID, dbUser.WorkosID.String)
}

// TestExternalID_EmailMatchWinsOverExternalID verifies that when a user with
// the same email already exists in Gram, their existing ID is preserved — even
// if WorkOS has a different external_id. User IDs are immutable once assigned.
func TestExternalID_EmailMatchWinsOverExternalID(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01EXTID_VS_EMAIL"
	externalID := uuid.New().String()
	legacyUUID := uuid.New().String()

	userInfo := &MockUserInfo{
		UserID:     workosUserID,
		Email:      "existing@example.com",
		ExternalID: externalID,
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// Pre-seed a user with a different ID but same email (legacy data).
	usersQueries := usersRepo.New(instance.conn)
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          legacyUUID,
		Email:       userInfo.Email,
		DisplayName: "Legacy User",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	idpUser, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	returnedID, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)

	// Existing email match always wins — IDs are immutable.
	require.Equal(t, legacyUUID, returnedID, "existing user ID must be preserved over external_id")
}

// TestExternalID_Priority1_EmailMatch verifies that when a user with the same
// email already exists, Gram reuses their existing ID regardless of whether
// WorkOS has an external_id.
func TestExternalID_Priority1_EmailMatch(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01EMAIL_MATCH"
	legacyUUID := uuid.New().String()

	userInfo := &MockUserInfo{
		UserID:     workosUserID,
		Email:      "email-match@example.com",
		ExternalID: "", // no external_id
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// Pre-seed user with legacy UUID.
	usersQueries := usersRepo.New(instance.conn)
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          legacyUUID,
		Email:       userInfo.Email,
		DisplayName: "Legacy User",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	idpUser, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)
	require.Empty(t, idpUser.ExternalID, "no external_id set")

	returnedID, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)
	require.Equal(t, legacyUUID, returnedID, "should reuse existing legacy UUID")

	// Verify workos_id was backfilled.
	dbUser, err := usersQueries.GetUserByEmail(ctx, userInfo.Email)
	require.NoError(t, err)
	require.Equal(t, legacyUUID, dbUser.ID)
	require.True(t, dbUser.WorkosID.Valid)
	require.Equal(t, workosUserID, dbUser.WorkosID.String)
}

// TestExternalID_Priority3_DeterministicUUIDv5 verifies that a brand-new user
// with no external_id and no existing email match gets a deterministic UUIDv5
// derived from their WorkOS user ID.
func TestExternalID_Priority3_DeterministicUUIDv5(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01BRAND_NEW_UUIDV5"

	userInfo := &MockUserInfo{
		UserID:     workosUserID,
		Email:      "brand-new-v5@example.com",
		ExternalID: "", // no external_id
	}

	ctx, instance := newTestAuthService(t, userInfo)

	idpUser, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)

	returnedID, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser)
	require.NoError(t, err)

	// Should be a deterministic UUIDv5.
	expectedID := users.UserIDFromWorkOSID(workosUserID)
	require.Equal(t, expectedID, returnedID, "new user should get UUIDv5 derived from WorkOS ID")

	// Verify it's a valid UUIDv5.
	parsed, err := uuid.Parse(returnedID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(5), parsed.Version())

	// Verify DB row.
	usersQueries := usersRepo.New(instance.conn)
	dbUser, err := usersQueries.GetUser(ctx, expectedID)
	require.NoError(t, err)
	require.Equal(t, expectedID, dbUser.ID)
	require.Equal(t, userInfo.Email, dbUser.Email)

	// Verify workos_id was stored.
	require.True(t, dbUser.WorkosID.Valid)
	require.Equal(t, workosUserID, dbUser.WorkosID.String)
}

// TestExternalID_Deterministic verifies the UUIDv5 derivation is stable —
// logging in twice with the same WorkOS ID produces the same Gram user ID.
func TestExternalID_Deterministic(t *testing.T) {
	t.Parallel()

	workosUserID := "user_01DETERMINISTIC"

	userInfo := &MockUserInfo{
		UserID:     workosUserID,
		Email:      "deterministic@example.com",
		ExternalID: "",
	}

	ctx, instance := newTestAuthService(t, userInfo)

	// First login.
	idpUser1, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)
	id1, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser1)
	require.NoError(t, err)

	// Second login — same user, should resolve to email match (priority 2)
	// which returns the same ID that was created by priority 3 on first login.
	idpUser2, err := instance.sessionManager.ExchangeCodeForTokens(ctx, "test-code")
	require.NoError(t, err)
	id2, err := instance.sessionManager.UpsertUserFromIDP(ctx, idpUser2)
	require.NoError(t, err)

	require.Equal(t, id1, id2, "repeated logins must produce the same user ID")
}
