package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogResetRestoresVisibleCatalogAndRetainsRecords(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return refreshTestBundle(basicRecord), nil })
	original, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "Demo", "Testing edit", 1)))
	require.NoError(t, err)
	local, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/testing", 1)))
	require.NoError(t, err)
	hidden, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/hidden", 1)))
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, hidden.ID, Token(hidden), false)
	require.NoError(t, err)
	r := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
	plan, err := r.Preview(ctx, "staff")
	require.NoError(t, err)
	require.Equal(t, "reset", plan.Operation)
	require.Equal(t, "local https://localhost:9090", plan.Destination)
	require.Contains(t, plan.Confirmation, plan.ID.String())
	require.Contains(t, plan.Confirmation, plan.Destination)
	require.Contains(t, plan.ScopeWarning, "independent preview")
	before, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Equal(t, original, before, "preview commits no catalog mutation")
	receipt, err := r.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	require.Equal(t, "completed", receipt.State)
	restored, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Equal(t, original.ID, restored.ID)
	require.True(t, restored.Published)
	require.Contains(t, string(restored.Data), `"description": "Demo"`)
	for _, entry := range []Entry{local, hidden} {
		got, err := s.Get(ctx, entry.ID)
		require.NoError(t, err)
		require.False(t, got.Published)
		require.Equal(t, entry.Data, got.Data)
	}
	status, err := r.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "reset", status.LastAttemptOperation)
	require.Equal(t, plan.SourceRevision, status.LastFullSuccessRevision)
	// A locally hidden production entry must be republished without replacing it.
	_, err = s.SetPublished(ctx, restored.ID, Token(restored), false)
	require.NoError(t, err)
	plan, err = r.Preview(ctx, "staff")
	require.NoError(t, err)
	require.Contains(t, plan.Changes, CatalogRefreshChange{Name: "example.test/demo", Action: "republish", Reason: "restore_production"})
	_, err = r.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	republished, err := s.Get(ctx, restored.ID)
	require.NoError(t, err)
	require.True(t, republished.Published)
	require.Equal(t, restored.Data, republished.Data)
}

func TestCatalogResetRequiresItsExactDestinationPlanBeforeReplay(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	offline := false
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		if offline {
			return CatalogBundle{}, errors.New("offline")
		}
		return refreshTestBundle(basicRecord), nil
	})
	refresh := NewCatalogRefresher(s, "local", "production", source, nil)
	reset := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
	other := NewCatalogResetter(s, "local", "production", "local https://localhost:9091", source, nil)
	old, err := refresh.Preview(ctx, "staff")
	require.NoError(t, err)
	_, err = reset.Apply(ctx, "staff", old.ID, "RESET local https://localhost:9090 "+old.ID.String())
	require.ErrorIs(t, err, ErrConflict)
	_, err = refresh.Apply(ctx, "staff", old.ID)
	require.NoError(t, err)
	_, err = reset.Apply(ctx, "staff", old.ID, "RESET local https://localhost:9090 "+old.ID.String())
	require.ErrorIs(t, err, ErrConflict)
	plan, err := reset.Preview(ctx, "staff")
	require.NoError(t, err)
	_, err = refresh.Apply(ctx, "staff", plan.ID)
	require.ErrorIs(t, err, ErrConflict)
	_, err = reset.Apply(ctx, "staff", plan.ID, "yes")
	require.ErrorIs(t, err, ErrConflict)
	_, err = other.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.ErrorIs(t, err, ErrConflict)
	_, err = reset.Apply(ctx, "different-staff", plan.ID, plan.Confirmation)
	require.ErrorIs(t, err, ErrNotFound)
	receipt, err := reset.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	offline = true
	replay, err := reset.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	_, err = refresh.Apply(ctx, "staff", plan.ID)
	require.ErrorIs(t, err, ErrConflict)
	_, err = other.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.ErrorIs(t, err, ErrConflict)
	_, err = reset.Apply(ctx, "staff", plan.ID, "yes")
	require.ErrorIs(t, err, ErrConflict)
}

func TestCatalogResetStructuralConflictsNeverClaimFullSuccess(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	original, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "https://example.test/mcp", "https://example.test/immutable", 1)))
	require.NoError(t, err)
	local, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/testing", 1)))
	require.NoError(t, err)
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return refreshTestBundle(basicRecord), nil })
	r := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
	plan, err := r.Preview(ctx, "staff")
	require.NoError(t, err)
	require.Contains(t, plan.Changes, CatalogRefreshChange{Name: "example.test/demo", Action: "conflict", Reason: "immutable_structure"})
	require.Contains(t, plan.Changes, CatalogRefreshChange{Name: "example.test/testing", Action: "unpublish", Reason: "retained_destination_only"})
	receipt, err := r.Apply(ctx, "staff", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	require.Equal(t, "completed_with_conflicts", receipt.State)
	retained, err := s.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, original, retained)
	hidden, err := s.Get(ctx, local.ID)
	require.NoError(t, err)
	require.False(t, hidden.Published)
	status, err := r.Status(ctx)
	require.NoError(t, err)
	require.Nil(t, status.LastFullSuccessAt)
	require.Equal(t, "completed_with_conflicts", status.LastAttemptState)
	require.Len(t, status.Conflicts, 1)
}
