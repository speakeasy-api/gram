package mcptoolexecution

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// TestClassifyIdentityCoverage pins the bounded mapping from derivation
// outcomes to identity coverage classes. No branch may surface an identifier
// or fold unsupported identity into a success-looking class.
func TestClassifyIdentityCoverage(t *testing.T) {
	t.Parallel()

	candidates, err := killswitches.NewPrincipalCandidateResult([]killswitches.PrincipalCandidate{{Kind: PrincipalKindUser, Key: "user_01J8EXAMPLE"}})
	require.NoError(t, err)
	unsupported := killswitches.UnsupportedPrincipalCandidateResult()
	zero := killswitches.PrincipalCandidateResult{}

	tests := []struct {
		name   string
		source any
		result killswitches.PrincipalCandidateResult
		err    error
		want   mcpmetrics.KillswitchIdentityClass
	}{
		{name: "active user", source: testIdentity(t, mcpidentity.KindUserSession, "user_01J8EXAMPLE"), result: candidates, err: nil, want: mcpmetrics.KillswitchIdentityActiveUser},
		{name: "inactive user", source: testIdentity(t, mcpidentity.KindUserSession, "user_01J8EXAMPLE"), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityInactiveUser},
		{name: "inactive delegated user", source: testIdentity(t, mcpidentity.KindDelegatedUser, "user_01J8EXAMPLE"), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityInactiveUser},
		{name: "anonymous", source: testIdentity(t, mcpidentity.KindAnonymous, ""), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityAnonymous},
		{name: "api key", source: testIdentity(t, mcpidentity.KindAPIKey, ""), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityAPIKey},
		{name: "assistant", source: testIdentity(t, mcpidentity.KindAssistant, ""), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityAssistant},
		{name: "chat session", source: testIdentity(t, mcpidentity.KindChatSession, ""), result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityChatSession},
		{name: "no provenance", source: nil, result: zero, err: nil, want: mcpmetrics.KillswitchIdentityUnattributed},
		{name: "opaque zero value", source: mcpidentity.Identity{}, result: unsupported, err: nil, want: mcpmetrics.KillswitchIdentityUnattributed},
		{name: "infrastructure failure", source: testIdentity(t, mcpidentity.KindUserSession, "user_01J8EXAMPLE"), result: zero, err: fmt.Errorf("membership lookup: %w", errors.New("closed")), want: mcpmetrics.KillswitchIdentityUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ClassifyPrincipalCoverage(tt.source, tt.result, tt.err))
		})
	}
}

// TestClassifyResourceCoverage pins the bounded mapping from resource
// derivation outcomes to resource coverage classes, keeping ownership
// rejections distinct from availability failures.
func TestClassifyResourceCoverage(t *testing.T) {
	t.Parallel()

	serverID := uuid.MustParse("0198a1b2-c3d4-7000-8000-0123456789ab")
	canonical, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(serverID.String()))
	require.NoError(t, err)
	unsupported := killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()
	zero := killswitches.CanonicalizationResult[killswitches.ResourceKey]{}
	fronting := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}}
	noServer := ServerSource{FrontingServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}

	tests := []struct {
		name   string
		source any
		result killswitches.CanonicalizationResult[killswitches.ResourceKey]
		err    error
		want   mcpmetrics.KillswitchResourceClass
	}{
		{name: "canonical server", source: fronting, result: canonical, err: nil, want: mcpmetrics.KillswitchResourceCanonicalServer},
		{name: "legacy no server", source: noServer, result: unsupported, err: nil, want: mcpmetrics.KillswitchResourceLegacyNoServer},
		{name: "unsupported surface", source: nil, result: zero, err: nil, want: mcpmetrics.KillswitchResourceUnsupportedSurface},
		{name: "invalid owner", source: fronting, result: zero, err: fmt.Errorf("derive: %w", ErrServerNotInOrganization), want: mcpmetrics.KillswitchResourceInvalidOwner},
		{name: "infrastructure failure", source: fronting, result: zero, err: errors.New("connection refused"), want: mcpmetrics.KillswitchResourceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ClassifyResourceCoverage(tt.source, tt.result, tt.err))
		})
	}
}
