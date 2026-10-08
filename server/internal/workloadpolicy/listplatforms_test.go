package workloadpolicy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func listPlatformsPayload() *gen.ListPlatformsPayload {
	return &gen.ListPlatformsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestListPlatforms_ReturnsTheCatalogWithItsSetup(t *testing.T) {
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
	require.NotEmpty(t, claudeTag.Steps)
	require.Equal(t, "collect", claudeTag.Steps[0].Phase)
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
		MatchKind:        "wildcard",
		Name:             nil,
		Tags:             []string{"slack"},
		AgentID:          newAgent(t, ctx, ti, "Claude Tag agent").String(),
		ProjectScoped:    false,
	})
	require.NoError(t, err)

	require.Len(t, policy.Admissions, 1)
	admission := policy.Admissions[0]
	require.Equal(t, "wimse://identity.anthropic.com/org/org-123/agent/*", admission.Subject)
	require.Equal(t, "wildcard", admission.MatchKind)
	require.True(t, admission.WildcardActive)
	require.Equal(t, "Claude Tag agent", admission.AgentName)
	require.Equal(t, []string{"slack"}, admission.Tags)
}
