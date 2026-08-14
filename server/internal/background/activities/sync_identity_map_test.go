package activities_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func seedIdentityOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Identity Map Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	return orgID
}

func seedIdentityUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, email string) string {
	t.Helper()

	userID := uuid.NewString()
	_, err := userrepo.New(conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: "identity-map-test",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	return userID
}

func seedIdentityAccount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, userID *string, email string) {
	t.Helper()

	_, err := hooksrepo.New(conn).UpsertUserAccount(ctx, hooksrepo.UpsertUserAccountParams{
		OrganizationID:      orgID,
		Provider:            "anthropic",
		ExternalAccountUuid: uuid.NewString(),
		UserID:              conv.PtrToPGText(userID),
		ExternalOrgID:       conv.PtrToPGText(nil),
		ExternalAccountID:   conv.PtrToPGText(nil),
		Email:               conv.ToPGText(email),
		AccountType:         conv.ToPGText("personal"),
	})
	require.NoError(t, err)
}

// lookupFold reads the map through joinGet — the read contract; a composite-key
// StorageJoin cannot be scanned with SELECT. Missing keys return two empty
// strings.
func lookupFold(t *testing.T, ctx context.Context, chConn clickhouse.Conn, orgID, emailLower string) (string, string) {
	t.Helper()

	var canonicalUserID, canonicalEmail string
	require.NoError(t, chConn.QueryRow(ctx,
		"SELECT joinGet('identity_map', 'canonical_user_id', ?, ?), joinGet('identity_map', 'canonical_email', ?, ?)",
		orgID, emailLower, orgID, emailLower).Scan(&canonicalUserID, &canonicalEmail))
	return canonicalUserID, canonicalEmail
}

func requireFoldsTo(t *testing.T, ctx context.Context, chConn clickhouse.Conn, orgID, emailLower, wantUserID, wantEmail string) {
	t.Helper()

	gotUserID, gotEmail := lookupFold(t, ctx, chConn, orgID, emailLower)
	require.Equal(t, wantUserID, gotUserID)
	require.Equal(t, wantEmail, gotEmail)
}

func requireAbsent(t *testing.T, ctx context.Context, chConn clickhouse.Conn, orgID, emailLower string) {
	t.Helper()

	gotUserID, gotEmail := lookupFold(t, ctx, chConn, orgID, emailLower)
	require.Empty(t, gotUserID, "expected %s to be absent", emailLower)
	require.Empty(t, gotEmail, "expected %s to be absent", emailLower)
}

func TestSyncIdentityMap_FoldRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "sync_identity_map")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	org1 := seedIdentityOrg(t, ctx, conn)
	org2 := seedIdentityOrg(t, ctx, conn)
	suffix := uuid.NewString()[:8]

	// Directory email with mixed case folds to itself; the linked personal
	// account email (mixed case, padded) folds to the same user.
	aliceEmail := "Alice-" + suffix + "@Example.COM"
	aliceLower := "alice-" + suffix + "@example.com"
	alice := seedIdentityUser(t, ctx, conn, org1, aliceEmail)
	alicePersonal := "  Personal-Alice-" + suffix + "@Example.com  "
	alicePersonalLower := "personal-alice-" + suffix + "@example.com"
	seedIdentityAccount(t, ctx, conn, org1, &alice, alicePersonal)

	// A shared account email claimed by two users stays out of the map.
	bob := seedIdentityUser(t, ctx, conn, org1, "bob-"+suffix+"@example.com")
	carol := seedIdentityUser(t, ctx, conn, org1, "carol-"+suffix+"@example.com")
	sharedEmail := "shared-" + suffix + "@example.com"
	seedIdentityAccount(t, ctx, conn, org1, &bob, sharedEmail)
	seedIdentityAccount(t, ctx, conn, org1, &carol, sharedEmail)

	// Case-variant directory duplicates: neither user's email enters the map,
	// and an account owned by one of them is refused because its owner's
	// directory identity is ambiguous.
	dupeLower := "dupe-" + suffix + "@example.com"
	dave := seedIdentityUser(t, ctx, conn, org1, "Dupe-"+suffix+"@example.com")
	seedIdentityUser(t, ctx, conn, org1, "dupe-"+suffix+"@EXAMPLE.com")
	davePersonal := "dave-personal-" + suffix + "@example.com"
	seedIdentityAccount(t, ctx, conn, org1, &dave, davePersonal)

	// Deleted rows: a deleted user, a deleted relationship, and a deleted
	// account link all stay out.
	fixtures := testrepo.New(conn)
	frank := seedIdentityUser(t, ctx, conn, org1, "frank-"+suffix+"@example.com")
	require.NoError(t, fixtures.ForceSoftDeleteUser(ctx, frank))
	gina := seedIdentityUser(t, ctx, conn, org1, "gina-"+suffix+"@example.com")
	require.NoError(t, fixtures.ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
		OrganizationID: org1,
		UserID:         conv.ToPGText(gina),
	}))
	henry := seedIdentityUser(t, ctx, conn, org1, "henry-"+suffix+"@example.com")
	henryPersonal := "henry-personal-" + suffix + "@example.com"
	seedIdentityAccount(t, ctx, conn, org1, &henry, henryPersonal)
	require.NoError(t, fixtures.ForceSoftDeleteUserAccountsByEmail(ctx, testrepo.ForceSoftDeleteUserAccountsByEmailParams{
		OrganizationID: org1,
		EmailLower:     henryPersonal,
	}))

	// An unattributed account (no user_id) resolves to nobody.
	orphanEmail := "orphan-" + suffix + "@example.com"
	seedIdentityAccount(t, ctx, conn, org1, nil, orphanEmail)

	// Directory ownership wins over a linked account claiming the same email.
	ivyEmail := "ivy-" + suffix + "@example.com"
	ivy := seedIdentityUser(t, ctx, conn, org1, ivyEmail)
	jack := seedIdentityUser(t, ctx, conn, org1, "jack-"+suffix+"@example.com")
	seedIdentityAccount(t, ctx, conn, org1, &jack, ivyEmail)

	// The same personal email linked in a different org folds independently.
	org2User := seedIdentityUser(t, ctx, conn, org2, "other-"+suffix+"@example.com")
	seedIdentityAccount(t, ctx, conn, org2, &org2User, alicePersonalLower)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	// A held replacement claim defers the sync instead of interleaving with
	// the holder's statements; releasing it lets the retry proceed.
	require.NoError(t, cacheAdapter.Set(ctx, "identity-map:replace-lock", "held", time.Minute))
	act := activities.NewSyncIdentityMap(logger, conn, chConn, cacheAdapter)
	_, err = act.Do(ctx)
	require.ErrorContains(t, err, "already in progress")
	require.NoError(t, cacheAdapter.Delete(ctx, "identity-map:replace-lock"))
	result, err := act.Do(ctx)
	require.NoError(t, err)
	require.NotZero(t, result.Entries)

	// A second pass is a full rebuild and swap; assertions below hold for the
	// fresh generation, proving idempotency across the staging/live exchange.
	secondResult, err := act.Do(ctx)
	require.NoError(t, err)
	require.Equal(t, result.Entries, secondResult.Entries)

	requireFoldsTo(t, ctx, chConn, org1, aliceLower, alice, aliceLower)
	requireFoldsTo(t, ctx, chConn, org1, alicePersonalLower, alice, aliceLower)
	requireFoldsTo(t, ctx, chConn, org1, ivyEmail, ivy, ivyEmail)
	requireFoldsTo(t, ctx, chConn, org2, alicePersonalLower, org2User, "other-"+suffix+"@example.com")

	for _, absent := range []string{sharedEmail, dupeLower, davePersonal, "frank-" + suffix + "@example.com", "gina-" + suffix + "@example.com", henryPersonal, orphanEmail} {
		requireAbsent(t, ctx, chConn, org1, absent)
	}
}

