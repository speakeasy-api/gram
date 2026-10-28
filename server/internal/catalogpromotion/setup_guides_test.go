package catalogpromotion

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/setupguides"
	"github.com/stretchr/testify/require"
)

func TestGetCandidateListsPinnedSetupGuidesForReview(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)

	candidate, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.NotNil(t, candidate.SuggestedMetadata)
	require.Equal(t, setupguides.List(), candidate.SetupGuides)
	require.Equal(t, setupguides.PinnedVersion, candidate.SetupGuidesVersion)
	require.Empty(t, candidate.EndpointGuideSlug, "the synthetic endpoint has no guide")
}

func TestGetCandidateReportsTheGuideForADocumentedEndpoint(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{Name: "Synthetic documented", Slug: "synthetic-documented", OrganizationID: f.target.OrganizationID})
	require.NoError(t, err)
	source, err := repo.New(f.db).SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: project.ID, Url: "https://mcp.slack.com/mcp", CreatedAt: conv.ToPGTimestamptz(f.health.now.Add(-7 * 24 * time.Hour))})
	require.NoError(t, err)

	candidate, err := f.service.GetCandidate(t.Context(), Target{OrganizationID: f.target.OrganizationID, ProjectID: project.ID, MCPServerID: source})
	require.NoError(t, err)
	require.Equal(t, "slack", candidate.EndpointGuideSlug)
	require.NotNil(t, candidate.SuggestedMetadata)
	require.Equal(t, "slack", candidate.SuggestedMetadata.DocsSlug)
}
