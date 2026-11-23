package orgencryption_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

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

func TestTransactionColdCachesSingleConnection(t *testing.T) {
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
	for range 3 {
		require.NoError(t, encryptor.RequireUsable(ctx))
		ciphertext, err := encryptor.Encrypt(ctx, column, []byte("transaction secret"))
		require.NoError(t, err)
		plaintext, err := encryptor.Decrypt(ctx, column, ciphertext)
		require.NoError(t, err)
		require.Equal(t, "transaction secret", string(plaintext))
	}
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts())
	require.NoError(t, tx.Rollback(t.Context()))
	// Transaction-local key material must not populate even a cold shared cache.
	committed, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	require.NoError(t, committed.RequireUsable(ctx))
	require.Equal(t, before+2, ti.fixture.KMS.Decrypts())
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

// canceledUnwrap waits for the caller's cancellation, rather than the service's
// detached shared-cache flight timeout.
type canceledUnwrap struct {
	gcpkms.EncryptionClient
	finished chan struct{}
}

func (k *canceledUnwrap) Decrypt(ctx context.Context, _ string, _, _ []byte) ([]byte, error) {
	<-ctx.Done()
	close(k.finished)
	return nil, fmt.Errorf("canceled test unwrap: %w", ctx.Err())
}

func TestTransactionCancellationWaitsForUnwrapToExit(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	ti.provision(t)
	_, pool := transactionService(t, ti)
	kms := &canceledUnwrap{EncryptionClient: ti.fixture.KMS, finished: make(chan struct{})}
	service, err := orgencryption.NewService(testenv.NewLogger(t), pool, newIdentity(), func(context.Context, oauth2.TokenSource) (gcpkms.EncryptionClient, error) { return kms, nil }, ti.fixture.Keys)
	require.NoError(t, err)
	tx, err := pool.Begin(t.Context()) //nolint:glint // notestingrawsql: explicit transaction protocol under test
	require.NoError(t, err)
	defer rollbackTransaction(t, tx)
	encryptor, err := service.ForIn(tx, ti.orgID, useCase)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	require.ErrorIs(t, encryptor.RequireUsable(ctx), context.DeadlineExceeded)
	require.Less(t, time.Since(started), 3*time.Second)
	// The unwrap must already have exited when the operation returns: nothing
	// may keep using the transaction asynchronously after rollback.
	select {
	case <-kms.finished:
	default:
		t.Fatal("transaction unwrap still running after cancellation")
	}
	require.NoError(t, tx.Rollback(t.Context()))
}

func rollbackTransaction(t *testing.T, tx pgx.Tx) {
	t.Helper()
	err := tx.Rollback(t.Context())
	if !errors.Is(err, pgx.ErrTxClosed) {
		require.NoError(t, err)
	}
}
