package pgdriver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/pgdriver"
)

// invitation is the context of the chart whose state lives on a domain row.
type invitation struct {
	ExpiresAt time.Time `json:"expires_at"`
}

type acceptInvite struct{}

func invitationMachine(t *testing.T) *sc.Machine[string, invitation] {
	t.Helper()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, invitation]) {
		b.Initial("pending")
		pending := b.State("pending")
		pending.Deadline("expiresAt", func(c invitation) time.Time { return c.ExpiresAt }).To("expired")
		pending.On[acceptInvite]().Do("welcome", func(context.Context, invitation, acceptInvite) {}).To("accepted")
		b.Final("expired")
		b.Final("accepted")
	})
	require.NoError(t, err)

	return m
}

// invitationStore keeps each instance on a row of its own table and copies
// the active state into a status column that queries can filter on, the way
// a domain table would.
type invitationStore struct {
	table string
}

func newInvitationStore(t *testing.T) invitationStore {
	t.Helper()

	table := fmt.Sprintf("invitations_%d", machineSeq.Add(1))
	_, err := pool.Exec(t.Context(), fmt.Sprintf(`
		CREATE TABLE %s (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			snapshot JSONB NOT NULL
		)`, table))
	require.NoError(t, err)

	return invitationStore{table: table}
}

func (s invitationStore) status(snap sc.Snapshot[string, invitation]) string {
	return snap.Active()[0]
}

func (s invitationStore) Lock(ctx context.Context, tx pgx.Tx, id string) ([]byte, error) {
	return s.read(ctx, tx, id, " FOR UPDATE")
}

func (s invitationStore) Read(ctx context.Context, q pgdriver.Querier, id string) ([]byte, error) {
	return s.read(ctx, q, id, "")
}

func (s invitationStore) read(ctx context.Context, q pgdriver.Querier, id, lock string) ([]byte, error) {
	var data []byte
	err := q.QueryRow(ctx, fmt.Sprintf(`SELECT snapshot FROM %s WHERE id = $1%s`, s.table, lock), id).Scan(&data)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, pgdriver.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("read invitation: %w", err)
	}

	return data, nil
}

func (s invitationStore) Insert(ctx context.Context, tx pgx.Tx, id string, snap sc.Snapshot[string, invitation], data []byte) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, status, snapshot) VALUES ($1, $2, $3)`, s.table), id, s.status(snap), data); err != nil {
		return fmt.Errorf("insert invitation: %w", err)
	}

	return nil
}

func (s invitationStore) Update(ctx context.Context, tx pgx.Tx, id string, step sc.Step[string, invitation], data []byte) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = $2, snapshot = $3 WHERE id = $1`, s.table), id, s.status(step.To), data); err != nil {
		return fmt.Errorf("update invitation: %w", err)
	}

	return nil
}

func (s invitationStore) statusOf(t *testing.T, id string) string {
	t.Helper()

	var status string
	require.NoError(t, pool.QueryRow(t.Context(), fmt.Sprintf(`SELECT status FROM %s WHERE id = $1`, s.table), id).Scan(&status))

	return status
}

// capturingSink keeps the encoded effects instead of writing them anywhere,
// standing in for an existing outbox.
type capturingSink struct {
	m *sc.Machine[string, invitation]

	mu      sync.Mutex
	encoded [][]byte
}

func (s *capturingSink) Enqueue(_ context.Context, _ pgx.Tx, _ string, _ time.Time, effects []sc.Effect[invitation]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range effects {
		data, err := s.m.EncodeEffect(e)
		if err != nil {
			return fmt.Errorf("encode effect: %w", err)
		}
		s.encoded = append(s.encoded, data)
	}

	return nil
}

