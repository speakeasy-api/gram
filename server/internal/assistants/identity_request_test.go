package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestAssistantCreateRequestCanonicalFingerprint(t *testing.T) {
	t.Parallel()
	empty := ""
	first := assistantCreateFingerprint{
		Version: 1, Name: "Example", Model: "test-model", Instructions: "Instructions",
		Toolsets:       []*types.AssistantToolsetRef{{ToolsetSlug: "b", EnvironmentSlug: nil}, nil, {ToolsetSlug: "a", EnvironmentSlug: &empty}},
		MCPServers:     []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: nil, EndpointSlug: nil}},
		WarmTTLSeconds: 60, MaxConcurrency: 1, Status: StatusActive,
	}
	second := first
	second.Toolsets = []*types.AssistantToolsetRef{{ToolsetSlug: "a", EnvironmentSlug: nil}, {ToolsetSlug: "b", EnvironmentSlug: &empty}}
	second.MCPServers = []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &empty, EndpointSlug: &empty}}
	before, err := assistantCreateRequestHash(first)
	require.NoError(t, err)
	after, err := assistantCreateRequestHash(second)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Nil(t, first.Toolsets[0].EnvironmentSlug, "hash normalization must not mutate caller references")
	require.Equal(t, "b", first.Toolsets[0].ToolsetSlug, "hash normalization must not reorder caller references")
	for name, change := range map[string]func(*assistantCreateFingerprint){
		"name":         func(v *assistantCreateFingerprint) { v.Name = "Other" },
		"model":        func(v *assistantCreateFingerprint) { v.Model = "other-model" },
		"instructions": func(v *assistantCreateFingerprint) { v.Instructions = "Other instructions" },
		"toolsets": func(v *assistantCreateFingerprint) {
			v.Toolsets = []*types.AssistantToolsetRef{{ToolsetSlug: "other", EnvironmentSlug: nil}}
		},
		"mcp servers": func(v *assistantCreateFingerprint) {
			v.MCPServers = []*types.AssistantMCPServerRef{{McpServerSlug: "other", EnvironmentSlug: nil, EndpointSlug: nil}}
		},
		"environment": func(v *assistantCreateFingerprint) {
			v.MCPServers = []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: conv.PtrEmpty("staging"), EndpointSlug: nil}}
		},
		"endpoint": func(v *assistantCreateFingerprint) {
			v.MCPServers = []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: nil, EndpointSlug: conv.PtrEmpty("other")}}
		},
		"warm ttl":    func(v *assistantCreateFingerprint) { v.WarmTTLSeconds = 120 },
		"concurrency": func(v *assistantCreateFingerprint) { v.MaxConcurrency = 2 },
		"status":      func(v *assistantCreateFingerprint) { v.Status = StatusPaused },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			altered := first
			change(&altered)
			hash, err := assistantCreateRequestHash(altered)
			require.NoError(t, err)
			require.NotEqual(t, before, hash)
		})
	}
}

func TestAssistantCreateRequestKeyScopesCallerAndTenant(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	base, err := assistantCreateRequestKey("org-example", project, "user-example", "retry-key")
	require.NoError(t, err)
	require.Len(t, base, 64)
	for _, scope := range []struct {
		org     string
		project uuid.UUID
		actor   string
		key     string
	}{
		{org: "org-other", project: project, actor: "user-example", key: "retry-key"},
		{org: "org-example", project: uuid.New(), actor: "user-example", key: "retry-key"},
		{org: "org-example", project: project, actor: "user-other", key: "retry-key"},
		{org: "org-example", project: project, actor: "user-example", key: "other-key"},
	} {
		got, err := assistantCreateRequestKey(scope.org, scope.project, scope.actor, scope.key)
		require.NoError(t, err)
		require.NotEqual(t, base, got)
	}
	_, err = assistantCreateRequestKey("org-example", project, "user-example", "")
	require.ErrorIs(t, err, errAssistantValidation)
}
