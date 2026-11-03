package workloadpolicy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
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
	// Empty where there are none.
	require.Empty(t, claudeTag.Subject.Label)
	require.Empty(t, claudeTag.Subject.Alternatives)
}

// recordingFlags is an in-memory flag provider that records what each
// evaluation asks and, with fail set, fails every one.
type recordingFlags struct {
	*feature.InMemory
	fail  error
	asked []flagRequest
}

// flagRequest is what one evaluation asked the provider.
type flagRequest struct {
	flag       feature.Flag
	distinctID string
	groups     map[string]string
}

func (f *recordingFlags) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	f.asked = append(f.asked, flagRequest{flag: flag, distinctID: distinctID, groups: groups})
	if f.fail != nil {
		return feature.EvaluationIndeterminate, f.fail
	}
	return f.InMemory.EvaluateFlag(ctx, flag, distinctID, groups) //nolint:wrapcheck // Test double.
}

// listedEnabled lists the catalog as ctx's caller sees it and reports whether
// each entry is enabled, by key.
func listedEnabled(t *testing.T, ctx context.Context, ti *testInstance) map[string]bool {
	t.Helper()

	result, err := ti.service.ListPlatforms(ctx, listPlatformsPayload())
	require.NoError(t, err)
	enabled := make(map[string]bool, len(result.Platforms))
	for _, platform := range result.Platforms {
		enabled[platform.Key] = platform.Enabled
	}
	return enabled
}

// requireGitHubActionsComingSoon requires ctx's caller to be offered every
// entry but GitHub Actions.
func requireGitHubActionsComingSoon(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	require.Equal(t, map[string]bool{"claude-tag": true, "github-actions": false, "gke": true}, listedEnabled(t, ctx, ti))
}

func TestListPlatforms_OffersGitHubActionsWhereItsFlagIsOn(t *testing.T) {
	t.Parallel()
	flags := &recordingFlags{InMemory: &feature.InMemory{}}
	ctx, ti := newTestServiceWithFlags(t, flags)
	flags.SetFlag(feature.FlagWorkloadPlatformGitHubActions, ti.orgID, true)

	require.Equal(t, map[string]bool{"claude-tag": true, "github-actions": true, "gke": true}, listedEnabled(t, ctx, ti))
	// Asked once, for the one entry that names a flag, as the caller's
	// organization and its organization group, which the flag targets.
	require.NotEmpty(t, ti.orgSlug)
	require.Equal(t, []flagRequest{{
		flag:       feature.FlagWorkloadPlatformGitHubActions,
		distinctID: ti.orgID,
		groups:     map[string]string{"organization": ti.orgSlug},
	}}, flags.asked)

	// Offering it changed the copy one response is built from, not the
	// catalog every request shares.
	platforms, err := catalog.Embedded().Platforms(ctx)
	require.NoError(t, err)
	for _, platform := range platforms {
		require.Equal(t, platform.Key != "github-actions", platform.Enabled, platform.Key)
	}
}

func TestListPlatforms_KeepsGitHubActionsComingSoonWhileItsFlagIsMissing(t *testing.T) {
	t.Parallel()
	flags := &recordingFlags{InMemory: &feature.InMemory{}}
	ctx, ti := newTestServiceWithFlags(t, flags)

	requireGitHubActionsComingSoon(t, ctx, ti)
	require.Len(t, flags.asked, 1)
}

func TestListPlatforms_KeepsGitHubActionsComingSoonWhileItsFlagIsOff(t *testing.T) {
	t.Parallel()
	flags := &recordingFlags{InMemory: &feature.InMemory{}}
	ctx, ti := newTestServiceWithFlags(t, flags)
	flags.SetFlag(feature.FlagWorkloadPlatformGitHubActions, ti.orgID, false)

	requireGitHubActionsComingSoon(t, ctx, ti)
	require.Len(t, flags.asked, 1)
}

func TestListPlatforms_KeepsGitHubActionsComingSoonWhileItsFlagIsOnForAnotherOrganization(t *testing.T) {
	t.Parallel()
	flags := &recordingFlags{InMemory: &feature.InMemory{}}
	ctx, ti := newTestServiceWithFlags(t, flags)
	flags.SetFlag(feature.FlagWorkloadPlatformGitHubActions, "another-organization", true)

	requireGitHubActionsComingSoon(t, ctx, ti)
	require.Len(t, flags.asked, 1)
}

func TestListPlatforms_KeepsGitHubActionsComingSoonWhenItsFlagFails(t *testing.T) {
	t.Parallel()
	flags := &recordingFlags{InMemory: &feature.InMemory{}, fail: errors.New("flag service unavailable")}
	ctx, ti := newTestServiceWithFlags(t, flags)
	// On, but the failure wins: an evaluation that errors is never enabled.
	flags.SetFlag(feature.FlagWorkloadPlatformGitHubActions, ti.orgID, true)

	requireGitHubActionsComingSoon(t, ctx, ti)
	require.Len(t, flags.asked, 1)
}

func TestListPlatforms_KeepsGitHubActionsComingSoonWithoutAFlagProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	requireGitHubActionsComingSoon(t, ctx, ti)
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
	// Listed while it is not offered, with the narrower rules it offers in
	// place of the whole repository.
	require.False(t, github.Enabled)
	require.Equal(t, "Whole repository", github.Subject.Label)
	require.Equal(t, []*gen.WorkloadPlatformSubjectAlternative{
		{Key: "branch", Label: "One branch", Template: "repo:{owner}/{repo}:ref:refs/heads/{branch}", Wildcard: false},
		{Key: "environment", Label: "One environment", Template: "repo:{owner}/{repo}:environment:{environment}", Wildcard: false},
	}, github.Subject.Alternatives)

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
