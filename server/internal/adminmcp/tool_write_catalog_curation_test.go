package adminmcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

type curationFake struct {
	candidate catalogpromotion.Candidate
	invalid   bool
	calls     atomic.Int32
}

func (f *curationFake) ValidateCurationTx(_ context.Context, _ pgx.Tx, org string, id uuid.UUID, revision, action string) (catalogpromotion.Candidate, error) {
	if !curationTransition(f.candidate.Status, action) || f.invalid || org != f.candidate.OrganizationID || id.String() != f.candidate.ID || revision != f.candidate.CurationRevision {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrStale
	}
	return f.candidate, nil
}
func (f *curationFake) CurateTx(_ context.Context, _ pgx.Tx, request catalogpromotion.CurationRequest, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	result := f.candidate
	result.Status = curationStatus(request.Action)
	result.CurationRevision = uuid.NewString()
	return result, nil
}
func TestCatalogCurationRequiresApprovalAndReplaysReceipt(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_curation")
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	mutator := &curationFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, CurationRevision: uuid.NewString(), Status: "candidate", ProjectID: project.ID.String()}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCurateRegistryPromotionCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogCurationWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test/admin-mcp", mutator: mutator, now: time.Now}
	input := PrepareCatalogCurationInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, CurationRevision: mutator.candidate.CurationRevision, RetryKey: "curate-example", Action: "reject"}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.Zero(t, mutator.calls.Load())
	replay, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, prepared.ProposalID, replay.ProposalID)
	require.True(t, replay.Replay)
	tools := newWriteTools(f.store, writes, w.baseURL, map[WriteOperation]operationWriter{OperationCurateRegistryPromotionCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	require.False(t, proposal.PlatformGlobal)
	require.True(t, proposal.Target.ProjectID.Valid)
	require.Equal(t, project.ID, proposal.Target.ProjectID.UUID)
	require.Equal(t, f.orgA, proposal.Target.OrganizationID)
	view, err := w.view(proposal)
	require.NoError(t, err)
	require.Contains(t, view.SideEffects, "until restored")
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
func TestCatalogCurationRejectsChangedStateAfterApproval(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_curation_stale")
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	mutator := &curationFake{candidate: catalogpromotion.Candidate{ID: uuid.NewString(), OrganizationID: f.orgA, CurationRevision: uuid.NewString(), Status: "candidate", ProjectID: project.ID.String()}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCurateRegistryPromotionCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogCurationWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, PrepareCatalogCurationInput{OrganizationID: f.orgA, PromotionID: mutator.candidate.ID, CurationRevision: mutator.candidate.CurationRevision, RetryKey: "curate", Action: "archive"})
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	mutator.candidate.CurationRevision = uuid.NewString()
	tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationCurateRegistryPromotionCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
	proposal.Target.OrganizationID = f.orgB
	_, err = storedCatalogCuration(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
}
func TestCatalogCurationRequiresWriteScope(t *testing.T) {
	t.Parallel()
	w := &catalogCurationWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCurateRegistryPromotionCandidate: true}}} //nolint:exhaustive // Only this operation is enabled.
	_, err := w.prepare(writePrincipalContext(t, []string{ScopeRead}), PrepareCatalogCurationInput{})
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
