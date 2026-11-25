package upstreamtokens_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// upstreamTokenQueriesPath is the only queries file allowed to write a copy of
// a stored token, relative to this package.
const upstreamTokenQueriesPath = "../upstreamtoken_queries.sql"

// tokenColumns are the bare column names of every copy the use case holds.
var tokenColumns = []string{
	"access_token_encrypted", "access_token_org_encrypted",
	"refresh_token_encrypted", "refresh_token_org_encrypted",
	"identity_assertion_encrypted", "identity_assertion_org_encrypted",
	"upstream_subject_encrypted", "upstream_subject_org_encrypted",
	"nonce_encrypted", "nonce_org_encrypted",
}

func TestUpstreamTokenQueriesFenceEveryCopyWrite(t *testing.T) {
	t.Parallel()

	queries, err := os.ReadFile(upstreamTokenQueriesPath)
	require.NoError(t, err)

	orgencryptiontest.RequireFencedWrites(t, string(queries), tokenColumns...)
}

func TestSetRemoteSessionInPlatformKeyWritesOnlyPlatformCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.True(t, row.AccessTokenEncrypted.Valid)
	require.False(t, row.AccessTokenOrgEncrypted.Valid)
	require.True(t, row.RefreshTokenEncrypted.Valid)
	require.False(t, row.RefreshTokenOrgEncrypted.Valid)
	require.Equal(t, session.UpdatedAt, row.UpdatedAt, "the statement that changed the session owns updated_at")

	access, refresh := ti.open(t, row)
	require.Equal(t, "access", access)
	require.Equal(t, "refresh", refresh)
}

func TestSetRemoteSessionInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session := ti.createSession(t)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.True(t, row.AccessTokenEncrypted.Valid)
	require.True(t, row.AccessTokenOrgEncrypted.Valid)
	require.True(t, row.RefreshTokenEncrypted.Valid)
	require.True(t, row.RefreshTokenOrgEncrypted.Valid)

	plaintext, err := ti.platform.Decrypt(row.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "refresh", plaintext, "the platform-key copy stays usable for a rollback")

	orgOnly := row
	orgOnly.AccessTokenEncrypted = nullText()
	orgOnly.RefreshTokenEncrypted = nullText()
	access, refresh := ti.open(t, orgOnly)
	require.Equal(t, "access", access, "dual_write reads the organization-key copy")
	require.Equal(t, "refresh", refresh)
}

func TestSetRemoteSessionAfterFinalizeWritesOnlyOrganizationCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	session := ti.createSession(t)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.False(t, row.AccessTokenEncrypted.Valid)
	require.True(t, row.AccessTokenOrgEncrypted.Valid)
	require.False(t, row.RefreshTokenEncrypted.Valid)
	require.True(t, row.RefreshTokenOrgEncrypted.Valid)

	access, refresh := ti.open(t, row)
	require.Equal(t, "access", access)
	require.Equal(t, "refresh", refresh)
}

func TestSetRemoteSessionKeepsAnEchoedRefreshTokenAsStored(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)
	first, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)

	// Enrolled since, so sealing the refresh token again would add a copy.
	ti.enroll(t)
	second, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access 2", RefreshToken: "", KeepRefreshToken: true})
	require.NoError(t, err)
	require.Equal(t, first.RefreshTokenEncrypted, second.RefreshTokenEncrypted, "a kept refresh token is not sealed again")
	require.Equal(t, first.RefreshTokenOrgEncrypted, second.RefreshTokenOrgEncrypted)
	require.True(t, second.AccessTokenOrgEncrypted.Valid, "the new access token follows dual_write")

	access, refresh := ti.open(t, second)
	require.Equal(t, "access 2", access)
	require.Equal(t, "refresh", refresh)
}

func TestSetRemoteSessionWithoutARefreshTokenErasesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session := ti.createSession(t)
	_, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access 2", RefreshToken: "", KeepRefreshToken: false})
	require.NoError(t, err)
	require.False(t, row.RefreshTokenEncrypted.Valid)
	require.False(t, row.RefreshTokenOrgEncrypted.Valid)
	require.False(t, upstreamtokens.Has(upstreamtokens.Stored(row.RefreshTokenEncrypted, row.RefreshTokenOrgEncrypted)))
}

func TestSetRemoteSessionSealsAgainWhenTheTransitionMovesAfterTheDecision(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)

	// Cache platform_key in this process, then enroll through another
	// process's transition state so the cached decision is stale.
	require.NoError(t, ti.tokens.RequireUsable(t.Context(), ti.conn, session.OrganizationID))
	_, err := ti.otherProcessTransitions(t).Enroll(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, orgencryption.SystemActor())
	require.NoError(t, err)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.True(t, row.AccessTokenOrgEncrypted.Valid, "the fenced write sealed again under dual_write")
	require.True(t, row.RefreshTokenOrgEncrypted.Valid)
}

