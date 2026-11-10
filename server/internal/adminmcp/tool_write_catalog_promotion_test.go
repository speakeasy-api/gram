package adminmcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/catalogauth"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/setupguides"
	"github.com/stretchr/testify/require"
)

type promotionMutatorFake struct {
	candidate     catalogpromotion.Candidate
	invalid       bool
	validationErr error
	calls         atomic.Int32
	promoted      catalogpromotion.Request
}

func (f *promotionMutatorFake) DiscoverAuth(context.Context, string, string) catalogauth.Discovery {
	if f.candidate.AuthDiscovery != nil {
		return *f.candidate.AuthDiscovery
	}
	return catalogauth.Discovery{}
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

func (f *promotionMutatorFake) PromoteTx(_ context.Context, _ pgx.Tx, request catalogpromotion.Request, _ string) (catalogpromotion.Candidate, error) {
	f.calls.Add(1)
	f.promoted = request
	out := f.candidate
	out.Status = "promoted"
	out.ID = uuid.NewString()
	out.RegistryEntryID = uuid.NewString()
	return out, nil
}

func newPromotionWriter(f writeFixture, eligible bool, reason string) (*catalogPromotionWriter, *promotionMutatorFake, CatalogPromotionInput) {
	project, server := uuid.NewString(), uuid.NewString()
	mutator := &promotionMutatorFake{candidate: catalogpromotion.Candidate{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: uuid.NewString(), Status: "candidate", Eligible: eligible, Reason: reason}}
	writes := WriteConfig{Enabled: true}
	w := &catalogPromotionWriter{runner: f.runner, writes: writes, mutator: mutator}
	input := CatalogPromotionInput{OrganizationID: f.orgA, ProjectID: project, MCPServerID: server, SourceRevision: "source-v1", CurationRevision: mutator.candidate.CurationRevision, PublicMetadataConfirmed: true, Public: catalogpromotion.PublicMetadata{Name: "com.example/mcp", Title: "Example", Description: "Public example server", URL: "https://mcp.example.test/mcp", DocumentationURL: "https://example.test/docs", DocsSlug: "example", AuthMode: "oauth"}}
	return w, mutator, input
}

func TestCatalogPromotionPublishesAndRecordsEvent(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_promotion")
	w, mutator, input := newPromotionWriter(f, true, "eligible")
	evidence := catalogauth.Discovery{SupportsCIMD: true, AuthSetup: "cimd", Status: "complete"}
	mutator.candidate.AuthDiscovery = &evidence
	receipt, err := w.apply(writeContext(t, f), input)
	require.NoError(t, err)
	require.NotEmpty(t, receipt.PromotionID)
	require.NotEmpty(t, receipt.RegistryEntryID)
	require.Contains(t, receipt.SideEffects, "setup-docs")
	require.Equal(t, setupGuideNote("example"), receipt.SetupGuide)
	require.Equal(t, int32(1), mutator.calls.Load())
	require.Equal(t, evidence, mutator.promoted.AuthDiscovery, "promotion records the discovered capabilities")
	require.False(t, mutator.promoted.OverrideEligibility)
	require.NotEmpty(t, mutator.promoted.RetryKey, "the promotion service requires a retry key")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationPromoteRegistryCandidate, f.orgA, writeEventExecuted))
}

func TestCatalogPromotionSetupGuideNoteWarnsWhenTheSlugIsTaken(t *testing.T) {
	t.Parallel()
	taken := setupGuideNote("slack")
	require.Contains(t, taken, "already uses this slug")
	require.Contains(t, taken, "Slack")
	require.Contains(t, setupGuideNote("example-mcp"), "None in the setup guides released to Speakeasy ("+setupguides.PinnedVersion+")")
}

func TestCatalogPromotionRejectsDriftAndOtherTenant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(f writeFixture, mutator *promotionMutatorFake, input *CatalogPromotionInput)
	}{
		{"validation finds the source changed", func(_ writeFixture, mutator *promotionMutatorFake, _ *CatalogPromotionInput) {
			mutator.invalid = true
		}},
		{"curation cycle replaced the review revision", func(_ writeFixture, mutator *promotionMutatorFake, _ *CatalogPromotionInput) {
			mutator.candidate.CurationRevision = uuid.NewString()
		}},
		{"source revision moved", func(_ writeFixture, mutator *promotionMutatorFake, _ *CatalogPromotionInput) {
			mutator.candidate.SourceRevision = "source-v2"
		}},
		{"other organization", func(f writeFixture, _ *promotionMutatorFake, input *CatalogPromotionInput) {
			input.OrganizationID = f.orgB
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newWriteFixture(t, "catalog_promotion_stale")
			w, mutator, input := newPromotionWriter(f, true, "eligible")
			tc.change(f, mutator, &input)
			_, err := w.apply(writeContext(t, f), input)
			require.ErrorIs(t, err, ErrStaleState)
			require.Zero(t, mutator.calls.Load())
		})
	}
}

func TestCatalogPromotionRequiresPublicConfirmationAndWriteScope(t *testing.T) {
	t.Parallel()
	_, err := catalogPromotionRequest(CatalogPromotionInput{OrganizationID: "org_example", ProjectID: uuid.NewString(), MCPServerID: uuid.NewString(), SourceRevision: "source-v1", CurationRevision: uuid.NewString()})
	require.Error(t, err)
	w := &catalogPromotionWriter{writes: WriteConfig{Enabled: true}}
	_, err = w.apply(writePrincipalContext(t, []string{ScopeRead}), CatalogPromotionInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}

func TestCatalogPromotionValidationOutageIsRetryableAndPrivate(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_promotion_outage")
	w, mutator, input := newPromotionWriter(f, true, "eligible")
	ctx := writeContext(t, f)
	outages := []error{catalogpromotion.ErrUnavailable, errors.New("private database details")}
	for _, outage := range outages {
		mutator.validationErr = outage
		_, err := w.apply(ctx, input)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrStaleState)
		require.NotContains(t, err.Error(), "private database details")
	}
	require.Zero(t, mutator.calls.Load())
	require.Equal(t, len(outages), countWriteEvents(t, f.db, OperationPromoteRegistryCandidate, f.orgA, writeEventFailed))
	mutator.validationErr = nil
	_, err := w.apply(ctx, input)
	require.NoError(t, err)
	require.Equal(t, int32(1), mutator.calls.Load())
}

func TestCatalogPromotionOverrideAllowsIneligibleSource(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "catalog_promotion_override")
	w, mutator, input := newPromotionWriter(f, false, "errors_observed")
	ctx := writeContext(t, f)
	_, err := w.apply(ctx, input)
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, mutator.calls.Load())
	input.OverrideEligibility = true
	_, err = w.apply(ctx, input)
	require.NoError(t, err)
	require.Equal(t, int32(1), mutator.calls.Load())
	require.True(t, mutator.promoted.OverrideEligibility)
}

func TestCatalogPromotionLengthErrorsGiveSafeFieldGuidance(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{catalogpromotion.ErrPublicTitleTooLong, catalogpromotion.ErrPublicDescriptionTooLong} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			err := catalogPromotionValidationError(errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value")))
			require.EqualError(t, err, cause.Error())
			require.NotErrorIs(t, err, ErrStaleState)

			f := newWriteFixture(t, "catalog_promotion_length")
			w, mutator, input := newPromotionWriter(f, true, "eligible")
			mutator.validationErr = errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value"))
			_, err = w.apply(writeContext(t, f), input)
			require.EqualError(t, err, cause.Error())
			require.Zero(t, mutator.calls.Load())
		})
	}
}
