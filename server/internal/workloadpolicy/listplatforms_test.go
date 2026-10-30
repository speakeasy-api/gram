package workloadpolicy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func listPlatformsPayload() *gen.ListPlatformsPayload {
	return &gen.ListPlatformsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestListPlatforms_ReturnsTheCatalogPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.ListPlatforms(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), listPlatformsPayload())
	require.NoError(t, err)

	var claudeTag *gen.WorkloadPlatform
	for _, platform := range result.Platforms {
		if platform.Key == "claude-tag" {
			claudeTag = platform
		}
	}
	require.NotNil(t, claudeTag)
	require.Equal(t, "https://identity.anthropic.com/agents", claudeTag.Issuer.Value)
	require.Equal(t, "hidden", claudeTag.Issuer.Visibility)
	require.True(t, claudeTag.Subject.Wildcard)
	require.Equal(t, "org_id", claudeTag.Variables[0].Key)
}

func TestListPlatforms_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListPlatforms(withScopes(t, ctx, ti), listPlatformsPayload())
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The rows the guided setup writes for a catalog platform are ordinary ones:
// its constants register as a trusted platform, and its filled subject
// template admits as a wildcard rule with an active agent.
func TestCatalogPlatform_RegistersAndAdmitsThroughTheAPI(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	result, err := ti.service.ListPlatforms(orgCtx, listPlatformsPayload())
	require.NoError(t, err)
	var claudeTag *gen.WorkloadPlatform
	for _, platform := range result.Platforms {
		if platform.Key == "claude-tag" {
			claudeTag = platform
		}
	}
	require.NotNil(t, claudeTag)

	_, err = ti.service.RegisterIssuer(orgCtx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   claudeTag.DisplayName,
		Issuer:                 claudeTag.Issuer.Value,
		JwksURI:                claudeTag.JwksURI.Value,
		Description:            &claudeTag.Description,
		AllowWildcardAdmission: nil,
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	rule := strings.ReplaceAll(claudeTag.Subject.Template, "{org_id}", "org-123") + "*"
	policy, err := ti.service.AdmitSubject(orgCtx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           claudeTag.Issuer.Value,
		Subject:          rule,
		MatchKind:        string(workloadidentity.MatchKindWildcard),
		Name:             nil,
		Tags:             []string{"slack"},
		AgentID:          newAgent(t, ctx, ti, "Claude Tag agent").String(),
		ProjectScoped:    false,
	})
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	admission := policy.Admissions[0]
	require.Equal(t, "wimse://identity.anthropic.com/org/org-123/agent/*", admission.Subject)
	require.Equal(t, string(workloadidentity.MatchKindWildcard), admission.MatchKind)
	require.True(t, admission.WildcardActive)
	require.Equal(t, "Claude Tag agent", admission.AgentName)
	require.Equal(t, []string{"slack"}, admission.Tags)
}

// The GitHub Actions setup writes both rows at the organization tier, because
// the device-agent token endpoint, which a job's device agent exchanges at,
// reads only that tier. A project's MCP servers read it too, so the one rule
// serves plugins, hooks and MCP alike.
func TestCatalogGitHubActions_WritesOrganizationTierRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	orgCtx := withoutProject(t, ctx)

	result, err := ti.service.ListPlatforms(orgCtx, listPlatformsPayload())
	require.NoError(t, err)
	var github *gen.WorkloadPlatform
	for _, platform := range result.Platforms {
		if platform.Key == "github-actions" {
			github = platform
		}
	}
	require.NotNil(t, github)
	require.Equal(t, "read-only", github.Issuer.Visibility)

	_, err = ti.service.RegisterIssuer(orgCtx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   github.DisplayName,
		Issuer:                 github.Issuer.Value,
		JwksURI:                github.JwksURI.Value,
		Description:            &github.Description,
		AllowWildcardAdmission: nil,
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	rule := strings.NewReplacer("{owner}", "acme", "{repo}", "payments-api").Replace(github.Subject.Template) + "*"
	policy, err := ti.service.AdmitSubject(orgCtx, &gen.AdmitSubjectPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Issuer:           github.Issuer.Value,
		Subject:          rule,
		MatchKind:        string(workloadidentity.MatchKindWildcard),
		Name:             nil,
		Tags:             nil,
		AgentID:          newAgent(t, ctx, ti, "Release bot").String(),
		ProjectScoped:    false,
	})
	require.NoError(t, err)

	require.Len(t, policy.Issuers, 1)
	require.Empty(t, policy.Issuers[0].ProjectID, "the issuer must be organization-tier")
	require.Len(t, policy.Admissions, 1)
	admission := policy.Admissions[0]
	require.Empty(t, admission.ProjectID, "the rule must be organization-tier")
	require.Equal(t, "repo:acme/payments-api:*", admission.Subject)
	require.Equal(t, string(workloadidentity.MatchKindWildcard), admission.MatchKind)
	require.True(t, admission.WildcardActive)
	require.Equal(t, "Release bot", admission.AgentName)
}