// TestReplaceIdentityMap_GenerationSwap exercises the telemetry repo's staging
// swap directly, covering the behaviors the fold-rule sync never reaches: the
// duplicate-key rejection (the SQL source provably cannot emit duplicates, so
// only a direct call can trip it), an insert spanning the insert-chunk
// boundary, and a key vanishing with its old generation (deletions propagate
// by omission).
//
// Deliberately not parallel: identity_map is one package-global ClickHouse
// table, and a full-refresh swap here would race the joinGet assertions in
// TestSyncIdentityMap_FoldRules if the two tests overlapped.
func TestReplaceIdentityMap_GenerationSwap(t *testing.T) { //nolint:paralleltest // Swaps the ClickHouse identity_map shared with TestSyncIdentityMap_FoldRules.
	ctx := t.Context()

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	repo := telemetryrepo.New(chConn)
	org := "org-" + uuid.NewString()[:8]
	seedEmail := "seed-" + uuid.NewString()[:8] + "@example.com"

	require.NoError(t, repo.ReplaceIdentityMap(ctx, []telemetryrepo.IdentityMapEntry{
		{OrgID: org, EmailLower: seedEmail, CanonicalUserID: "user-a", CanonicalEmail: seedEmail},
	}))
	requireFoldsTo(t, ctx, chConn, org, seedEmail, "user-a", seedEmail)

	// A duplicate (org, email) key is rejected before either table is touched,
	// so the previous complete generation keeps serving.
	err = repo.ReplaceIdentityMap(ctx, []telemetryrepo.IdentityMapEntry{
		{OrgID: org, EmailLower: seedEmail, CanonicalUserID: "user-a", CanonicalEmail: seedEmail},
		{OrgID: org, EmailLower: seedEmail, CanonicalUserID: "user-b", CanonicalEmail: seedEmail},
	})
	require.ErrorContains(t, err, "duplicate identity map key")
	requireFoldsTo(t, ctx, chConn, org, seedEmail, "user-a", seedEmail)

	// One entry beyond the insert chunk size, derived from the constant so the
	// boundary stays exercised if the chunk size ever changes:
	// rows land on both sides of the chunk boundary, and the seed key is gone
	// with the generation that carried it.
	boundary := telemetryrepo.IdentityMapInsertChunk + 1
	entries := make([]telemetryrepo.IdentityMapEntry, 0, boundary)
	for i := range boundary {
		email := fmt.Sprintf("bulk-%d-%s@example.com", i, org)
		entries = append(entries, telemetryrepo.IdentityMapEntry{
			OrgID:           org,
			EmailLower:      email,
			CanonicalUserID: fmt.Sprintf("user-%d", i),
			CanonicalEmail:  email,
		})
	}
	require.NoError(t, repo.ReplaceIdentityMap(ctx, entries))

	for _, i := range []int{0, telemetryrepo.IdentityMapInsertChunk - 1, telemetryrepo.IdentityMapInsertChunk} {
		email := fmt.Sprintf("bulk-%d-%s@example.com", i, org)
		requireFoldsTo(t, ctx, chConn, org, email, fmt.Sprintf("user-%d", i), email)
	}
	requireAbsent(t, ctx, chConn, org, seedEmail)
}
