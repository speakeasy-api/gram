package adminmcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/stretchr/testify/require"
)

type docsRetryFake struct {
	candidate catalogpromotion.Candidate
	calls     atomic.Int32
}

func (f *docsRetryFake) ValidateDocsRetryTx(_ context.Context, _ pgx.Tx, org string, id uuid.UUID, revision string) (catalogpromotion.Candidate, error) {
	if org != f.candidate.OrganizationID || id.String() != f.candidate.ID || revision != f.candidate.DocsRevision {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrStale
	}
	return f.candidate, nil
}
func (f *docsRetryFake) RetryDocsTx(_ context.Context, _ pgx.Tx, request catalogpromotion.DocsRetryRequest, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	if request.RetryKey == "" {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrInvalid
	}
	result := f.candidate
	result.DocsStatus = "retrying"
	result.DocsRevision = uuid.NewString()
	result.DocsRetryAllowed = false
	return result, nil
}
func newDocsRetryWriter(f writeFixture) (*catalogDocsRetryWriter, *docsRetryFake, CatalogDocsRetryInput) {
	mutator := &docsRetryFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, DocsRevision: uuid.NewString(), Status: "promoted", DocsStatus: "needs_attention", DocsRetryAllowed: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationRetryRegistryPromotionDocs: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogDocsRetryWriter{runner: f.runner, writes: writes, mutator: mutator}
	return w, mutator, CatalogDocsRetryInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, DocsRevision: mutator.candidate.DocsRevision}
}

func TestCatalogDocsRetryAppliesToOrphanPublication(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_docs_retry")
	w, mutator, input := newDocsRetryWriter(f)
	receipt, err := w.apply(writeContext(t, f), input)
	require.NoError(t, err)
	require.Equal(t, mutator.candidate.ID, receipt.PromotionID)
	require.Equal(t, "retrying", receipt.DocsStatus)
	require.Contains(t, receipt.SideEffects, "previously approved public metadata")
	require.Equal(t, int32(1), mutator.calls.Load())
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRetryRegistryPromotionDocs, f.orgA, writeEventExecuted))
}

func TestCatalogDocsRetryRejectsChangedDeliveryAndOtherTenant(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_docs_retry_stale")
	w, mutator, input := newDocsRetryWriter(f)
	ctx := writeContext(t, f)
	otherTenant := input
	otherTenant.OrganizationID = f.orgB
	_, err := w.apply(ctx, otherTenant)
	require.ErrorIs(t, err, ErrStaleState)
	mutator.candidate.DocsRevision = uuid.NewString()
	_, err = w.apply(ctx, input)
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
}

func TestCatalogDocsRetryRequiresWriteScope(t *testing.T) {
	t.Parallel()
	w := &catalogDocsRetryWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationRetryRegistryPromotionDocs: true}}} //nolint:exhaustive // Only this operation is enabled.
	_, err := w.apply(writePrincipalContext(t, []string{ScopeRead}), CatalogDocsRetryInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}

func TestCatalogPromotionProjectsDocsRetrySafely(t *testing.T) {
	t.Parallel()
	candidate := catalogpromotion.Candidate{ID: testRegistryID, OrganizationID: "org-example", SourceRevision: "aabbccdd", Status: "promoted", Reason: "already_promoted", RegistryEntryID: testRegistryID, DocsStatus: "needs_attention", DocsRevision: testRegistryID, DocsRetryAllowed: true, DocsAttempts: 8, DeliveryError: "docs_attempts_exhausted", PublicTitle: "Example public MCP", PromotedAt: time.Now().UTC().Format(time.RFC3339)}
	result, ok := projectCatalogPromotion(candidate)
	require.True(t, ok)
	require.Equal(t, candidate.DocsRevision, result.DocsRevision)
	require.True(t, result.DocsRetryAllowed)
	candidate.DocsStatus = "queued"
	_, ok = projectCatalogPromotion(candidate)
	require.False(t, ok)
	candidate.DocsRetryAllowed = false
	candidate.DocsQueuedAt = "raw unsafe text"
	_, ok = projectCatalogPromotion(candidate)
	require.False(t, ok)
}

func TestCatalogDocsRetryDistinguishesLookupAndStateErrors(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, docsRetryValidationError(catalogpromotion.ErrStale), ErrStaleState)
	require.ErrorIs(t, docsRetryValidationError(catalogpromotion.ErrConflict), ErrStaleState)
	missing := docsRetryValidationError(catalogpromotion.ErrNotFound)
	require.NotErrorIs(t, missing, ErrStaleState)
	require.ErrorContains(t, missing, "not found")
	unavailable := docsRetryValidationError(errors.New("private database details"))
	require.NotErrorIs(t, unavailable, ErrStaleState)
	require.ErrorContains(t, unavailable, "unavailable")
	require.NotContains(t, unavailable.Error(), "private database details")
}
