package aiproviderkeys_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiproviderkeys"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestQueriesFenceEveryKeyWrite(t *testing.T) {
	t.Parallel()

	for _, check := range []struct {
		path    string
		columns []string
	}{
		{path: "queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted", "key_encrypted", "key_org_encrypted"}},
		{path: "../aiintegrations/queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted"}},
		{path: "../modelkeys/queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted"}},
		{path: "../thirdparty/openrouter/queries.sql", columns: []string{"key_encrypted", "key_org_encrypted"}},
	} {
		queries, err := os.ReadFile(check.path)
		require.NoError(t, err)
		orgencryptiontest.RequireFencedWrites(t, string(queries), check.columns...)
	}
}

func TestStoreInPlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.True(t, stored.Platform.Valid)
	require.False(t, stored.Org.Valid)

	opened, err := ti.keys.Open(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)

	require.NoError(t, ti.keys.Store(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.True(t, stored.Platform.Valid)
	require.True(t, stored.Org.Valid)

	ti.finalize(t)
	opened, err := ti.keys.Open(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, orgencryption.Copies{Org: stored.Org})
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreAfterFinalizeWritesOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)

	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.False(t, stored.Platform.Valid)
	require.True(t, stored.Org.Valid)

	opened, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreSealsAgainWhenTheTransitionMoves(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	write := ti.createKey("chat")
	attempts := 0

	err := ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error) {
		attempts++
		if attempts == 1 {
			ti.enroll(t)
		}
		return write(ctx, copies, generation)
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)

	stored := ti.stored(t, "chat")
	require.True(t, stored.Org.Valid, "the second attempt seals for dual_write")
}

func TestStoreReportsAWriteThatMatchesNoRow(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	err := ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), func(context.Context, orgencryption.Copies, int64) (bool, error) {
		return false, nil
	})
	require.ErrorIs(t, err, aiproviderkeys.ErrNotStored)
}

func TestSuspendedKeyIsACustomerKeyProblemNotUnreadable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))
	ti.finalize(t)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, ti.stored(t, "chat"))
	require.Error(t, err)
	require.False(t, aiproviderkeys.IsUnreadable(err), "a suspended key never asks for a new key: %v", err)
	require.True(t, aiproviderkeys.IsCustomerKeyProblem(err), "%v", err)

	require.Error(t, ti.keys.RequireUsable(t.Context(), ti.conn, ti.orgID))

	err = ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("internal"))
	require.Error(t, err, "nothing falls back to the platform key")
}

func TestBackfillAndFinalizeMoveKeysToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-chat"), ti.createKey("chat")))
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-internal"), ti.createKey("internal")))

	stores := aiproviderkeys.CopyStores(ti.conn)
	var waiting int64
	for _, store := range stores {
		counts, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err)
		waiting += counts.Waiting
	}
	require.Equal(t, int64(2), waiting)

	ti.enroll(t)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, stores)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Converted)

	ti.finalize(t)

	for keyType, want := range map[string]string{"chat": "sk-or-chat", "internal": "sk-or-internal"} {
		stored := ti.stored(t, keyType)
		require.False(t, stored.Platform.Valid, "the platform-key copy is cleared")
		require.True(t, stored.Org.Valid)

		opened, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
		require.NoError(t, err)
		require.Equal(t, want, string(opened))
	}
}
