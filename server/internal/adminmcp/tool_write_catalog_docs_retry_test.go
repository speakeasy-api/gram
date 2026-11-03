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
	invalid   bool
	calls     atomic.Int32
}

func (f *docsRetryFake) ValidateDocsRetryTx(_ context.Context, _ pgx.Tx, org string, id uuid.UUID, revision string) (catalogpromotion.Candidate, error) {
	if f.invalid || org != f.candidate.OrganizationID || id.String() != f.candidate.ID || revision != f.candidate.DocsRevision {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrStale
	}
	return f.candidate, nil
}
func (f *docsRetryFake) RetryDocsTx(_ context.Context, _ pgx.Tx, _ catalogpromotion.DocsRetryRequest, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	result := f.candidate
	result.DocsStatus = "retrying"
	result.DocsRevision = uuid.NewString()
	result.DocsRetryAllowed = false
	return result, nil
}
func TestCatalogDocsRetryRequiresApprovalAndReplaysOrphanReceipt(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_docs_retry")
	mutator := &docsRetryFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, DocsRevision: uuid.NewString(), Status: "promoted", DocsStatus: "needs_attention", DocsRetryAllowed: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationRetryRegistryPromotionDocs: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogDocsRetryWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test/admin-mcp", mutator: mutator, now: time.Now}
	input := PrepareCatalogDocsRetryInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, DocsRevision: mutator.candidate.DocsRevision, RetryKey: "retry-guide"}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.Zero(t, mutator.calls.Load())
	replay, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, prepared.ProposalID, replay.ProposalID)
	require.True(t, replay.Replay)
	tools := newWriteTools(f.store, writes, w.baseURL, map[WriteOperation]operationWriter{OperationRetryRegistryPromotionDocs: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	require.False(t, proposal.PlatformGlobal)
	require.False(t, proposal.Target.ProjectID.Valid)
	require.Equal(t, f.orgA, proposal.Target.OrganizationID)
	view, err := w.view(proposal)
	require.NoError(t, err)
	require.Contains(t, view.SideEffects, "previously approved public metadata")
	_, err = f.store.Approve(ctx, proposal.ID, "user:other-staff", proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.Error(t, err)
	_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, int32(1), mutator.calls.Load())
}
func TestCatalogDocsRetryRejectsChangedDeliveryAfterApproval(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_docs_retry_stale")
	mutator := &docsRetryFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, DocsRevision: uuid.NewString(), Status: "promoted", DocsStatus: "needs_attention", DocsRetryAllowed: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationRetryRegistryPromotionDocs: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogDocsRetryWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, PrepareCatalogDocsRetryInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, DocsRevision: mutator.candidate.DocsRevision, RetryKey: "retry"})
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	mutator.candidate.DocsRevision = uuid.NewString()
	tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationRetryRegistryPromotionDocs: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
	proposal.Target.OrganizationID = f.orgB
	_, err = storedCatalogDocsRetry(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
}
func TestCatalogDocsRetryRequiresWriteScope(t *testing.T) {
	t.Parallel()
	w := &catalogDocsRetryWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationRetryRegistryPromotionDocs: true}}} //nolint:exhaustive // Only this operation is enabled.
	_, err := w.prepare(writePrincipalContext(t, []string{ScopeRead}), PrepareCatalogDocsRetryInput{})
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
