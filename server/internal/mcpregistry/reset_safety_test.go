package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCatalogResetRejectsStaleAndUnavailableSourceWithoutWrites(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"source", "destination", "unavailable", "invalid_export"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := newTestService(t)
			local, err := s.Create(ctx, json.RawMessage(strings.Replace(basicRecord, "example.test/demo", "example.test/testing", 1)))
			require.NoError(t, err)
			bundle := refreshTestBundle(basicRecord)
			offline := false
			source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
				if offline {
					return CatalogBundle{}, errors.New("offline")
				}
				return bundle, nil
			})
			r := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
			plan, err := r.Preview(ctx, "owner")
			require.NoError(t, err)
			switch kind {
			case "source":
				bundle = refreshTestBundle(strings.Replace(basicRecord, "Demo", "Upstream changed", 1))
			case "destination":
				_, err = s.Save(ctx, local.ID, Token(local), json.RawMessage(strings.Replace(string(local.Data), "Demo", "Concurrent edit", 1)))
				require.NoError(t, err)
			case "unavailable":
				offline = true
			case "invalid_export":
				bundle.Version = 0
			}
			_, err = r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
			require.Error(t, err)
			got, err := s.Get(ctx, local.ID)
			require.NoError(t, err)
			require.True(t, got.Published)
			_, err = s.GetByName(ctx, "example.test/demo")
			require.ErrorIs(t, err, ErrNotFound)
			status, err := r.Status(ctx)
			require.NoError(t, err)
			require.Nil(t, status.LastFullSuccessAt)
			if kind == "unavailable" {
				offline = false
				receipt, err := r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
				require.NoError(t, err)
				require.Equal(t, "completed", receipt.State)
			}
		})
	}
}

func TestCatalogResetRejectsProductionAndMissingDestination(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		t.Fatal("rejected target must not fetch source")
		return CatalogBundle{}, nil
	})
	for _, environment := range []string{"prod", "production", "staging", ""} {
		r := NewCatalogResetter(s, environment, "production", "configured destination", source, nil)
		_, err := r.Preview(ctx, "owner")
		require.Error(t, err)
		_, err = r.Apply(ctx, "owner", uuid.New(), "yes")
		require.Error(t, err)
	}
	r := NewCatalogResetter(s, "local", "production", "", source, nil)
	_, err := r.Preview(ctx, "owner")
	require.Error(t, err)
}

func TestCatalogResetExpiredAndLegacyPlanIsolation(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		t.Fatal("rejected plan must not fetch source")
		return CatalogBundle{}, nil
	})
	reset := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
	refresh := NewCatalogRefresher(s, "local", "production", source, nil)
	for _, legacy := range []bool{false, true} {
		id := uuid.New()
		public := CatalogRefreshPlan{ID: id, Operation: "reset", Destination: "local https://localhost:9090", Confirmation: resetConfirmation("local https://localhost:9090", id)}
		if legacy {
			public = CatalogRefreshPlan{ID: id}
		}
		raw, err := json.Marshal(refreshStoredPlan{Public: public})
		require.NoError(t, err)
		tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: persist legacy/expired plan fixtures via production storage helpers.
		require.NoError(t, err)
		expiry := time.Now().Add(time.Hour)
		if !legacy {
			expiry = time.Now().Add(-time.Minute)
		}
		require.NoError(t, refreshSavePlan(ctx, tx, id, "production", "owner", expiry, raw))
		require.NoError(t, tx.Commit(ctx))
		_, err = reset.Apply(ctx, "owner", id, resetConfirmation("local https://localhost:9090", id))
		require.ErrorIs(t, err, ErrConflict)
		if !legacy {
			require.ErrorContains(t, err, "expired")
			continue
		}
		// Old receipts have no operation/destination. They remain refresh-only after
		// their large plan payload is pruned by receipt persistence.
		receipt := CatalogRefreshReceipt{PlanID: id, State: "completed", CompletedAt: time.Now().UTC()}
		raw, err = json.Marshal(receipt)
		require.NoError(t, err)
		tx, err = db.Begin(ctx) //nolint:glint // notestingrawsql: exercise legacy compact receipt replay compatibility.
		require.NoError(t, err)
		require.NoError(t, refreshSaveReceipt(ctx, tx, id, raw))
		require.NoError(t, tx.Commit(ctx))
		_, err = reset.Apply(ctx, "owner", id, resetConfirmation("local https://localhost:9090", id))
		require.ErrorIs(t, err, ErrConflict)
		replay, err := refresh.Apply(ctx, "owner", id)
		require.NoError(t, err)
		require.Equal(t, receipt.PlanID, replay.PlanID)
	}
}

func TestCatalogResetConcurrentApplyReplaysOneReceipt(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	source := refreshSourceFunc(func(context.Context) (CatalogBundle, error) { return refreshTestBundle(basicRecord), nil })
	r := NewCatalogResetter(s, "local", "production", "local https://localhost:9090", source, nil)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	type result struct {
		receipt CatalogRefreshReceipt
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			receipt, err := r.Apply(ctx, "owner", plan.ID, plan.Confirmation)
			results <- result{receipt, err}
		}()
	}
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.receipt, second.receipt)
	_, err = s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
}
