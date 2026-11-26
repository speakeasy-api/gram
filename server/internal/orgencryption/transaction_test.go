package orgencryption_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func transactionService(t *testing.T, ti *testInstance) (*orgencryption.Service, *pgxpool.Pool) {
	t.Helper()
	cfg := ti.conn.Config()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	service, err := orgencryption.NewService(testenv.NewLogger(t), pool, newIdentity(), ti.fixture.KMS.Clients, ti.fixture.Keys)
	require.NoError(t, err)
	return service, pool
}

// A transaction-bound Encryptor never calls KMS: with a cold process cache it
// fails, and nothing it reads enters the shared cache.
func TestTransactionNeverUnwrapsInsideTheTransaction(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	service, pool := transactionService(t, ti)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: explicit transaction protocol under test
	require.NoError(t, err)
	defer rollbackTransaction(t, tx)
	encryptor, err := service.ForIn(tx, ti.orgID, useCase)
	require.NoError(t, err)
	before := ti.fixture.KMS.Decrypts()
	require.ErrorIs(t, encryptor.RequireUsable(ctx), orgencryption.ErrKeyNotWarm)
	_, err = encryptor.Encrypt(ctx, column, []byte("transaction secret"))
	require.ErrorIs(t, err, orgencryption.ErrKeyNotWarm)
	require.Equal(t, before, ti.fixture.KMS.Decrypts())
	require.NoError(t, tx.Rollback(t.Context()))

	require.NoError(t, service.Warm(ctx, ti.orgID, useCase))
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts(), "Warm unwraps on the pool")
	warmTx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: explicit transaction protocol under test
	require.NoError(t, err)
	defer rollbackTransaction(t, warmTx)
	warm, err := service.ForIn(warmTx, ti.orgID, useCase)
	require.NoError(t, err)
	_, err = warm.Encrypt(ctx, column, []byte("transaction secret"))
	require.NoError(t, err)
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts())
}

func TestTransactionRollbackDoesNotPublishSuspension(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	service, pool := transactionService(t, ti)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: explicit transaction protocol under test
	require.NoError(t, err)
	defer rollbackTransaction(t, tx)
	_, err = repo.New(tx).SuspendEncryptionConfig(ctx, repo.SuspendEncryptionConfigParams{OrganizationID: ti.orgID, SuspendedReason: pgtype.Text{String: "transaction isolation test", Valid: true}})
	require.NoError(t, err)
	encryptor, err := service.ForIn(tx, ti.orgID, useCase)
	require.NoError(t, err)
	require.ErrorIs(t, encryptor.RequireUsable(ctx), orgencryption.ErrSuspended)
	require.NoError(t, tx.Rollback(t.Context()))
	committed, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	require.NoError(t, committed.RequireUsable(ctx))
}

func TestTransactionIgnoresWarmSharedCaches(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	service, pool := transactionService(t, ti)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	committed, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	require.NoError(t, committed.RequireUsable(ctx))
	tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: explicit transaction protocol under test
	require.NoError(t, err)
	defer rollbackTransaction(t, tx)
	_, err = repo.New(tx).SuspendEncryptionConfig(ctx, repo.SuspendEncryptionConfigParams{OrganizationID: ti.orgID, SuspendedReason: pgtype.Text{String: "transaction isolation test", Valid: true}})
	require.NoError(t, err)
	encryptor, err := service.ForIn(tx, ti.orgID, useCase)
	require.NoError(t, err)
	require.ErrorIs(t, encryptor.RequireUsable(ctx), orgencryption.ErrSuspended)
}

func TestTransactionReusesWarmSharedDataKey(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	service, pool := transactionService(t, ti)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	committed, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	require.NoError(t, committed.RequireUsable(ctx))
	before := ti.fixture.KMS.Decrypts()
	for range 3 {
		tx, err := pool.Begin(ctx) //nolint:glint // notestingrawsql: explicit transaction protocol under test
		require.NoError(t, err)
		defer rollbackTransaction(t, tx)
		encryptor, err := service.ForIn(tx, ti.orgID, useCase)
		require.NoError(t, err)
		ciphertext, err := encryptor.Encrypt(ctx, column, []byte("transaction secret"))
		require.NoError(t, err)
		plaintext, err := encryptor.Decrypt(ctx, column, ciphertext)
		require.NoError(t, err)
		require.Equal(t, "transaction secret", string(plaintext))
		require.NoError(t, tx.Commit(ctx))
	}
	require.Equal(t, before, ti.fixture.KMS.Decrypts(), "transactions reuse the committed DEK instead of unwrapping it again")
}

func TestTransactionForInValidation(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	var pool *pgxpool.Pool
	for _, tc := range []struct {
		name        string
		db          repo.DBTX
		orgID       string
		useCase     orgencryption.UseCase
		wantMessage string
		wantErr     error
	}{
		{name: "nil database", orgID: ti.orgID, useCase: useCase, wantMessage: "orgencryption: database is required"},
		{name: "typed nil database", db: pool, orgID: ti.orgID, useCase: useCase, wantMessage: "orgencryption: database is required"},
		{name: "missing organization", db: ti.conn, useCase: useCase, wantMessage: "orgencryption: organization id is required"},
		{name: "unknown use case", db: ti.conn, orgID: ti.orgID, useCase: "unknown", wantErr: orgencryption.ErrUnknownUseCase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.fixture.Service.ForIn(tc.db, tc.orgID, tc.useCase)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.ErrorContains(t, err, tc.wantMessage)
			}
		})
	}
}

func rollbackTransaction(t *testing.T, tx pgx.Tx) {
	t.Helper()
	err := tx.Rollback(t.Context())
	if !errors.Is(err, pgx.ErrTxClosed) {
		require.NoError(t, err)
	}
}

// Warm leaves the DEK cached with at least warmMargin of life, so the
// transaction opened right after it still finds the DEK.
func TestWarmRefreshesAKeyCloseToExpiry(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	service, _ := transactionService(t, ti)
	active, err := repo.New(ti.conn).GetActiveDataEncryptionKey(t.Context(), repo.GetActiveDataEncryptionKeyParams{OrganizationID: ti.orgID, UseCase: string(useCase)})
	require.NoError(t, err)
	before := ti.fixture.KMS.Decrypts()

	require.NoError(t, service.Warm(t.Context(), ti.orgID, useCase))
	require.NoError(t, service.Warm(t.Context(), ti.orgID, useCase))
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts(), "a fresh key is reused")

	service.AgeDataKey(active.ID, orgencryption.DataKeyCacheTTL-10*time.Second)
	require.NoError(t, service.Warm(t.Context(), ti.orgID, useCase))
	require.Equal(t, before+2, ti.fixture.KMS.Decrypts(), "a key about to expire is unwrapped again")
}
