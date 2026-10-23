package adminmcp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

type promotionMutatorFake struct {
	candidate catalogpromotion.Candidate
	invalid   bool
	calls     atomic.Int32
}

func (f *promotionMutatorFake) GetCandidate(context.Context, catalogpromotion.Target) (catalogpromotion.Candidate, error) {
	return f.candidate, nil
}

func (f *promotionMutatorFake) ValidateTx(_ context.Context, _ pgx.Tx, target catalogpromotion.Target, revision string, _ catalogpromotion.PublicMetadata) (catalogpromotion.Candidate, error) {
	if f.invalid || revision != f.candidate.SourceRevision || target.OrganizationID != f.candidate.OrganizationID || target.ProjectID.String() != f.candidate.ProjectID || target.MCPServerID.String() != f.candidate.MCPServerID {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrStale
	}
	return f.candidate, nil
}

func (f *promotionMutatorFake) PromoteTx(_ context.Context, _ pgx.Tx, _ catalogpromotion.Request, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	out := f.candidate
	out.Status = "promoted"
	out.ID = uuid.NewString()
	out.RegistryEntryID = uuid.NewString()
	return out, nil
}

func TestCatalogPromotionApprovalAndReplay(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_promotion_approval")
	seededProject, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	project, server := seededProject.ID.String(), uuid.NewString()
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test/admin-mcp", mutator: mutator, now: time.Now}
	input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", PublicMetadataConfirmed: true, RetryKey: "publish-example", Public: catalogpromotion.PublicMetadata{Name: "com.example/mcp", Title: "Example", Description: "Public example server", URL: "https://mcp.example.test/mcp", DocumentationURL: "https://example.test/docs", DocsSlug: "example", AuthMode: "oauth"}}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.Zero(t, mutator.calls.Load())
	replayed, err := w.prepare(ctx, input)
	require.NoError(t, err)
	require.True(t, replayed.Replay)
	require.Equal(t, prepared.ProposalID, replayed.ProposalID)
	tools := newWriteTools(f.store, writes, w.baseURL, map[WriteOperation]operationWriter{OperationPromoteRegistryCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	p, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, p.ID, "user:other-staff", p.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.Error(t, err)
	view, err := w.view(p)
	require.NoError(t, err)
	require.Contains(t, view.SideEffects, "setup-docs")
	require.Equal(t, f.orgA, view.OrganizationID)
	_, err = f.store.Approve(ctx, p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, int32(1), mutator.calls.Load())
}

func TestCatalogPromotionRejectsChangedHealthAtExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_promotion_stale")
	seededProject, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	project, server := seededProject.ID.String(), uuid.NewString()
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	prepared, err := w.prepare(writeContext(t, f), PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", PublicMetadataConfirmed: true, RetryKey: "stale"})
	require.NoError(t, err)
	p, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	mutator.invalid = true
	tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationPromoteRegistryCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: p.ID.String()})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
}

func TestCatalogPromotionRejectsCrossTenantStoredTarget(t *testing.T) {
	t.Parallel()
	request := catalogpromotion.Request{Target: catalogpromotion.Target{OrganizationID: "org_example", ProjectID: uuid.New(), MCPServerID: uuid.New()}, SourceRevision: "source-v1", PublicMetadataConfirmed: true, RetryKey: "test"}
	args, err := json.Marshal(request)
	require.NoError(t, err)
	digest, err := stateDigest(args)
	require.NoError(t, err)
	proposal := Proposal{Operation: OperationPromoteRegistryCandidate, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: "org_other", ProjectID: uuid.NullUUID{UUID: request.Target.ProjectID, Valid: true}, ResourceKind: "mcp_server", ResourceID: request.Target.MCPServerID.String()}, Arguments: args, ExpectedStateDigest: digest, IdempotencyKey: "test"}
	_, err = storedCatalogPromotion(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
}

func TestCatalogPromotionRequiresPublicConfirmationAndWriteScope(t *testing.T) {
	t.Parallel()
	_, err := catalogPromotionRequest(PrepareCatalogPromotionInput{OrganizationID: "org_example", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", RetryKey: "test"})
	require.Error(t, err)
	w := &catalogPromotionWriter{writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}}} //nolint:exhaustive // Only this operation is enabled.
	_, err = w.prepare(writePrincipalContext(t, []string{ScopeRead}), PrepareCatalogPromotionInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}