func TestSetRemoteSessionReportsADeletedSession(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)
	require.NoError(t, repo.New(ti.conn).SoftDeleteRemoteSessionFixture(t.Context(), session.ID))

	_, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "", KeepRefreshToken: false})
	require.ErrorIs(t, err, upstreamtokens.ErrSessionNotFound)
}

func TestSetRemoteSessionRefusesAnotherOrganizationsSession(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)

	_, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, conv.ToPGText("org_"+uuid.NewString()), upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "", KeepRefreshToken: false})
	require.ErrorIs(t, err, upstreamtokens.ErrSessionNotFound)
}

func TestSetRemoteSessionFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session := ti.createSession(t)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	require.Error(t, ti.tokens.RequireUsable(t.Context(), ti.conn, session.OrganizationID), "the check before a provider call fails first")

	_, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.Error(t, err)
	require.True(t, orgencryption.IsKeyProblem(err), "a suspended key is a retryable key problem: %v", err)

	stored := ti.stored(t, session.ID)
	require.False(t, upstreamtokens.Has(upstreamtokens.Stored(stored.AccessTokenEncrypted, stored.AccessTokenOrgEncrypted)), "nothing falls back to the platform key")
}

func TestOpenFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session := ti.createSession(t)
	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "", KeepRefreshToken: false})
	require.NoError(t, err)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err = ti.tokens.Open(t.Context(), ti.conn, row.OrganizationID, upstreamtokens.RemoteSessionAccessToken, upstreamtokens.Stored(row.AccessTokenEncrypted, row.AccessTokenOrgEncrypted))
	require.Error(t, err, "nothing falls back to the platform-key copy")
	require.False(t, orgencryption.IsUnreadable(err), "a suspended key is retryable, never a reason to sign in again: %v", err)
	require.True(t, orgencryption.IsKeyProblem(err))
}

func TestOpenReportsAnEmptyCiphertextAsMissing(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	_, err := ti.tokens.Open(t.Context(), ti.conn, ti.org(), upstreamtokens.RemoteSessionAccessToken, upstreamtokens.Stored(conv.ToPGText(""), nullText()))
	require.ErrorIs(t, err, orgencryption.ErrMissing)
	require.False(t, upstreamtokens.Has(upstreamtokens.Stored(conv.ToPGText(""), nullText())))
}

func TestSessionWithoutOrganizationStaysOnThePlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session := ti.createUnattributedSession(t)

	row, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "access", RefreshToken: "refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.True(t, row.AccessTokenEncrypted.Valid)
	require.False(t, row.AccessTokenOrgEncrypted.Valid, "a row without an organization cannot be sealed for one")

	access, refresh := ti.open(t, row)
	require.Equal(t, "access", access)
	require.Equal(t, "refresh", refresh)

	for _, store := range remoteSessionStores(ti.conn) {
		counts, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err)
		require.Equal(t, int64(1), counts.Unattributed, "%s: its client's organization cannot finalize past it", store.Columns().PlatformColumn)
	}
}

func TestBackfillVerifyAndFinalizeMoveTokensToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	live := ti.createSession(t)
	revoked := ti.createSession(t)

	liveRow, err := ti.tokens.SetRemoteSession(t.Context(), ti.conn, live.ID, live.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "live access", RefreshToken: "live refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	_, err = ti.tokens.SetRemoteSession(t.Context(), ti.conn, revoked.ID, revoked.OrganizationID, upstreamtokens.RemoteSessionTokens{AccessToken: "revoked access", RefreshToken: "revoked refresh", KeepRefreshToken: false})
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).SoftDeleteRemoteSessionFixture(t.Context(), revoked.ID))

	stores := upstreamtokens.CopyStores(ti.conn)
	for _, store := range remoteSessionStores(ti.conn) {
		counts, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err)
		require.False(t, counts.LastWrittenAt.IsZero(), store.Columns().PlatformColumn)
		require.Equal(t, orgencryption.CopyCounts{Waiting: 2, Unreadable: 0, Unattributed: 0, PlatformCopies: 2, Values: 2, LastWrittenAt: counts.LastWrittenAt}, counts, store.Columns().PlatformColumn)
	}

	ti.enroll(t)
	result, err := ti.runner(t).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, stores)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Converted, "tokens of revoked sessions are backfilled too, for upstream revocation")

	sealed := ti.stored(t, live.ID)
	require.True(t, sealed.AccessTokenOrgEncrypted.Valid)
	require.True(t, sealed.RefreshTokenOrgEncrypted.Valid)
	require.Equal(t, liveRow.UpdatedAt, sealed.UpdatedAt, "the backfill leaves the refresh compare-and-swap version alone")

	ti.finalize(t)

	finalized := ti.stored(t, live.ID)
	require.False(t, finalized.AccessTokenEncrypted.Valid)
	require.False(t, finalized.RefreshTokenEncrypted.Valid)
	require.Equal(t, liveRow.UpdatedAt, finalized.UpdatedAt, "clearing the platform-key copies changes nothing else about the session")

	access, refresh := ti.open(t, finalized)
	require.Equal(t, "live access", access)
	require.Equal(t, "live refresh", refresh)

	tombstone := ti.stored(t, revoked.ID)
	access, refresh = ti.open(t, tombstone)
	require.Equal(t, "revoked access", access, "a revoked session's tokens stay readable for revocation")
	require.Equal(t, "revoked refresh", refresh)
}

