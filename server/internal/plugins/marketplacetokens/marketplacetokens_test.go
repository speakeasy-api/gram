package marketplacetokens_test

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// tokenColumns are the bare column names of every stored form of a token.
var tokenColumns = []string{"marketplace_token_hash", "marketplace_token_encrypted", "marketplace_token_org_encrypted", "marketplace_token"}

// tokenQueriesPath is the only queries file allowed to write a token column,
// relative to this package.
const tokenQueriesPath = "../marketplacetoken_queries.sql"

func TestMarketplaceTokenQueriesFenceEveryCopyWrite(t *testing.T) {
	t.Parallel()

	queries, err := os.ReadFile(tokenQueriesPath)
	require.NoError(t, err)

	orgencryptiontest.RequireFencedWrites(t, string(queries), tokenColumns...)
}

func TestMintedTokensAreURLSafeAndUnique(t *testing.T) {
	t.Parallel()

	first, err := marketplacetokens.Mint()
	require.NoError(t, err)
	second, err := marketplacetokens.Mint()
	require.NoError(t, err)

	require.Regexp(t, `^[A-Za-z0-9_-]{43}$`, first, "the marketplace proxy's handler precheck pins this shape")
	require.NotEqual(t, first, second)
	require.Len(t, marketplacetokens.Hash(first), 64)
	require.NotEqual(t, marketplacetokens.Hash(first), marketplacetokens.Hash(second))
}

func TestEnsureInPlatformKeyStoresTheHashAndOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)

	require.NoError(t, ti.tokens.Ensure(t.Context(), ti.conn, conn))

	row := ti.stored(t, conn.ID)
	require.True(t, row.MarketplaceTokenHash.Valid)
	require.True(t, row.MarketplaceTokenEncrypted.Valid)
	require.False(t, row.MarketplaceTokenOrgEncrypted.Valid)
	require.False(t, row.MarketplaceToken.Valid, "the token is never stored in plaintext")
	require.Equal(t, conn.UpdatedAt, row.UpdatedAt, "minting leaves the last-published timestamp to the upsert")

	token, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(row))
	require.NoError(t, err)
	require.Equal(t, marketplacetokens.Hash(token), row.MarketplaceTokenHash.String)
}

func TestEnsureKeepsAnExistingToken(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	require.NoError(t, ti.tokens.Ensure(t.Context(), ti.conn, conn))
	first := ti.stored(t, conn.ID)

	require.NoError(t, ti.tokens.Ensure(t.Context(), ti.conn, first))
	require.Equal(t, first.MarketplaceTokenHash, ti.stored(t, conn.ID).MarketplaceTokenHash, "a republish never rotates the token")
}

func TestEnsureKeepsAPlaintextTokenTheBackfillHasNotMoved(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, conn, "legacy-token", true)

	require.NoError(t, ti.tokens.Ensure(t.Context(), ti.conn, ti.stored(t, conn.ID)))

	row := ti.stored(t, conn.ID)
	require.False(t, row.MarketplaceTokenHash.Valid, "a device already holds the plaintext token's URL, so no new token is minted")
	token, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(row))
	require.NoError(t, err)
	require.Equal(t, "legacy-token", token)
}

func TestEnsureRefusesAConnectionWithoutItsOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	conn.OrganizationID = pgtype.Text{String: "", Valid: false}

	require.ErrorIs(t, ti.tokens.Ensure(t.Context(), ti.conn, conn), marketplacetokens.ErrUnattributed)
}

func TestStoreRefusesAnotherOrganizationsConnection(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	other := createOrganization(t, ti.conn)

	err := ti.tokens.Store(t.Context(), ti.conn, conn.ID, other, "token-for-another-org")
	require.ErrorIs(t, err, marketplacetokens.ErrNotStored)
	require.False(t, ti.stored(t, conn.ID).MarketplaceTokenHash.Valid)
}

func TestStoreRefusesAnEmptyToken(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)

	require.ErrorIs(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, ""), marketplacetokens.ErrEmptyToken)
	require.False(t, ti.stored(t, conn.ID).MarketplaceTokenHash.Valid)
}

func TestStoreInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	conn := ti.createConnection(t, ti.orgID)

	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "dual-write-token"))

	row := ti.stored(t, conn.ID)
	require.True(t, row.MarketplaceTokenEncrypted.Valid)
	require.True(t, row.MarketplaceTokenOrgEncrypted.Valid)

	plaintext, err := ti.platform.Decrypt(row.MarketplaceTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "dual-write-token", plaintext, "the platform-key copy stays usable for a rollback")

	orgOnly := marketplacetokens.FromConnection(row)
	orgOnly.Copies.Platform = pgtype.Text{String: "", Valid: false}
	token, err := ti.tokens.Open(t.Context(), ti.conn, orgOnly)
	require.NoError(t, err)
	require.Equal(t, "dual-write-token", token, "dual_write reads the organization-key copy")
}

func TestStoreAfterFinalizeWritesOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	conn := ti.createConnection(t, ti.orgID)

	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "org-kms-token"))

	row := ti.stored(t, conn.ID)
	require.False(t, row.MarketplaceTokenEncrypted.Valid)
	require.True(t, row.MarketplaceTokenOrgEncrypted.Valid)

	token, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(row))
	require.NoError(t, err)
	require.Equal(t, "org-kms-token", token)
}

func TestStoreSealsAgainWhenTheTransitionMovesAfterTheDecision(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)

	// Cache platform_key in this process, then enroll through another
	// process's transition state so the cached decision is stale.
	_, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.Stored{
		OrganizationID: conv.ToPGText(ti.orgID),
		Hash:           conv.ToPGText("a-hash-that-never-opens"),
		Copies:         orgencryption.ErasedCopies(),
		Legacy:         pgtype.Text{String: "", Valid: false},
	})
	require.True(t, orgencryption.IsUnreadable(err))
	_, err = ti.otherTransitions(t).Enroll(t.Context(), ti.orgID, orgencryption.UseCaseMarketplaceTokens, orgencryption.SystemActor())
	require.NoError(t, err)

	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	require.NoError(t, ti.tokens.Store(t.Context(), tx, conn.ID, ti.orgID, "moved-token"))
	require.NoError(t, tx.Commit(t.Context()))

	row := ti.stored(t, conn.ID)
	require.True(t, row.MarketplaceTokenEncrypted.Valid)
	require.True(t, row.MarketplaceTokenOrgEncrypted.Valid, "the fenced write sealed again under dual_write")
}

func TestStoreWithAStaleDualWriteDecisionWritesNoPlatformCopyAfterFinalize(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.enroll(t)

	// Another process caches dual_write, then this one finalizes.
	stale := ti.otherProcessTokens(t)
	_, err := stale.Open(t.Context(), ti.conn, marketplacetokens.Stored{
		OrganizationID: conv.ToPGText(ti.orgID),
		Hash:           conv.ToPGText("a-hash-that-never-opens"),
		Copies:         orgencryption.ErasedCopies(),
		Legacy:         pgtype.Text{String: "", Valid: false},
	})
	require.Error(t, err)
	ti.finalize(t)

	require.NoError(t, stale.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "stale-token"))

	row := ti.stored(t, conn.ID)
	require.False(t, row.MarketplaceTokenEncrypted.Valid, "a writer that decided under dual_write never writes a platform-key copy after finalize")
	require.True(t, row.MarketplaceTokenOrgEncrypted.Valid)
}

func TestOpenFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	conn := ti.createConnection(t, ti.orgID)
	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "suspended-token"))
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(ti.stored(t, conn.ID)))
	require.Error(t, err, "nothing falls back to the platform-key copy")
	require.True(t, marketplacetokens.CustomerKeyProblem(err), "a suspended key is the customer's to fix: %v", err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, marketplacetokens.OpenError(t.Context(), testenv.NewLogger(t), err, ti.orgID), &shareable)
	require.Equal(t, oops.CodeFailedPrecondition, shareable.Code, "412 keeps the device agent off its outage backoff")
}

