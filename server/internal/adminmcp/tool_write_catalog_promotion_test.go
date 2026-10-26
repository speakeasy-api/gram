package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
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
	candidate     catalogpromotion.Candidate
	invalid       bool
	validationErr error
	calls         atomic.Int32
}

func (f *promotionMutatorFake) GetCandidate(context.Context, catalogpromotion.Target) (catalogpromotion.Candidate, error) {
	return f.candidate, nil
}

func (f *promotionMutatorFake) ValidateTx(_ context.Context, _ pgx.Tx, target catalogpromotion.Target, revision, curationRevision string, _ catalogpromotion.PublicMetadata, override bool) (catalogpromotion.Candidate, error) {
	if !f.candidate.Eligible && (!override || !catalogpromotion.CanOverrideEligibility(f.candidate.Reason)) {
		return catalogpromotion.Candidate{}, catalogpromotion.ErrIneligible
	}
	if f.validationErr != nil {
		return catalogpromotion.Candidate{}, f.validationErr
	}
	if f.invalid || curationRevision != f.candidate.CurationRevision || f.candidate.Status != "candidate" || revision != f.candidate.SourceRevision || target.OrganizationID != f.candidate.OrganizationID || target.ProjectID.String() != f.candidate.ProjectID || target.MCPServerID.String() != f.candidate.MCPServerID {
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
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test/admin-mcp", mutator: mutator, now: time.Now}
	input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "publish-example", Public: catalogpromotion.PublicMetadata{Name: "com.example/mcp", Title: "Example", Description: "Public example server", URL: "https://mcp.example.test/mcp", DocumentationURL: "https://example.test/docs", DocsSlug: "example", AuthMode: "oauth"}}
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
	require.Contains(t, view.Changes, proposalViewChange{Setting: "Allow health eligibility override", Before: "", After: "No"})
	stored, err := storedCatalogPromotion(p)
	require.NoError(t, err)
	require.False(t, stored.OverrideEligibility)
	expected, err := catalogPromotionRequest(input)
	require.NoError(t, err)
	args, err := json.Marshal(expected)
	require.NoError(t, err)
	require.JSONEq(t, string(args), string(p.Arguments))
	require.JSONEq(t, string(args), string(p.Preview))
	digest, err := stateDigest(args)
	require.NoError(t, err)
	require.Equal(t, digest, p.ExpectedStateDigest)

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
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	prepared, err := w.prepare(writeContext(t, f), PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "stale"})
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
	request := catalogpromotion.Request{Target: catalogpromotion.Target{OrganizationID: "org_example", ProjectID: uuid.New(), MCPServerID: uuid.New()}, SourceRevision: "source-v1", CurationRevision: uuid.NewString(), PublicMetadataConfirmed: true, RetryKey: "test"}
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

func TestCatalogPromotionApprovalSurvivesValidationOutage(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_promotion_outage")
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	ctx := writeContext(t, f)
	input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: mutator.candidate.MCPServerID, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "outage"}
	prepared, err := w.prepare(ctx, input)
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	for _, outage := range []error{catalogpromotion.ErrUnavailable, errors.New("private database details")} {
		mutator.validationErr = outage
		_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrStaleState)
		require.NotContains(t, err.Error(), "private database details")
		persisted, readErr := f.store.GetForOwner(ctx, proposal.ID, f.owner)
		require.NoError(t, readErr)
		require.Equal(t, ProposalPendingApproval, persisted.Status)
	}
	mutator.validationErr = nil
	approved, err := f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, approved.Status)
	input.RetryKey = "stale-source"
	prepared, err = w.prepare(ctx, input)
	require.NoError(t, err)
	proposal, err = f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	mutator.invalid = true
	_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	persisted, err := f.store.GetForOwner(ctx, proposal.ID, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, persisted.Status)
}

func TestCatalogPromotionRejectsApprovalAfterCurationCycle(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_promotion_curation_cycle")
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: true}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "", mutator: mutator, now: time.Now}
	ctx := writeContext(t, f)
	prepared, err := w.prepare(ctx, PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: mutator.candidate.MCPServerID, SourceRevision: mutator.candidate.SourceRevision, CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "review-cycle"})
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
	require.NoError(t, err)
	// Reject then restore leaves the source configuration unchanged but replaces its review revision.
	mutator.candidate.CurationRevision = uuid.NewString()
	tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationPromoteRegistryCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
}

func TestCatalogPromotionOverrideApprovalAndReplay(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "catalog_promotion_override")
	seededProject, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
	require.NoError(t, err)
	project, server := seededProject.ID.String(), uuid.NewString()
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: false, Reason: "errors_observed"}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
	w := &catalogPromotionWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test/admin-mcp", mutator: mutator, now: time.Now}
	input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "publish-example", Public: catalogpromotion.PublicMetadata{Name: "com.example/mcp", Title: "Example", Description: "Public example server", URL: "https://mcp.example.test/mcp", DocumentationURL: "https://example.test/docs", DocsSlug: "example", AuthMode: "oauth"}}
	ctx := writeContext(t, f)
	_, err = w.prepare(ctx, input)
	require.ErrorIs(t, err, ErrStaleState)
	input.OverrideEligibility = true
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
	require.Contains(t, view.Changes, proposalViewChange{Setting: "Allow health eligibility override", Before: "", After: "Yes — allow promotion if health evidence does not meet the criteria"})
	stored, err := storedCatalogPromotion(p)
	require.NoError(t, err)
	require.True(t, stored.OverrideEligibility)
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