func TestCopyStoreCountsUnreadableTokens(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)

	// A platform-key copy the platform key cannot open, as a restored or
	// rotated platform key would leave behind.
	_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
		AccessPlatformCopy:  conv.ToPGText("not-a-platform-ciphertext"),
		AccessOrgCopy:       nullText(),
		ReplaceRefreshToken: false,
		RefreshPlatformCopy: nullText(),
		RefreshOrgCopy:      nullText(),
		ID:                  session.ID,
		OrganizationID:      session.OrganizationID,
		DecidedGeneration:   0,
	})
	require.NoError(t, err)

	ti.enroll(t)
	stores := upstreamtokens.CopyStores(ti.conn)
	result, err := ti.runner(t).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, stores)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Unreadable)

	counts, err := stores[0].Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, counts.LastWrittenAt.IsZero())
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 1, Unattributed: 0, PlatformCopies: 1, Values: 1, LastWrittenAt: counts.LastWrittenAt}, counts)
}

func TestCopyStoresTreatAnEmptyPlatformCopyAsNoValue(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)
	unattributed := ti.createUnattributedSession(t)
	sealed := ti.createSession(t)

	// Rows written before access_token_encrypted became nullable store ''
	// for a session without an access token. The sealed session also holds
	// an organization-key copy, so only the '' filter keeps verification
	// from comparing the two: the copy is not a ciphertext and would count
	// as unopenable if it were listed.
	for _, row := range []struct {
		session repo.RemoteSession
		orgCopy pgtype.Text
	}{
		{session: session, orgCopy: nullText()},
		{session: unattributed, orgCopy: nullText()},
		{session: sealed, orgCopy: conv.ToPGText("organization-key copy")},
	} {
		_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
			AccessPlatformCopy:  conv.ToPGText(""),
			AccessOrgCopy:       row.orgCopy,
			ReplaceRefreshToken: false,
			RefreshPlatformCopy: nullText(),
			RefreshOrgCopy:      nullText(),
			ID:                  row.session.ID,
			OrganizationID:      row.session.OrganizationID,
			DecidedGeneration:   0,
		})
		require.NoError(t, err)
	}

	stores := upstreamtokens.CopyStores(ti.conn)
	access := stores[0]
	require.Equal(t, upstreamtokens.RemoteSessionAccessToken, access.Columns())

	counts, err := access.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, counts.LastWrittenAt.IsZero())
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 0, PlatformCopies: 0, Values: 1, LastWrittenAt: counts.LastWrittenAt}, counts, "'' is neither waiting, unreadable, unattributed nor a platform-key copy; only the sealed session's organization-key copy is a value")

	waiting, err := access.ListWaiting(t.Context(), ti.orgID, "", 10)
	require.NoError(t, err)
	require.Empty(t, waiting)

	listed, err := access.ListSealed(t.Context(), ti.orgID, "", 10)
	require.NoError(t, err)
	require.Empty(t, listed, "'' beside an organization-key copy is not a pair to verify")

	ti.enroll(t)
	result, err := ti.runner(t).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, stores)
	require.NoError(t, err)
	require.Zero(t, result.Converted)
	require.Zero(t, result.Unreadable)
	require.Zero(t, result.Mismatched)
	require.Zero(t, result.Unopenable)
	require.True(t, result.VerificationPassed)

	ti.finalize(t)
	require.False(t, ti.stored(t, session.ID).AccessTokenEncrypted.Valid, "clearing still removes ''")
	require.False(t, ti.stored(t, sealed.ID).AccessTokenEncrypted.Valid, "clearing still removes '' beside an organization-key copy")
}

