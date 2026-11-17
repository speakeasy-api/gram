package upstreamtokens_test

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
)

// upstreamTokenQueriesPath is the only queries file allowed to write a copy of
// a stored token, relative to this package.
const upstreamTokenQueriesPath = "../upstreamtoken_queries.sql"

// tokenColumns are the bare column names of every copy the use case holds.
var tokenColumns = []string{
	"access_token_encrypted", "access_token_org_encrypted",
	"refresh_token_encrypted", "refresh_token_org_encrypted",
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
		require.Equal(t, orgencryption.CopyCounts{Waiting: 2, Unreadable: 0, Unattributed: 0, PlatformCopies: 2}, counts, store.Columns().PlatformColumn)
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
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 1, Unattributed: 0, PlatformCopies: 1}, counts)
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
