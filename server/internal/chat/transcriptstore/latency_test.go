package transcriptstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// prodUnwrap is the production p50 of one KMS Decrypt that unwraps a
// transcript DEK, from Datadog APM spans on gram-server.
const prodUnwrap = 114 * time.Millisecond

// slowKMS delays every unwrap by the production KMS round trip.
type slowKMS struct {
	*orgencryptiontest.KMS
	delay time.Duration
}

func (k *slowKMS) Decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error) {
	select {
	case <-time.After(k.delay):
	case <-ctx.Done():
		return nil, fmt.Errorf("slow test unwrap: %w", ctx.Err())
	}
	return k.KMS.Decrypt(ctx, keyName, ciphertext, aad) //nolint:wrapcheck // the slow stand-in is transparent
}

func (k *slowKMS) clients(context.Context, oauth2.TokenSource) (gcpkms.EncryptionClient, error) {
	return k, nil
}

func slowStore(t *testing.T, f *fixture) *Store {
	t.Helper()
	kms := &slowKMS{KMS: f.keys.KMS, delay: prodUnwrap}
	service, err := orgencryption.NewService(testenv.NewLogger(t), f.db, gcpauth.NewIdentity(gcpauth.NewStubResolver()), kms.clients, f.keys.Keys)
	require.NoError(t, err)
	store, err := New(f.db, service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	return store
}

func (f *fixture) captureWith(t *testing.T, store *Store, id uuid.UUID) time.Duration {
	t.Helper()
	started := time.Now()
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary is what the writer opens around ResealChatMessages.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	require.NoError(t, store.ResealChatMessages(t.Context(), tx, f.org, f.project, []uuid.UUID{id}))
	require.NoError(t, tx.Commit(t.Context()))
	return time.Since(started)
}

// TestCaptureLatencyWithWarmKey models one message write per request, as
// hooks.ingest does, against a KMS that takes as long as production. Once the
// process cache holds the transcript key, a capture transaction must make no
// KMS call.
func TestCaptureLatencyWithWarmKey(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	store := slowStore(t, f)
	require.NoError(t, store.Prepare(t.Context(), f.org))

	const captures = 10
	before := f.keys.KMS.Decrypts()
	var total time.Duration
	for range captures {
		id := uuid.New()
		f.add(t, id)
		total += f.captureWith(t, store, id)
	}
	unwraps := f.keys.KMS.Decrypts() - before
	perCapture := total / captures
	t.Logf("%d captures: %s per capture, %d KMS unwraps (simulated unwrap %s)", captures, perCapture.Round(100*time.Microsecond), unwraps, prodUnwrap)
	require.Zero(t, unwraps, "a capture transaction reuses the warm process key")
	// The unwrap count is the guarantee; the time only shows its effect, so
	// allow for a loaded test machine.
	require.Less(t, perCapture, prodUnwrap)
}

// TestPrepareKeepsKMSOutOfCaptureTransaction checks the writer's order: Prepare
// before the transaction pays the one unwrap, and the capture inside it pays none.
func TestPrepareKeepsKMSOutOfCaptureTransaction(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	store := slowStore(t, f)
	id := uuid.New()
	f.add(t, id)

	before := f.keys.KMS.Decrypts()
	require.NoError(t, store.Prepare(t.Context(), f.org))
	require.Equal(t, before+1, f.keys.KMS.Decrypts())
	require.NoError(t, store.Prepare(t.Context(), f.org))
	require.Equal(t, before+1, f.keys.KMS.Decrypts(), "a warm cache needs no unwrap")

	f.captureWith(t, store, id)
	require.Equal(t, before+1, f.keys.KMS.Decrypts(), "capture makes no KMS call in its transaction")
	opened, err := store.Load(t.Context(), f.org, f.project, id)
	require.NoError(t, err)
	require.Equal(t, "sensitive example", opened.Content)
}

func TestPrepareFailsClosedWithoutKeys(t *testing.T) {
	t.Parallel()
	f := setup(t, false)
	require.ErrorIs(t, f.store.Prepare(t.Context(), f.org), orgencryption.ErrNotProvisioned)
	require.ErrorIs(t, f.store.Prepare(t.Context(), ""), ErrInvalid)
}
