package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

type refreshSourceFunc func(context.Context) (CatalogBundle, error)

func (f refreshSourceFunc) Fetch(ctx context.Context) (CatalogBundle, error) { return f(ctx) }
func refreshTestBundle(records ...string) CatalogBundle {
	b := CatalogBundle{Version: CatalogBundleVersion, Entries: []json.RawMessage{}}
	for _, raw := range records {
		b.Entries = append(b.Entries, json.RawMessage(raw))
	}
	return b
}
func TestCatalogRefreshRealPath(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	upstream := refreshTestBundle(basicRecord)
	unavailable := false
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		if unavailable {
			return CatalogBundle{}, errors.New("offline")
		}
		return upstream, nil
	})
	r := NewCatalogRefresher(s, "local", "production", source, nil)
	imported, err := r.Import(ctx, "staff-owner")
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/demo"}, imported.Created)
	before, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	changed := strings.Replace(basicRecord, "Demo", "Updated demo", 1)
	require.NotEqual(t, basicRecord, changed)
	upstream = refreshTestBundle(changed)
	plan, err := r.Preview(ctx, "staff-one")
	require.NoError(t, err)
	require.Equal(t, "update", plan.Changes[0].Action)
	_, err = r.Apply(ctx, "staff-two", plan.ID)
	require.ErrorIs(t, err, ErrNotFound)
	receipt, err := r.Apply(ctx, "staff-one", plan.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", receipt.State)
	after, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.Contains(t, string(after.Data), "Updated demo")
	unavailable = true
	replay, err := r.Apply(ctx, "staff-one", plan.ID)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	unavailable = false
	upstream = refreshTestBundle()
	plan, err = r.Preview(ctx, "staff-one")
	require.NoError(t, err)
	require.Equal(t, "unpublish", plan.Changes[0].Action)
	_, err = r.Apply(ctx, "staff-one", plan.ID)
	require.NoError(t, err)
	after, err = s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.False(t, after.Published)
	upstream = refreshTestBundle(changed)
	plan, err = r.Preview(ctx, "staff-one")
	require.NoError(t, err)
	require.Equal(t, "republish", plan.Changes[0].Action)
	_, err = r.Apply(ctx, "staff-one", plan.ID)
	require.NoError(t, err)
	resurrected, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.True(t, resurrected.Published)
	require.Equal(t, before.ID, resurrected.ID)

}
func TestCatalogRefreshStaleAndConflictSubset(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	upstream := refreshTestBundle(basicRecord)
	r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return upstream, nil }), nil)
	_, err := r.Import(ctx, "staff-owner")
	require.NoError(t, err)
	original, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	local := strings.Replace(string(original.Data), "Demo", "Local", 1)
	_, err = s.Save(ctx, original.ID, Token(original), json.RawMessage(local))
	require.NoError(t, err)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "preserved", plan.Changes[0].Action)
	upstream = refreshTestBundle(strings.Replace(basicRecord, "Demo", "Upstream", 1), strings.Replace(basicRecord, "example.test/demo", "example.test/new", 1))
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "conflict", plan.Changes[0].Action)
	require.Equal(t, "create", plan.Changes[1].Action)
	statusBefore, err := r.Status(ctx)
	require.NoError(t, err)
	receipt, err := r.Apply(ctx, "owner", plan.ID)
	require.NoError(t, err)
	require.Equal(t, "completed_with_conflicts", receipt.State)
	statusAfter, err := r.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, statusBefore.LastFullSuccessAt, statusAfter.LastFullSuccessAt)
	after, err := s.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Contains(t, string(after.Data), "Local")
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "conflict", plan.Changes[0].Action, "conflict baseline must not advance")
	_, err = s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/another", 1)))
	require.NoError(t, err)
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.ErrorIs(t, err, ErrConflict)
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	upstream = refreshTestBundle(basicRecord)
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.ErrorIs(t, err, ErrConflict)
}
func TestCatalogRefreshAdoptionAndStructure(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	_, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	upstream := refreshTestBundle(basicRecord)
	r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return upstream, nil }), nil)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "adopt", plan.Changes[0].Action)
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.NoError(t, err)
	upstream = refreshTestBundle(strings.Replace(basicRecord, "https://example.test/mcp", "https://changed.test/mcp", 1))
	require.NotEqual(t, basicRecord, string(upstream.Entries[0]))
	plan, err = r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "conflict", plan.Changes[0].Action)
	require.Equal(t, "immutable_structure", plan.Changes[0].Reason)
}
func TestCatalogRefreshExpiredPlan(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		t.Fatal("expired plan must not fetch source")
		return CatalogBundle{}, nil
	}), nil)
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: exercise transactional plan persistence with an expired fixture.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	id := uuid.New()
	require.NoError(t, refreshSavePlan(ctx, tx, id, "production", "owner", time.Now().Add(-time.Minute), json.RawMessage(`{}`)))
	require.NoError(t, tx.Commit(ctx))
	_, err = r.Apply(ctx, "owner", id)
	require.ErrorContains(t, err, "expired")
}

func TestCatalogRefreshSafeSubsetMappingConflict(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	_, err := s.Create(ctx, json.RawMessage(oktaRecord("example.test/existing", "shared")))
	require.NoError(t, err)
	upstream := refreshTestBundle(oktaRecord("example.test/conflict", "shared"), strings.Replace(basicRecord, "example.test/demo", "example.test/safe", 1))
	r := NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return upstream, nil }), nil)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, []CatalogRefreshChange{{Name: "example.test/conflict", Action: "conflict", Reason: "validation_conflict"}, {Name: "example.test/existing", Action: "preserved", Reason: "destination_only"}, {Name: "example.test/safe", Action: "create"}}, plan.Changes)
	_, err = s.GetByName(ctx, "example.test/safe")
	require.ErrorIs(t, err, ErrNotFound)
	receipt, err := r.Apply(ctx, "owner", plan.ID)
	require.NoError(t, err)
	require.Equal(t, "completed_with_conflicts", receipt.State)
	_, err = s.GetByName(ctx, "example.test/safe")
	require.NoError(t, err)
	_, err = s.GetByName(ctx, "example.test/conflict")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCatalogRefreshRejectsUntrustedOrInvalidSnapshot(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	r := NewCatalogRefresher(s, "production", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		t.Fatal("must reject destination before fetching")
		return CatalogBundle{}, nil
	}), nil)
	_, err := r.Preview(ctx, "owner")
	require.ErrorContains(t, err, "destination")
	r = NewCatalogRefresher(s, "local", "production", refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return refreshTestBundle(basicRecord, `{}`), nil }), nil)
	_, err = r.Preview(ctx, "owner")
	require.ErrorContains(t, err, "snapshot is invalid")
	_, err = s.GetByName(ctx, "example.test/demo")
	require.ErrorIs(t, err, ErrNotFound)
}