func TestCopyStoreClearReportsWhatItDestroyedByKind(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	alone := ti.createSession(t)
	beside := ti.createSession(t)

	for _, row := range []struct {
		session repo.RemoteSession
		orgCopy pgtype.Text
	}{
		{session: alone, orgCopy: nullText()},
		{session: beside, orgCopy: conv.ToPGText("organization-key copy")},
	} {
		_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
			AccessPlatformCopy:  conv.ToPGText("platform-key copy"),
			AccessOrgCopy:       row.orgCopy,
			ReplaceRefreshToken: false,
			RefreshPlatformCopy: nullText(),
			RefreshOrgCopy:      nullText(),
			ID:                  row.session.ID,
			OrganizationID:      row.session.OrganizationID,
			DecidedGeneration:   0,
		})
		require.NoError(t, err)
	}

	access := upstreamtokens.CopyStores(ti.conn)[0]
	cleared, err := access.ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
	require.NoError(t, err)
	require.Equal(t, orgencryption.ClearedCopies{PlatformOnly: 1, BesideOrgCopy: 1}, cleared)
	require.False(t, ti.stored(t, alone.ID).AccessTokenEncrypted.Valid)
	require.True(t, ti.stored(t, beside.ID).AccessTokenOrgEncrypted.Valid, "the organization-key copy stays")
}

func TestCopyStoreClearDoesNotCountACopyAConcurrentClearDestroyed(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)
	_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
		AccessPlatformCopy:  conv.ToPGText("platform-key copy"),
		AccessOrgCopy:       nullText(),
		ReplaceRefreshToken: false,
		RefreshPlatformCopy: nullText(),
		RefreshOrgCopy:      nullText(),
		ID:                  session.ID,
		OrganizationID:      session.OrganizationID,
		DecidedGeneration:   0,
	})
	require.NoError(t, err)

	// The first clear runs in a transaction that holds the row's lock.
	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	first, err := repo.New(tx).ClearRemoteSessionAccessTokenPlatformCopies(t.Context(), repo.ClearRemoteSessionAccessTokenPlatformCopiesParams{OrganizationID: ti.orgID, LimitValue: 10, DecidedGeneration: 0})
	require.NoError(t, err)
	require.Equal(t, int64(1), first.PlatformOnly)

	// The second clear picked the same row before the first committed, and
	// waits for its lock.
	type clearResult struct {
		cleared orgencryption.ClearedCopies
		err     error
	}
	second := make(chan clearResult, 1)
	go func() {
		cleared, err := upstreamtokens.CopyStores(ti.conn)[0].ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
		second <- clearResult{cleared: cleared, err: err}
	}()
	testenv.WaitForBackendsBlockedBy(t, t.Context(), ti.conn, testenv.BackendPID(tx), 1)
	require.NoError(t, tx.Commit(t.Context()))

	result := <-second
	require.NoError(t, result.err)
	require.Equal(t, orgencryption.ClearedCopies{PlatformOnly: 0, BesideOrgCopy: 0}, result.cleared, "the copy the first clear destroyed is not counted again")
}

func TestCopyStoreClearCountsAnEmptyPlatformCopyApart(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	emptyAlone := ti.createSession(t)
	emptyBeside := ti.createSession(t)
	valueAlone := ti.createSession(t)

	for _, row := range []struct {
		session  repo.RemoteSession
		platform string
		orgCopy  pgtype.Text
	}{
		{session: emptyAlone, platform: "", orgCopy: nullText()},
		{session: emptyBeside, platform: "", orgCopy: conv.ToPGText("organization-key copy")},
		{session: valueAlone, platform: "platform-key copy", orgCopy: nullText()},
	} {
		_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
			AccessPlatformCopy:  conv.ToPGText(row.platform),
			AccessOrgCopy:       row.orgCopy,
			ReplaceRefreshToken: false,
			RefreshPlatformCopy: nullText(),
			RefreshOrgCopy:      nullText(),
			ID:                  row.session.ID,
			OrganizationID:      row.session.OrganizationID,
			DecidedGeneration:   0,
		})
		require.NoError(t, err)
	}

	cleared, err := upstreamtokens.CopyStores(ti.conn)[0].ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
	require.NoError(t, err)
	require.Equal(t, orgencryption.ClearedCopies{PlatformOnly: 1, BesideOrgCopy: 0, Empty: 2}, cleared, "'' is cleared but never reported as a platform-only straggler")
	for _, session := range []repo.RemoteSession{emptyAlone, emptyBeside, valueAlone} {
		require.False(t, ti.stored(t, session.ID).AccessTokenEncrypted.Valid, "every platform-key copy is cleared")
	}
	require.True(t, ti.stored(t, emptyBeside.ID).AccessTokenOrgEncrypted.Valid, "the organization-key copy beside an empty one stays")
}