func TestOpenErrorIsUnexpectedForAnythingElse(t *testing.T) {
	t.Parallel()

	var shareable *oops.ShareableError
	require.ErrorAs(t, marketplacetokens.OpenError(t.Context(), testenv.NewLogger(t), errors.New("database unavailable"), "org_test"), &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
}

func TestOpenRefusesACopyThatDoesNotMatchTheHash(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	platformCopy, err := ti.platform.Encrypt([]byte("the-real-token"))
	require.NoError(t, err)

	require.NoError(t, repo.New(ti.conn).PlantMarketplaceTokenFixture(t.Context(), repo.PlantMarketplaceTokenFixtureParams{
		OrganizationID: conv.ToPGText(ti.orgID),
		LegacyToken:    pgtype.Text{String: "", Valid: false},
		TokenHash:      conv.ToPGText(marketplacetokens.Hash("a-different-token")),
		PlatformCopy:   conv.ToPGText(platformCopy),
		OrgCopy:        pgtype.Text{String: "", Valid: false},
		ID:             conn.ID,
	}))

	_, err = ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(ti.stored(t, conn.ID)))
	require.True(t, orgencryption.IsUnreadable(err), "a URL built from this copy would not resolve: %v", err)
}

func TestOpenReportsAConnectionWithoutAToken(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)

	_, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(conn))
	require.ErrorIs(t, err, orgencryption.ErrMissing)
}

func TestMoveLegacyHashesSealsAndClearsThePlaintextToken(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, conn, "legacy-token", true)
	before := ti.stored(t, conn.ID)

	moved, err := ti.tokens.MoveLegacy(t.Context(), ti.conn, conn.ID, ti.orgID, "legacy-token")
	require.NoError(t, err)
	require.True(t, moved)

	row := ti.stored(t, conn.ID)
	require.False(t, row.MarketplaceToken.Valid, "the plaintext column is cleared")
	require.Equal(t, marketplacetokens.Hash("legacy-token"), row.MarketplaceTokenHash.String, "the URL devices already hold keeps resolving")
	require.Equal(t, before.UpdatedAt, row.UpdatedAt, "the backfill changes nothing else about the connection")

	token, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(row))
	require.NoError(t, err)
	require.Equal(t, "legacy-token", token)

	again, err := ti.tokens.MoveLegacy(t.Context(), ti.conn, conn.ID, ti.orgID, "legacy-token")
	require.NoError(t, err)
	require.False(t, again, "a second run finds nothing to move")
}

func TestMoveLegacyInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, conn, "legacy-token", true)
	ti.enroll(t)

	moved, err := ti.tokens.MoveLegacy(t.Context(), ti.conn, conn.ID, ti.orgID, "legacy-token")
	require.NoError(t, err)
	require.True(t, moved)

	row := ti.stored(t, conn.ID)
	require.True(t, row.MarketplaceTokenEncrypted.Valid)
	require.True(t, row.MarketplaceTokenOrgEncrypted.Valid)
}

func TestMoveLegacyLeavesAReplacedTokenAlone(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, conn, "current-token", true)

	moved, err := ti.tokens.MoveLegacy(t.Context(), ti.conn, conn.ID, ti.orgID, "token-read-before-a-rotation")
	require.NoError(t, err)
	require.False(t, moved)
	require.Equal(t, "current-token", ti.stored(t, conn.ID).MarketplaceToken.String)
}

func TestBackfillVerifyAndFinalizeMoveTokensToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "backfilled-token"))
	stored := ti.stored(t, conn.ID)

	store := marketplacetokens.NewCopyStore(ti.conn)
	counts, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, counts.LastWrittenAt.IsZero())
	require.Equal(t, orgencryption.CopyCounts{Waiting: 1, Unreadable: 0, Unattributed: 0, PlatformCopies: 1, Values: 1, LastWrittenAt: counts.LastWrittenAt}, counts)

	ti.enroll(t)
	result := ti.run(t)
	require.Equal(t, int64(1), result.Converted)

	sealed := ti.stored(t, conn.ID)
	require.True(t, sealed.MarketplaceTokenOrgEncrypted.Valid)
	require.Equal(t, stored.UpdatedAt, sealed.UpdatedAt, "the backfill changes nothing else about the connection")

	ti.finalize(t)

	finalized := ti.stored(t, conn.ID)
	require.False(t, finalized.MarketplaceTokenEncrypted.Valid)
	require.Equal(t, stored.MarketplaceTokenHash, finalized.MarketplaceTokenHash, "the proxy keeps resolving the same URL")

	token, err := ti.tokens.Open(t.Context(), ti.conn, marketplacetokens.FromConnection(finalized))
	require.NoError(t, err)
	require.Equal(t, "backfilled-token", token)
}

