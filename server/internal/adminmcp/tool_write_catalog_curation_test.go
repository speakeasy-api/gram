package adminmcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/stretchr/testify/require"
)

type curationFake struct {
	candidate catalogpromotion.Candidate
	fail      error
	calls     atomic.Int32
}

func (f *curationFake) ValidateCurationTx(_ context.Context, _ pgx.Tx, org string, id uuid.UUID, revision, action string) (catalogpromotion.Candidate, error) {
	if !curationTransition(f.candidate.Status, action) || org != f.candidate.OrganizationID || id.String() != f.candidate.ID || revision != f.candidate.CurationRevision {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrStale
	}
	return f.candidate, nil
}
func (f *curationFake) CurateTx(_ context.Context, _ pgx.Tx, request catalogpromotion.CurationRequest, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	if f.fail != nil {
		return catalogpromotion.Candidate{}, f.fail
	}
	if request.RetryKey == "" {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrInvalid
	}
	result := f.candidate
	result.Status = curationStatus(request.Action)
	result.CurationRevision = uuid.NewString()
	return result, nil
}
func newCurationWriter(f writeFixture) (*catalogCurationWriter, *curationFake, CatalogCurationInput) {
	mutator := &curationFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, CurationRevision: uuid.NewString(), Status: "candidate", ProjectID: uuid.NewString()}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCurateRegistryPromotionCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogCurationWriter{runner: f.runner, writes: writes, mutator: mutator}
	return w, mutator, CatalogCurationInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, CurationRevision: mutator.candidate.CurationRevision, Action: "reject"}
}

func TestCatalogCurationAppliesAndRecordsEvent(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_curation")
	w, mutator, input := newCurationWriter(f)
	receipt, err := w.apply(writeContext(t, f), input)
	require.NoError(t, err)
	require.Equal(t, mutator.candidate.ID, receipt.PromotionID)
	require.Equal(t, "rejected", receipt.Status)
	require.Contains(t, receipt.SideEffects, "until restored")
	require.Equal(t, int32(1), mutator.calls.Load())
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationCurateRegistryPromotionCandidate, f.orgA, writeEventExecuted))
}

func TestCatalogCurationRejectsChangedRevisionAndOtherTenant(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_curation_stale")
	w, mutator, input := newCurationWriter(f)
	ctx := writeContext(t, f)
	otherTenant := input
	otherTenant.OrganizationID = f.orgB
	_, err := w.apply(ctx, otherTenant)
	require.ErrorIs(t, err, ErrStaleState)
	mutator.candidate.CurationRevision = uuid.NewString()
	_, err = w.apply(ctx, input)
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationCurateRegistryPromotionCandidate, f.orgA, writeEventFailed))
}

func TestCatalogCurationHidesMutatorFailure(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_curation_failure")
	w, mutator, input := newCurationWriter(f)
	mutator.fail = errors.New("private database details")
	_, err := w.apply(writeContext(t, f), input)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private database details")
	require.Zero(t, countWriteEvents(t, f.db, OperationCurateRegistryPromotionCandidate, f.orgA, writeEventExecuted))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationCurateRegistryPromotionCandidate, f.orgA, writeEventFailed))
}

func TestCatalogCurationRequiresWriteScope(t *testing.T) {
	t.Parallel()
	w := &catalogCurationWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCurateRegistryPromotionCandidate: true}}} //nolint:exhaustive // Only this operation is enabled.
	_, err := w.apply(writePrincipalContext(t, []string{ScopeRead}), CatalogCurationInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}

func TestCatalogCurationRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status, action string
		allowed        bool
	}{
		{"candidate", "reject", true}, {"candidate", "archive", true}, {"rejected", "restore", true}, {"archived", "restore", true},
		{"promoted", "reject", false}, {"promoted", "archive", false}, {"promoted", "restore", false}, {"candidate", "restore", false}, {"rejected", "archive", false}, {"candidate", "delete", false},
	} {
		t.Run(tc.status+"_"+tc.action, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.allowed, curationTransition(tc.status, tc.action))
		})
	}
}
