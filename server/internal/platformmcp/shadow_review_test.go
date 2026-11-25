package platformmcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

type fixedOrganizationSlugs struct{ slug string }

func (f fixedOrganizationSlugs) OrganizationSlug(context.Context, string) (string, error) {
	return f.slug, nil
}

func TestShadowMCPReviewLinkServiceBuildsTheServerPage(t *testing.T) {
	t.Parallel()

	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := NewShadowMCPReviewLinkService(dashboard, fixedOrganizationSlugs{slug: "acme"})
	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	principal := Principal{OrganizationID: "org", UserID: "admin"}

	require.Equal(t, "https://app.example.test/acme/projects/project/shadow-ai/mcps/server-1234abcd", service.ShadowMCPReviewURL(t.Context(), principal, project, "server-1234abcd"))
	require.Empty(t, service.ShadowMCPReviewURL(t.Context(), principal, project, ""))
	require.Empty(t, service.ShadowMCPReviewURL(t.Context(), principal, ResolvedProject{}, "server-1234abcd"))
	require.Empty(t, NewShadowMCPReviewLinkService(nil, fixedOrganizationSlugs{slug: "acme"}).ShadowMCPReviewURL(t.Context(), principal, project, "server-1234abcd"))
	require.Empty(t, (*ShadowMCPReviewLinkService)(nil).ShadowMCPReviewURL(t.Context(), principal, project, "server-1234abcd"))
}

// The standing is read back from the admission reader, never assumed from the
// write, and "covered" is reported in the policy page's word: approved.
func TestShadowMCPStandingReportsTheReadBackState(t *testing.T) {
	t.Parallel()

	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	links := NewShadowMCPReviewLinkService(dashboard, fixedOrganizationSlugs{slug: "acme"})
	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	principal := Principal{OrganizationID: "org", UserID: "admin"}
	const remote = "https://MCP.example.test/server/"
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(remote)
	require.True(t, ok)

	for _, test := range []struct {
		readState string
		want      string
	}{
		{readState: DistributionAdmissionCovered, want: shadowMCPStandingApproved},
		{readState: DistributionAdmissionNotApplicable, want: DistributionAdmissionNotApplicable},
		{readState: DistributionAdmissionRepairRequired, want: DistributionAdmissionRepairRequired},
	} {
		calls := 0
		reader := stubDistributionAdmissionReader{target: DistributionAdmission{State: test.readState, MissingAudienceCounts: admission.MissingAudienceCounts{}, CheckedAt: "", Complete: true}, targetCalls: &calls}
		standing := shadowMCPStanding(t.Context(), reader, links, principal, project, remote)
		require.NotNil(t, standing)
		require.Equal(t, test.want, standing.State)
		require.Equal(t, canonical.CanonicalURL, standing.Target)
		require.Equal(t, "https://app.example.test/acme/projects/project/shadow-ai/mcps/"+shadowmcp.ServerSlug(canonical.CanonicalURL), standing.ReviewURL)
		require.Equal(t, 1, calls)
	}

	require.Nil(t, shadowMCPStanding(t.Context(), nil, links, principal, project, remote), "no reader, no standing")
	require.Nil(t, shadowMCPStanding(t.Context(), stubDistributionAdmissionReader{}, links, principal, project, ""), "no URL, no standing")
	require.Nil(t, shadowMCPStanding(t.Context(), stubDistributionAdmissionReader{}, links, principal, ResolvedProject{}, remote), "no project, no standing")
	withoutLinks := shadowMCPStanding(t.Context(), stubDistributionAdmissionReader{target: DistributionAdmission{State: DistributionAdmissionCovered, Complete: true}}, nil, principal, project, remote)
	require.NotNil(t, withoutLinks)
	require.Empty(t, withoutLinks.ReviewURL)
}

func TestShadowMCPStandingMessageOnlySpeaksWhenThereIsSomethingToSay(t *testing.T) {
	t.Parallel()

	require.Empty(t, shadowMCPStandingMessage(nil))
	require.Empty(t, shadowMCPStandingMessage(&ShadowMCPStanding{State: DistributionAdmissionNotApplicable}))
	approved := shadowMCPStandingMessage(&ShadowMCPStanding{State: shadowMCPStandingApproved})
	require.Contains(t, approved, "approved under it")
	require.NotContains(t, approved, "your distribution", "an earlier decision may already have covered the audience, so the write is not credited")
	require.Contains(t, shadowMCPStandingMessage(&ShadowMCPStanding{State: DistributionAdmissionEmptyAudience}), "No one is assigned")
	require.Contains(t, shadowMCPStandingMessage(&ShadowMCPStanding{State: DistributionAdmissionUnavailable}), "could not be confirmed")
}
