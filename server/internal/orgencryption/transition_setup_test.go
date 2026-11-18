package orgencryption_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var fakeColumns = orgencryption.CopyColumns{
	UseCase:        useCase,
	PlatformColumn: "example_table.value_encrypted",
	OrgColumn:      "example_table.value_org_encrypted",
}

type transitionInstance struct {
	*testInstance

	platform    *encryption.Client
	transitions *orgencryption.Transitions
	cipher      *orgencryption.Cipher
	runner      *orgencryption.Runner
	store       *fakeCopyStore
}

func newTransitionInstance(t *testing.T) *transitionInstance {
	t.Helper()

	ti := newTestInstance(t)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, ti.fixture.Service, transitions)

	return &transitionInstance{
		testInstance: ti,
		platform:     platform,
		transitions:  transitions,
		cipher:       cipher,
		runner:       orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, cipher, testenv.NewMeterProvider(t)),
		store:        newFakeCopyStore(platform, transitions),
	}
}

func (ti *transitionInstance) enroll(t *testing.T) orgencryption.Transition {
	t.Helper()

	transition, err := ti.transitions.Enroll(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.NoError(t, err)

	return transition
}

// backdate moves the use case's state_changed_at into the past, past any
// minimum time in dual_write.
func (ti *transitionInstance) backdate(t *testing.T, by time.Duration) {
	t.Helper()

	err := repo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), repo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-by), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(useCase),
	})
	require.NoError(t, err)
}

// platformCopy encrypts plaintext with the platform key.
func (ti *transitionInstance) platformCopy(t *testing.T, plaintext string) string {
	t.Helper()

	ciphertext, err := ti.platform.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	return ciphertext
}

type fakeRow struct {
	organizationID string
	platform       pgtype.Text
	org            pgtype.Text
}

// fakeCopyStore is an in-memory CopyStore. Each write checks the transition
// generation from the database and changes the row while holding the store's
// lock, so the check and the write are atomic as in a feature's fenced SQL
// statement.
type fakeCopyStore struct {
	platform    *encryption.Client
	transitions *orgencryption.Transitions
	store       *fakeStore
}

// fakeStore holds a fakeCopyStore's rows, so views of one table can share them.
type fakeStore struct {
	mu   sync.Mutex
	rows map[string]fakeRow
}

func newFakeCopyStore(platform *encryption.Client, transitions *orgencryption.Transitions) *fakeCopyStore {
	return &fakeCopyStore{platform: platform, transitions: transitions, store: &fakeStore{mu: sync.Mutex{}, rows: map[string]fakeRow{}}}
}

// withPlatformKey returns a view of the same rows that counts with another
// platform key, as the table would after the platform key is restored.
func (c *fakeCopyStore) withPlatformKey(platform *encryption.Client) *fakeCopyStore {
	return &fakeCopyStore{platform: platform, transitions: c.transitions, store: c.store}
}

func (c *fakeCopyStore) put(organizationID string, rowID string, copies orgencryption.Copies) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	c.store.rows[rowID] = fakeRow{organizationID: organizationID, platform: copies.Platform, org: copies.Org}
}

func (c *fakeCopyStore) get(rowID string) orgencryption.Copies {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	row := c.store.rows[rowID]

	return orgencryption.Copies{Platform: row.platform, Org: row.org}
}

func (c *fakeCopyStore) Columns() orgencryption.CopyColumns { return fakeColumns }

// sortedIDs returns the organization's row ids after after, in order, whose
// rows pass keep. Callers hold the store's lock.
func (c *fakeCopyStore) sortedIDs(organizationID string, after string, keep func(fakeRow) bool) []string {
	ids := make([]string, 0, len(c.store.rows))
	for id, row := range c.store.rows {
		if row.organizationID == organizationID && id > after && keep(row) {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, cmp.Compare[string])

	return ids
}

func (c *fakeCopyStore) ListWaiting(_ context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	ids := c.sortedIDs(organizationID, after, func(row fakeRow) bool { return row.platform.Valid && !row.org.Valid })
	values := make([]orgencryption.WaitingValue, 0, limit)
	for _, id := range ids[:min(len(ids), int(limit))] {
		values = append(values, orgencryption.WaitingValue{RowID: id, Platform: c.store.rows[id].platform.String})
	}

	return values, nil
}

// fenced reports whether the transition moved past generation. Callers hold
// the store's lock, so no write can land between this check and theirs.
func (c *fakeCopyStore) fenced(ctx context.Context, organizationID string, generation int64) (bool, error) {
	transition, err := c.transitions.GetFresh(ctx, organizationID, useCase)
	if err != nil {
		return false, fmt.Errorf("read transition for fence: %w", err)
	}

	return transition.Generation != generation, nil
}

func (c *fakeCopyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	fenced, err := c.fenced(ctx, organizationID, generation)
	if err != nil || fenced {
		return false, err
	}

	row, ok := c.store.rows[value.RowID]
	if !ok || row.organizationID != organizationID || row.platform.String != value.Platform || row.org.Valid {
		return false, nil
	}
	row.org = conv.ToPGText(orgCiphertext)
	c.store.rows[value.RowID] = row

	return true, nil
}

func (c *fakeCopyStore) ListSealed(_ context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	ids := c.sortedIDs(organizationID, after, func(row fakeRow) bool { return row.platform.Valid && row.org.Valid })
	values := make([]orgencryption.SealedValue, 0, limit)
	for _, id := range ids[:min(len(ids), int(limit))] {
		values = append(values, orgencryption.SealedValue{RowID: id, Platform: c.store.rows[id].platform.String, Org: c.store.rows[id].org.String})
	}

	return values, nil
}

// Count differs from a feature's count query in one way: it treats every
// waiting value whose platform-key copy does not decrypt as recorded
// unreadable, without reading organization_encryption_unreadable_values. That
// matches production only after a run has recorded every such value, so tests
// that check finalize must run the runner first. Before that run, production
// counts such a value as waiting and finalize refuses.
func (c *fakeCopyStore) Count(_ context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	counts := orgencryption.CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 0, PlatformCopies: 0}
	for _, row := range c.store.rows {
		if row.organizationID != organizationID || !row.platform.Valid {
			continue
		}
		counts.PlatformCopies++
		if row.org.Valid {
			continue
		}
		if _, err := c.platform.Decrypt(row.platform.String); err != nil {
			counts.Unreadable++
		} else {
			counts.Waiting++
		}
	}

	return counts, nil
}

func (c *fakeCopyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (int64, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	fenced, err := c.fenced(ctx, organizationID, generation)
	if err != nil || fenced {
		return 0, err
	}

	var cleared int64
	for _, id := range c.sortedIDs(organizationID, "", func(row fakeRow) bool { return row.platform.Valid }) {
		if cleared == int64(limit) {
			break
		}
		row := c.store.rows[id]
		row.platform = pgtype.Text{String: "", Valid: false}
		c.store.rows[id] = row
		cleared++
	}

	return cleared, nil
}
