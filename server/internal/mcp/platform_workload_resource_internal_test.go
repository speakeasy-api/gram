package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func TestPlatformWorkloadResourceWithoutServedToolsets(t *testing.T) {
	t.Parallel()
	serverURL := &url.URL{Scheme: "https", Host: "api.example"}
	// The token endpoint can run in a process that serves no platform toolsets.
	s := &Service{serverURL: serverURL}
	for _, slug := range []string{platformtools.AssistantsPlatformToolsetSlug, platformtools.ManagedAssistantPlatformToolsetSlug, platformtools.PlatformMCPReadToolsetSlug} {
		got, ok := s.platformWorkloadResource(platformtools.PlatformToolsetURL(serverURL, slug))
		require.True(t, ok, slug)
		require.Equal(t, slug, got)
	}
	_, ok := s.platformWorkloadResource(platformtools.PlatformToolsetURL(serverURL, platformtools.ResearchToolsetSlug))
	require.False(t, ok, "toolsets no assistant can be granted are not workload resources")
	_, ok = s.platformWorkloadResource("https://other.example/platform/mcp/assistants")
	require.False(t, ok)
}
