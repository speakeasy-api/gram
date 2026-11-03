package mcp

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/stretchr/testify/require"
)

func TestPlatformWorkloadResourceExact(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://management.example.com")
	require.NoError(t, err)
	slug := platformtools.AssistantsPlatformToolsetSlug
	s := &Service{serverURL: base, platformToolsets: map[string]platformtools.Toolset{slug: platformtools.NewAssistantsToolset()}}
	resource := platformtools.PlatformToolsetURL(base, slug)
	got, ok := s.platformWorkloadResource(resource)
	require.True(t, ok)
	require.Equal(t, slug, got)
	for _, invalid := range []string{resource + "/", resource + "?project_id=x", resource + "#fragment", "https://other.example.com/platform/mcp/" + slug, InferenceResource, base.JoinPath("mcp", slug).String()} {
		_, ok := s.platformWorkloadResource(invalid)
		require.False(t, ok, invalid)
	}
}

func TestPlatformWorkloadMarkerBindsProject(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	a, err := encodePlatformWorkloadPolicy(projectID, runtimepolicy.IssuedDelegatedPolicyVersion)
	require.NoError(t, err)
	again, err := encodePlatformWorkloadPolicy(projectID, runtimepolicy.IssuedDelegatedPolicyVersion)
	require.NoError(t, err)
	require.Equal(t, a, again, "the same project and policy version must produce a stable marker")
	b, err := encodePlatformWorkloadPolicy(uuid.New(), runtimepolicy.IssuedDelegatedPolicyVersion)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "persisted project cannot be retargeted without invalidating its marker")
}
