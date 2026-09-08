package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
)

type stubShadowInventory struct {
	list   *accessgen.ListShadowMCPInventoryResult
	target *accessgen.ShadowMCPInventoryServer
}

func (s stubShadowInventory) ReadShadowMCPInventory(context.Context, access.ShadowMCPInventoryReadInput) (*accessgen.ListShadowMCPInventoryResult, error) {
	return s.list, nil
}

func (s stubShadowInventory) ReadShadowMCPInventoryTarget(context.Context, access.ShadowMCPInventoryTargetInput) (*accessgen.ShadowMCPInventoryServer, error) {
	return s.target, nil
}

type stubShadowReview struct {
	summary mcpapproval.PlatformReviewSummary
}

func (s stubShadowReview) ReadPlatformReview(context.Context, mcpapproval.PlatformReviewReadInput) (mcpapproval.PlatformReviewSummary, error) {
	return s.summary, nil
}

func TestShadowInventoryProjectionSuppressesIdentityAndReferencesRoundTrip(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Default", Slug: "default"}
	canonical := "https://private.example.test/mcp?token=secret"
	decision := "approved"
	status := "requested"
	row := &accessgen.ShadowMCPInventoryServer{
		CanonicalServerURL: canonical,
		TargetKind:         new(shadowTargetKindServerURL),
		ServerName:         new("Reviewed service"),
		FirstSeen:          "2026-09-06T10:00:00Z",
		LastSeen:           "2026-09-06T11:00:00Z",
		ObservedUseCount:   7,
		UserCount:          1,
		AccessSummary: &accessgen.ShadowMCPAccessSummary{
			State: "restricted", AllowedFor: "selected", BlockedFor: "some", BlockingDefault: "deny", Decision: &decision, DecisionCoverage: "partial",
		},
		ApprovalRequest: &accessgen.ShadowMCPInventoryApprovalRequest{Status: status, RequesterCount: 1},
	}
	flags := &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}
	codec, err := newSubjectReferenceCodec("shadow-test-key")
	require.NoError(t, err)
	service := &ShadowInventoryService{
		projects: &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "organization", projectID: project.ID.String()}, {organizationID: "organization", projectID: project.ID.String()}}}, inventory: stubShadowInventory{list: &accessgen.ListShadowMCPInventoryResult{Servers: []*accessgen.ShadowMCPInventoryServer{row}}, target: row},
		reviews: stubShadowReview{summary: mcpapproval.PlatformReviewSummary{Status: status, EvidenceCollected: true, EvidenceGaps: []string{}, IdentityKind: "remote", PackagePublication: "unknown", RepositoryState: "found", AdvisoryLookup: "complete", KnownAdvisories: 2, AuthorityState: "declared", CapabilitySource: "server", DeclaredToolCount: 3, RiskyDeclarationCount: 1, ResearchStatus: "completed", ResearchCoverage: "moderate", CitationCount: 2, TrustedCitationCount: 1}},
		flags:   flags, organizations: riskMutationOrganizationResolver{slug: "organization"}, budget: allowBudget(), references: codec,
		now: func() time.Time { return time.Date(6, 9, 6, 12, 0, 0, 0, time.UTC) },
	}
	principal := Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}

	listed, err := service.List(t.Context(), principal, ListShadowMCPInventoryInput{ProjectID: project.ID.String(), Limit: 10})
	require.NoError(t, err)
	require.Len(t, listed.Targets, 1)
	require.True(t, listed.Targets[0].UserCount.Suppressed())
	require.True(t, listed.Targets[0].Review.RequesterCount.Suppressed())
	require.Equal(t, "Observed MCP server", listed.Targets[0].Display)
	require.NotContains(t, listed.Targets[0].TargetReference, "private")

	detail, err := service.GetReview(t.Context(), principal, GetShadowMCPReviewInput{ProjectID: project.ID.String(), TargetReference: listed.Targets[0].TargetReference})
	require.NoError(t, err)
	require.Equal(t, 2, detail.Evidence.KnownAdvisories)

	encoded, err := json.Marshal(struct {
		List   ListShadowMCPInventoryOutput `json:"list"`
		Detail GetShadowMCPReviewOutput     `json:"detail"`
	}{List: listed, Detail: detail})
	require.NoError(t, err)
	for _, forbidden := range []string{canonical, "private.example.test", "token=secret", "Reviewed service", "user:", "policy_id", "citation.invalid", "requester@example"} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func TestShadowInventoryBoundsRequestOnlyPrefixAcrossPages(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "default"}
	rows := make([]*accessgen.ShadowMCPInventoryServer, 0, 60)
	for range 60 {
		rows = append(rows, &accessgen.ShadowMCPInventoryServer{
			CanonicalServerURL: uuid.NewString(), TargetKind: new(shadowTargetKindStdioCommand),
			AccessSummary: &accessgen.ShadowMCPAccessSummary{State: "unenforced", AllowedFor: "none", BlockedFor: "none", BlockingDefault: "none", DecisionCoverage: "none"},
		})
	}
	codec, err := newSubjectReferenceCodec("shadow-prefix-key")
	require.NoError(t, err)
	service := &ShadowInventoryService{
		projects:  &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "organization", projectID: project.ID.String()}, {organizationID: "organization", projectID: project.ID.String()}}},
		inventory: stubShadowInventory{list: &accessgen.ListShadowMCPInventoryResult{Servers: rows}}, reviews: stubShadowReview{},
		flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, organizations: riskMutationOrganizationResolver{slug: "organization"},
		budget: allowBudget(), references: codec, now: time.Now,
	}
	principal := Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}

	first, err := service.List(t.Context(), principal, ListShadowMCPInventoryInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.Len(t, first.Targets, shadowInventoryPageSize)
	require.NotEmpty(t, first.NextCursor)
	second, err := service.List(t.Context(), principal, ListShadowMCPInventoryInput{ProjectID: project.ID.String(), Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Targets, 10)
	require.Empty(t, second.NextCursor)
}

func TestShadowEvidenceRejectsUnknownCategories(t *testing.T) {
	t.Parallel()

	projected := shadowEvidence(mcpapproval.PlatformReviewSummary{
		EvidenceGaps: []string{"future_gap"}, IdentityKind: "future_identity", PackagePublication: "unknown",
		RepositoryState: "unknown", AdvisoryLookup: "unknown", AuthorityState: "unknown", CapabilitySource: "unknown",
		ResearchStatus: "none", ResearchCoverage: "unknown",
	})
	require.Equal(t, emptyShadowEvidence(), projected)
}

func TestShadowInventoryCursorIsProjectAndSessionBound(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "default"}
	row := &accessgen.ShadowMCPInventoryServer{CanonicalServerURL: "https://one.example.test", TargetKind: new(shadowTargetKindServerURL), FirstSeen: "2026-09-06T10:00:00Z", AccessSummary: &accessgen.ShadowMCPAccessSummary{State: "unenforced", AllowedFor: "none", BlockedFor: "none", BlockingDefault: "none", DecisionCoverage: "none"}}
	codec, err := newSubjectReferenceCodec("shadow-cursor-key")
	require.NoError(t, err)
	service := &ShadowInventoryService{projects: &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "organization", projectID: project.ID.String()}, {organizationID: "organization", projectID: project.ID.String()}}}, inventory: stubShadowInventory{list: &accessgen.ListShadowMCPInventoryResult{Servers: []*accessgen.ShadowMCPInventoryServer{row, row}}}, reviews: stubShadowReview{}, flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, organizations: riskMutationOrganizationResolver{slug: "organization"}, budget: allowBudget(), references: codec, now: time.Now}
	principal := Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}

	first, err := service.List(t.Context(), principal, ListShadowMCPInventoryInput{ProjectID: project.ID.String(), Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	_, err = service.List(t.Context(), Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}, ListShadowMCPInventoryInput{ProjectID: project.ID.String(), Cursor: first.NextCursor})
	require.ErrorIs(t, err, ErrShadowInventoryInvalid)
	_, err = codec.DecodeScoped(first.NextCursor, principal, shadowInventoryCursorKind, queryScope("shadow_inventory", uuid.NewString()), time.Now())
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}