func TestCopyStoreCountsUnmovedTokensAsUnattributed(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	attributed := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, attributed, "legacy-attributed", true)
	unattributed := ti.createConnection(t, ti.orgID)
	ti.plantLegacy(t, unattributed, "legacy-unattributed", false)

	counts, err := marketplacetokens.NewCopyStore(ti.conn).Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, int64(2), counts.Unattributed, "finalize waits until the marketplace token backfill moves both")
}

func TestCopyStoreSealWaitingLandsOnlyOnTheCopyItRead(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "old-token"))

	store := marketplacetokens.NewCopyStore(ti.conn)
	waiting, err := store.ListWaiting(t.Context(), ti.orgID, "", 10)
	require.NoError(t, err)
	require.Len(t, waiting, 1)

	waiting[0].Platform = "a-platform-copy-the-row-no-longer-holds"
	landed, err := store.SealWaiting(t.Context(), ti.orgID, 0, waiting[0], "organization-copy")
	require.NoError(t, err)
	require.False(t, landed)
	require.False(t, ti.stored(t, conn.ID).MarketplaceTokenOrgEncrypted.Valid)
}

func TestCopyStoreClearPlatformCopiesIsFencedOnGeneration(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	conn := ti.createConnection(t, ti.orgID)
	ti.enroll(t)
	require.NoError(t, ti.tokens.Store(t.Context(), ti.conn, conn.ID, ti.orgID, "fenced-token"))

	cleared, err := marketplacetokens.NewCopyStore(ti.conn).ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
	require.NoError(t, err)
	require.Zero(t, cleared, "a clear decided under a stale generation changes nothing")
	require.True(t, ti.stored(t, conn.ID).MarketplaceTokenEncrypted.Valid)
}

func TestCopyStoreReportsStoredTokens(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	none := pgtype.Text{String: "", Valid: false}
	plant := func(conn repo.PluginGithubConnection, organizationID, legacy, platform, org pgtype.Text) repo.PluginGithubConnection {
		require.NoError(t, repo.New(ti.conn).PlantMarketplaceTokenFixture(t.Context(), repo.PlantMarketplaceTokenFixtureParams{
			OrganizationID: organizationID, LegacyToken: legacy, TokenHash: none, PlatformCopy: platform, OrgCopy: org, ID: conn.ID,
		}))
		return ti.stored(t, conn.ID)
	}

	organizationID := conv.ToPGText(ti.orgID)
	platformOnly := plant(ti.createConnection(t, ti.orgID), organizationID, none, conv.ToPGText("platform-copy"), none)
	orgOnly := plant(ti.createConnection(t, ti.orgID), organizationID, none, none, conv.ToPGText("org-copy"))
	// A plaintext token is not a organizations value, and an unattributed copy belongs
	// to no organization.
	plant(ti.createConnection(t, ti.orgID), organizationID, conv.ToPGText("legacy-token"), none, none)
	plant(ti.createConnection(t, ti.orgID), none, none, conv.ToPGText("unattributed-copy"), none)

	lastWrittenAt := platformOnly.UpdatedAt.Time
	if orgOnly.UpdatedAt.Time.After(lastWrittenAt) {
		lastWrittenAt = orgOnly.UpdatedAt.Time
	}

	store := marketplacetokens.NewCopyStore(ti.conn)
	listed, err := store.ListOrganizationsWithValues(t.Context())
	require.NoError(t, err)
	var organization orgencryption.OrganizationValues
	for _, h := range listed {
		if h.OrganizationID == ti.orgID {
			organization = h
		}
	}
	require.Equal(t, int64(2), organization.Values)
	require.True(t, lastWrittenAt.Equal(organization.LastWrittenAt), "listed %v, stored %v", organization.LastWrittenAt, lastWrittenAt)

	counted, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, organization.Values, counted.Values)
	require.True(t, organization.LastWrittenAt.Equal(counted.LastWrittenAt))

	other := createOrganization(t, ti.conn)
	empty, err := store.Count(t.Context(), other)
	require.NoError(t, err)
	require.Zero(t, empty.Values)
	require.True(t, empty.LastWrittenAt.IsZero())
}