func TestCopyStoreClearClassifiesTheVersionItClears(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	session := ti.createSession(t)
	setAccess := func(db repo.DBTX, platform string) {
		t.Helper()

		_, err := repo.New(db).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
			AccessPlatformCopy:  conv.ToPGText(platform),
			AccessOrgCopy:       nullText(),
			ReplaceRefreshToken: false,
			RefreshPlatformCopy: nullText(),
			RefreshOrgCopy:      nullText(),
			ID:                  session.ID,
			OrganizationID:      session.OrganizationID,
			DecidedGeneration:   0,
		})
		require.NoError(t, err)
	}
	setAccess(ti.conn, "")

	// A writer replaces the empty copy with a value in a transaction that
	// holds the row's lock while the clear runs.
	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	setAccess(tx, "platform-key copy")

	type clearResult struct {
		cleared orgencryption.ClearedCopies
		err     error
	}
	done := make(chan clearResult, 1)
	go func() {
		cleared, err := upstreamtokens.CopyStores(ti.conn)[0].ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
		done <- clearResult{cleared: cleared, err: err}
	}()
	testenv.WaitForBackendsBlockedBy(t, t.Context(), ti.conn, testenv.BackendPID(tx), 1)
	require.NoError(t, tx.Commit(t.Context()))

	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, orgencryption.ClearedCopies{PlatformOnly: 1, BesideOrgCopy: 0, Empty: 0}, result.cleared, "the value the clear destroyed is reported, not taken for the empty copy it replaced")
	require.False(t, ti.stored(t, session.ID).AccessTokenEncrypted.Valid)
}

// remoteSessionStores are the CopyStores of the remote session columns.
func remoteSessionStores(conn *pgxpool.Pool) []orgencryption.CopyStore {
	var stores []orgencryption.CopyStore
	for _, store := range upstreamtokens.CopyStores(conn) {
		if strings.HasPrefix(store.Columns().PlatformColumn, "remote_sessions.") {
			stores = append(stores, store)
		}
	}
	return stores
}

func TestSetTrustedIssuerSessionReplacesKeepsAndErasesEachValue(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	session, err := repo.New(ti.conn).CreateTrustedIssuerSessionFixture(t.Context(), repo.CreateTrustedIssuerSessionFixtureParams{
		OrganizationID:        ti.orgID,
		RemoteSessionClientID: ti.createClient(t).ID,
		SubjectUrn:            "user:" + uuid.NewString(),
	})
	require.NoError(t, err)

	first, err := ti.tokens.SetTrustedIssuerSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.TrustedIssuerSessionValues{
		IdentityAssertion: new("assertion"), RefreshToken: new("refresh"), UpstreamSubject: new("subject"), Nonce: new("nonce"),
	})
	require.NoError(t, err)
	require.True(t, first.RefreshTokenEncrypted.Valid)
	require.True(t, first.RefreshTokenOrgEncrypted.Valid, "dual_write seals every replaced value under the organization's key")
	require.Equal(t, session.UpdatedAt, first.UpdatedAt, "the statement that changed the session owns updated_at")

	second, err := ti.tokens.SetTrustedIssuerSession(t.Context(), ti.conn, session.ID, session.OrganizationID, upstreamtokens.TrustedIssuerSessionValues{
		IdentityAssertion: new(""), RefreshToken: new("rotated"), UpstreamSubject: nil, Nonce: nil,
	})
	require.NoError(t, err)
	require.False(t, second.IdentityAssertionEncrypted.Valid)
	require.False(t, second.IdentityAssertionOrgEncrypted.Valid, "an erased value clears both copies")
	require.Equal(t, first.UpstreamSubjectEncrypted, second.UpstreamSubjectEncrypted, "a kept value keeps both stored copies")
	require.Equal(t, first.UpstreamSubjectOrgEncrypted, second.UpstreamSubjectOrgEncrypted)
	require.Equal(t, first.NonceOrgEncrypted, second.NonceOrgEncrypted)

	for columns, want := range map[orgencryption.CopyColumns]string{
		upstreamtokens.TrustedIssuerSessionRefreshToken:    "rotated",
		upstreamtokens.TrustedIssuerSessionUpstreamSubject: "subject",
		upstreamtokens.TrustedIssuerSessionNonce:           "nonce",
	} {
		var stored orgencryption.Copies
		switch columns {
		case upstreamtokens.TrustedIssuerSessionRefreshToken:
			stored = upstreamtokens.Stored(nullText(), second.RefreshTokenOrgEncrypted)
		case upstreamtokens.TrustedIssuerSessionUpstreamSubject:
			stored = upstreamtokens.Stored(nullText(), second.UpstreamSubjectOrgEncrypted)
		default:
			stored = upstreamtokens.Stored(nullText(), second.NonceOrgEncrypted)
		}
		opened, err := ti.tokens.Open(t.Context(), ti.conn, second.OrganizationID, columns, stored)
		require.NoError(t, err)
		require.Equal(t, want, opened, columns.OrgColumn)
	}
}