func TestSendTxCommitsWithTheCallersTransactionAndRollsBackWithIt(t *testing.T) {
	t.Parallel()

	m := invitationMachine(t)
	store := newInvitationStore(t)
	sink := &capturingSink{m: m, mu: sync.Mutex{}, encoded: nil}
	d := pgdriver.New(pool, m, pgdriver.WithStore[string, invitation](store), pgdriver.WithSink[invitation](sink))

	_, err := d.Create(t.Context(), "i1", invitation{ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.Equal(t, "pending", store.statusOf(t, "i1"))

	errAborted := errors.New("caller aborted")
	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		step, err := d.SendTx(t.Context(), tx, "i1", acceptInvite{})
		require.NoError(t, err)
		require.True(t, step.Accepted())

		return errAborted
	})
	require.ErrorIs(t, err, errAborted)
	require.Equal(t, "pending", store.statusOf(t, "i1"))

	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := d.SendTx(t.Context(), tx, "i1", acceptInvite{}); err != nil {
			return fmt.Errorf("accept: %w", err)
		}

		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", store.statusOf(t, "i1"))
}

func TestSendTxWritesNothingForAnEventItIgnores(t *testing.T) {
	t.Parallel()

	m := invitationMachine(t)
	store := newInvitationStore(t)
	sink := &capturingSink{m: m, mu: sync.Mutex{}, encoded: nil}
	d := pgdriver.New(pool, m, pgdriver.WithStore[string, invitation](store), pgdriver.WithSink[invitation](sink))

	_, err := d.Create(t.Context(), "i1", invitation{ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	before := len(sink.encoded)

	step, err := d.Send(t.Context(), "i1", tick{})
	require.NoError(t, err)

	require.False(t, step.Accepted())
	require.Len(t, sink.encoded, before)
}

func TestACustomSinksConsumerPerformsTheEffects(t *testing.T) {
	t.Parallel()

	m := invitationMachine(t)
	store := newInvitationStore(t)
	sink := &capturingSink{m: m, mu: sync.Mutex{}, encoded: nil}
	d := pgdriver.New(pool, m, pgdriver.WithStore[string, invitation](store), pgdriver.WithSink[invitation](sink))

	// The expiry is already past, so the deadline's effect is due at once.
	_, err := d.Create(t.Context(), "i1", invitation{ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	require.ErrorIs(t, d.Work(t.Context()), pgdriver.ErrNoOutbox)

	// The deadline fired inside Create, so nothing was enqueued and the
	// row already says expired.
	require.Empty(t, sink.encoded)
	require.Equal(t, "expired", store.statusOf(t, "i1"))

	_, err = d.Create(t.Context(), "i2", invitation{ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.Len(t, sink.encoded, 1)

	var timer struct {
		Kind sc.EffectKind `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(sink.encoded[0], &timer))
	require.Equal(t, sc.EffectStartTimer, timer.Kind)

	// Performing the timer early is what a consumer that ignores run
	// times would do; the driver's clock is the authority, so it fires.
	require.NoError(t, d.Perform(t.Context(), "i2", sink.encoded[0]))
	require.Equal(t, "expired", store.statusOf(t, "i2"))
}

func TestLoadShowsAnExpiryNothingHasProcessed(t *testing.T) {
	t.Parallel()

	m := invitationMachine(t)
	store := newInvitationStore(t)
	sink := &capturingSink{m: m, mu: sync.Mutex{}, encoded: nil}
	d := pgdriver.New(pool, m, pgdriver.WithStore[string, invitation](store), pgdriver.WithSink[invitation](sink))

	// expiresIn is short so the test can pass it; long enough that the
	// invitation is still pending when created.
	const expiresIn = 300 * time.Millisecond

	_, err := d.Create(t.Context(), "i1", invitation{ExpiresAt: time.Now().Add(expiresIn)})
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		snap, err := d.Load(t.Context(), "i1")
		if assert.NoError(c, err) {
			assert.Equal(c, []string{"expired"}, snap.Active())
		}
	}, 10*time.Second, 50*time.Millisecond)

	// Load writes nothing: the row still says pending until something
	// sends the instance an event.
	require.Equal(t, "pending", store.statusOf(t, "i1"))
}