func TestCatalogPromotionOverrideReplayPreservesConsentAcrossHealthChanges(t *testing.T) {
	t.Parallel()
	for _, initiallyEligible := range []bool{true, false} {
		name := "becomes eligible"
		if initiallyEligible {
			name = "becomes ineligible"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "catalog_promotion_health_replay")
			project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
			require.NoError(t, err)
			reason := "errors_observed"
			if initiallyEligible {
				reason = "eligible"
			}
			mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: initiallyEligible, Reason: reason}}
			writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
			w := &catalogPromotionWriter{store: f.store, writes: writes, mutator: mutator, now: time.Now}
			ctx := writeContext(t, f)
			input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: mutator.candidate.MCPServerID, SourceRevision: mutator.candidate.SourceRevision, CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, OverrideEligibility: true, RetryKey: "stable-consent"}
			prepared, err := w.prepare(ctx, input)
			require.NoError(t, err)
			original, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
			require.NoError(t, err)
			stored, err := storedCatalogPromotion(original)
			require.NoError(t, err)
			require.True(t, stored.OverrideEligibility)
			view, err := w.view(original)
			require.NoError(t, err)
			require.Contains(t, view.Changes, proposalViewChange{Setting: "Allow health eligibility override", After: "Yes — allow promotion if health evidence does not meet the criteria"})
			_, err = f.store.Approve(ctx, original.ID, f.owner.SubjectURN, original.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
			require.NoError(t, err)
			mutator.candidate.Eligible = !initiallyEligible
			mutator.candidate.Reason = "errors_observed"
			if mutator.candidate.Eligible {
				mutator.candidate.Reason = "eligible"
			}
			replayed, err := w.prepare(ctx, input)
			require.NoError(t, err)
			require.True(t, replayed.Replay)
			require.Equal(t, prepared.ProposalID, replayed.ProposalID)
			persisted, err := f.store.GetForOwner(ctx, original.ID, f.owner)
			require.NoError(t, err)
			require.Equal(t, ProposalApproved, persisted.Status)
			require.Equal(t, original.Arguments, persisted.Arguments)
			require.Equal(t, original.Preview, persisted.Preview)
			require.Equal(t, original.ExpectedStateDigest, persisted.ExpectedStateDigest)
			require.Equal(t, original.ProposalDigest, persisted.ProposalDigest)
			replayView, err := w.view(persisted)
			require.NoError(t, err)
			require.Equal(t, view, replayView)
			// A healthy source permits both inputs through validation, so the stored consent must reject the changed request.
			mutator.candidate.Eligible = true
			mutator.candidate.Reason = "eligible"
			input.OverrideEligibility = false
			_, err = w.prepare(ctx, input)
			require.ErrorIs(t, err, ErrProposalConflict)
		})
	}
}

func TestCatalogPromotionLengthErrorsGiveSafeFieldGuidance(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{catalogpromotion.ErrPublicTitleTooLong, catalogpromotion.ErrPublicDescriptionTooLong} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			err := catalogPromotionValidationError(errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value")))
			require.EqualError(t, err, cause.Error())
			require.NotErrorIs(t, err, ErrStaleState)
		})
	}
}

func TestCatalogPromotionLengthErrorsInvalidateStoredProposals(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{catalogpromotion.ErrPublicTitleTooLong, catalogpromotion.ErrPublicDescriptionTooLong} {
		for _, atExecution := range []bool{false, true} {
			stage := "approval"
			if atExecution {
				stage = "execution"
			}
			t.Run(stage+"/"+cause.Error(), func(t *testing.T) {
				t.Parallel()
				f := newProposalFixture(t, "catalog_promotion_length")
				project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{OrganizationID: f.orgA, Name: "Example", Slug: "example"})
				require.NoError(t, err)
				mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: true}}
				writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPromoteRegistryCandidate: true}} //nolint:exhaustive // Only this operation is enabled.
				w := &catalogPromotionWriter{store: f.store, writes: writes, mutator: mutator, now: time.Now}
				ctx := writeContext(t, f)
				input := PrepareCatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project.ID.String(), MCPServerID: mutator.candidate.MCPServerID, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, RetryKey: "length-validation"}
				mutator.validationErr = errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value"))
				_, err = w.prepare(ctx, input)
				require.EqualError(t, err, cause.Error())

				// A stored proposal may have passed validation under an earlier metadata policy.
				mutator.validationErr = nil
				prepared, err := w.prepare(ctx, input)
				require.NoError(t, err)
				proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
				require.NoError(t, err)
				tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationPromoteRegistryCandidate: w}) //nolint:exhaustive // Only this operation is enabled.
				if atExecution {
					_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
					require.NoError(t, err)
				}
				mutator.validationErr = errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value"))
				if atExecution {
					_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
				} else {
					_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, w.revalidate)
				}
				require.ErrorIs(t, err, ErrStaleState)
				require.NotContains(t, err.Error(), "private rejected value")
				persisted, err := f.store.GetForOwner(ctx, proposal.ID, f.owner)
				require.NoError(t, err)
				require.Equal(t, ProposalInvalidated, persisted.Status)
				require.Equal(t, reasonStaleState, persisted.InvalidationReason)
				require.Zero(t, mutator.calls.Load())
				mutator.validationErr = nil
				_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
				require.ErrorIs(t, err, ErrProposalInvalidated)
				require.Zero(t, mutator.calls.Load())
			})
		}
	}
}