func TestCopyStoresCoverEveryOrganizationKeyColumn(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	got := map[string]bool{}
	for _, store := range upstreamtokens.CopyStores(ti.conn) {
		got[store.Columns().OrgColumn] = true
	}

	want, err := repo.New(ti.conn).ListOrganizationKeyTokenColumnsFixture(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, want)
	for _, column := range want {
		require.True(t, got[column], "%s has no CopyStore, so finalize would leave its platform-key copies behind", column)
	}
	require.Len(t, got, len(want), "every CopyStore names an organization-key column of the use case's tables")
}

func TestSessionOrganizationOfAProjectClientWithoutOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: ti.projectID, Valid: true},
		OrganizationID:          nullText(),
		RemoteSessionIssuerID:   ti.remoteIssuerID,
		ClientID:                "client-" + uuid.NewString(),
		TokenEndpointAuthMethod: conv.ToPGText("none"),
	})
	require.NoError(t, err)

	organization, err := repo.New(ti.conn).GetRemoteSessionClientSessionOrganization(t.Context(), client.ID)
	require.NoError(t, err)
	require.Equal(t, ti.orgID, organization, "the key check uses the organization a session under the client is stored with")

	session, err := repo.New(ti.conn).CreateRemoteSessionFixture(t.Context(), repo.CreateRemoteSessionFixtureParams{
		SubjectUrn:            urn.NewUserSubject("user_" + uuid.NewString()),
		UserSessionIssuerID:   ti.userSessionIssuer,
		RemoteSessionClientID: client.ID,
	})
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText(organization), session.OrganizationID, "the same organization the session row records")
}

// setSessionTokens stores a session's access and refresh tokens as the given
// copies, under generation 0.
func (ti *testInstance) setSessionTokens(t *testing.T, session repo.RemoteSession, accessPlatform pgtype.Text, accessOrg pgtype.Text, refreshPlatform pgtype.Text, refreshOrg pgtype.Text) {
	t.Helper()

	_, err := repo.New(ti.conn).SetRemoteSessionTokens(t.Context(), repo.SetRemoteSessionTokensParams{
		AccessPlatformCopy:  accessPlatform,
		AccessOrgCopy:       accessOrg,
		ReplaceRefreshToken: true,
		RefreshPlatformCopy: refreshPlatform,
		RefreshOrgCopy:      refreshOrg,
		ID:                  session.ID,
		OrganizationID:      session.OrganizationID,
		DecidedGeneration:   0,
	})
	require.NoError(t, err)
}

// createTrustedIssuerSession creates a trusted issuer session of the test
// organization storing every value as the given copies, under generation 0.
func (ti *testInstance) createTrustedIssuerSession(t *testing.T, platform pgtype.Text, org pgtype.Text) repo.TrustedIssuerSession {
	t.Helper()

	session, err := repo.New(ti.conn).CreateTrustedIssuerSessionFixture(t.Context(), repo.CreateTrustedIssuerSessionFixtureParams{
		OrganizationID:        ti.orgID,
		RemoteSessionClientID: ti.createClient(t).ID,
		SubjectUrn:            "user:" + uuid.NewString(),
	})
	require.NoError(t, err)

	row, err := repo.New(ti.conn).SetTrustedIssuerSessionTokens(t.Context(), repo.SetTrustedIssuerSessionTokensParams{
		ReplaceIdentityAssertion:      true,
		IdentityAssertionPlatformCopy: platform,
		IdentityAssertionOrgCopy:      org,
		ReplaceRefreshToken:           true,
		RefreshTokenPlatformCopy:      platform,
		RefreshTokenOrgCopy:           org,
		ReplaceUpstreamSubject:        true,
		UpstreamSubjectPlatformCopy:   platform,
		UpstreamSubjectOrgCopy:        org,
		ReplaceNonce:                  true,
		NoncePlatformCopy:             platform,
		NonceOrgCopy:                  org,
		ID:                            session.ID,
		OrganizationID:                session.OrganizationID,
		DecidedGeneration:             0,
	})
	require.NoError(t, err)

	return row
}

// plantEMACredential plants an EMA credential slot written at updatedAt. A
// live slot holds a platform-key copy of its access token; a retired one
// holds none. A slot without organization_id has no project either.
func (ti *testInstance) plantEMACredential(t *testing.T, organizationID pgtype.Text, retired bool, updatedAt time.Time) uuid.UUID {
	t.Helper()

	project := uuid.NullUUID{UUID: ti.projectID, Valid: organizationID.Valid}
	client := ti.createClient(t)
	id, err := repo.New(ti.conn).PlantEMACredentialFixture(t.Context(), repo.PlantEMACredentialFixtureParams{
		OrganizationID:          organizationID,
		ProjectID:               project,
		UserSessionIssuerID:     uuid.NullUUID{UUID: ti.userSessionIssuer, Valid: true},
		RemoteSessionIssuerID:   uuid.NullUUID{UUID: ti.remoteIssuerID, Valid: true},
		RemoteSessionClientID:   uuid.NullUUID{UUID: client.ID, Valid: true},
		Resource:                "https://resource.example.test",
		SubjectUrn:              urn.NewUserSubject("user_" + uuid.NewString()).String(),
		BindingGeneration:       pgtype.Int8{Int64: 0, Valid: false},
		EmaProvider:             "external",
		InternalSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedIssuerSessionID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Retired:                 retired,
		UpdatedAt:               pgtype.Timestamptz{Time: updatedAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	return id
}

// setEMACredentialAccessToken stores an EMA credential's access token as the
// given copies, under generation 0.
func (ti *testInstance) setEMACredentialAccessToken(t *testing.T, id uuid.UUID, platform pgtype.Text, org pgtype.Text) {
	t.Helper()

	_, err := repo.New(ti.conn).SetEMACredentialAccessToken(t.Context(), repo.SetEMACredentialAccessTokenParams{
		PlatformCopy:      platform,
		OrgCopy:           org,
		ID:                id,
		OrganizationID:    ti.org(),
		DecidedGeneration: 0,
	})
	require.NoError(t, err)
}

// valuesFor returns the organization's entry in organizations.
func valuesFor(t *testing.T, organizations []orgencryption.OrganizationValues, organizationID string) orgencryption.OrganizationValues {
	t.Helper()

	for _, entry := range organizations {
		if entry.OrganizationID == organizationID {
			return entry
		}
	}
	require.Failf(t, "organization not listed", "%s holds values but is not listed", organizationID)

	return orgencryption.OrganizationValues{}
}

// latest returns the latest of times.
func latest(times ...time.Time) time.Time {
	var last time.Time
	for _, at := range times {
		if at.After(last) {
			last = at
		}
	}
	return last
}

func TestCopyStoresValuesCountAValueUnderEitherKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	platform := conv.ToPGText("platform-key copy")
	org := conv.ToPGText("organization-key copy")
	empty := conv.ToPGText("")

	// Each column holds two values in the test organization: one under a
	// single key, and one under the other key or both, on a soft-deleted row
	// where the table has a fixture for it. The rows written after them hold
	// only '' or nothing, or have no organization_id, so they count nowhere
	// and leave LastWrittenAt alone.
	platformOnly := ti.createSession(t)
	ti.setSessionTokens(t, platformOnly, platform, nullText(), nullText(), org)
	revoked := ti.createSession(t)
	ti.setSessionTokens(t, revoked, platform, org, platform, org)
	require.NoError(t, repo.New(ti.conn).SoftDeleteRemoteSessionFixture(t.Context(), revoked.ID))
	unattributed := ti.createUnattributedSession(t)
	ti.setSessionTokens(t, unattributed, platform, nullText(), platform, nullText())
	ti.setSessionTokens(t, ti.createSession(t), empty, nullText(), empty, nullText())

	trustedPlatformOnly := ti.createTrustedIssuerSession(t, platform, nullText())
	trustedDeleted := ti.createTrustedIssuerSession(t, nullText(), org)
	deletedRows, err := repo.New(ti.conn).SoftDeleteTrustedIssuerSessionFixture(t.Context(), repo.SoftDeleteTrustedIssuerSessionFixtureParams{ID: trustedDeleted.ID, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.Equal(t, int64(1), deletedRows)
	ti.createTrustedIssuerSession(t, empty, nullText())

	base := time.Now().Add(-time.Hour)
	emaPlatformOnly := ti.plantEMACredential(t, ti.org(), false, base.Add(-2*time.Hour))
	emaOrgOnly := ti.plantEMACredential(t, ti.org(), false, base.Add(-time.Hour))
	ti.setEMACredentialAccessToken(t, emaOrgOnly, nullText(), org)
	ti.plantEMACredential(t, ti.org(), true, base)
	emaEmpty := ti.plantEMACredential(t, ti.org(), false, base.Add(time.Hour))
	ti.setEMACredentialAccessToken(t, emaEmpty, empty, nullText())
	ti.plantEMACredential(t, nullText(), false, base.Add(2*time.Hour))

	ema, err := repo.New(ti.conn).ListEMACredentialsFixture(t.Context(), uuid.NullUUID{UUID: ti.projectID, Valid: true})
	require.NoError(t, err)
	var emaWrites []time.Time
	for _, credential := range ema {
		if credential.ID == emaPlatformOnly || credential.ID == emaOrgOnly {
			emaWrites = append(emaWrites, credential.UpdatedAt.Time)
		}
	}
	require.Len(t, emaWrites, 2)

	lastWrittenAt := map[string]time.Time{
		"remote_sessions":                latest(ti.stored(t, platformOnly.ID).UpdatedAt.Time, ti.stored(t, revoked.ID).UpdatedAt.Time),
		"trusted_issuer_sessions":        latest(trustedPlatformOnly.UpdatedAt.Time, trustedDeleted.UpdatedAt.Time),
		"remote_session_ema_credentials": latest(emaWrites...),
	}

	stores := upstreamtokens.CopyStores(ti.conn)
	require.Len(t, stores, 7)
	for _, store := range stores {
		column := store.Columns().PlatformColumn
		table, _, _ := strings.Cut(column, ".")
		want, ok := lastWrittenAt[table]
		require.True(t, ok, "%s: unexpected table", column)

		counted, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err, column)
		require.Equal(t, int64(2), counted.Values, "%s: a value under either key counts, soft-deleted rows included, and '' or a row without organization_id does not", column)
		require.True(t, want.Equal(counted.LastWrittenAt), "%s: want %s, got %s", column, want, counted.LastWrittenAt)

		organizations, err := store.ListOrganizationsWithValues(t.Context())
		require.NoError(t, err, column)
		listed := valuesFor(t, organizations, ti.orgID)
		require.Equal(t, int64(2), listed.Values, column)
		require.True(t, want.Equal(listed.LastWrittenAt), "%s: want %s, got %s", column, want, listed.LastWrittenAt)
		for _, entry := range organizations {
			require.NotEmpty(t, entry.OrganizationID, "%s: a row without organization_id is never listed", column)
		}
	}
}

func TestCopyStoresValuesListEachOrganizationApart(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	access := upstreamtokens.CopyStores(ti.conn)[0]
	require.Equal(t, upstreamtokens.RemoteSessionAccessToken, access.Columns())

	ti.setSessionTokens(t, ti.createSession(t), conv.ToPGText("platform-key copy"), nullText(), nullText(), nullText())

	other := ti.createOrganization(t)
	for range 2 {
		ti.setSessionTokens(t, ti.createOrganizationSession(t, other), nullText(), conv.ToPGText("organization-key copy"), nullText(), nullText())
	}

	// An organization whose only access token is a legacy '' holds nothing.
	empty := ti.createOrganization(t)
	ti.setSessionTokens(t, ti.createOrganizationSession(t, empty), conv.ToPGText(""), nullText(), nullText(), nullText())

	organizations, err := access.ListOrganizationsWithValues(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), valuesFor(t, organizations, ti.orgID).Values)
	require.Equal(t, int64(2), valuesFor(t, organizations, other).Values)
	for _, entry := range organizations {
		require.NotEqual(t, empty, entry.OrganizationID, "an organization with only '' is not listed")
	}

	for _, organizationID := range []string{empty, "org_" + uuid.NewString()} {
		counted, err := access.Count(t.Context(), organizationID)
		require.NoError(t, err)
		require.Zero(t, counted.Values)
		require.True(t, counted.LastWrittenAt.IsZero(), "an organization without values has no last write")
	}
}

// createOrganization creates another organization and returns its id.
func (ti *testInstance) createOrganization(t *testing.T) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Session Tokens Other Org",
		Slug:        "session-tokens-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

// createOrganizationSession creates a session with no tokens under a new
// organization client of organizationID.
func (ti *testInstance) createOrganizationSession(t *testing.T, organizationID string) repo.RemoteSession {
	t.Helper()

	client, err := repo.New(ti.conn).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:          conv.ToPGText(organizationID),
		RemoteSessionIssuerID:   ti.remoteIssuerID,
		ClientID:                "client-" + uuid.NewString(),
		TokenEndpointAuthMethod: conv.ToPGText("none"),
	})
	require.NoError(t, err)

	session, err := repo.New(ti.conn).CreateRemoteSessionFixture(t.Context(), repo.CreateRemoteSessionFixtureParams{
		SubjectUrn:            urn.NewUserSubject("user_" + uuid.NewString()),
		UserSessionIssuerID:   ti.userSessionIssuer,
		RemoteSessionClientID: client.ID,
	})
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText(organizationID), session.OrganizationID)

	return session
}
