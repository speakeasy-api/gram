package mcpskills_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// recordingFlagProvider counts which evaluation path a flag check takes. The
// handshake must only ever take the local one.
type recordingFlagProvider struct {
	localCalls  int
	remoteCalls int
	localResult bool
	localErr    error
	lastGroups  map[string]string
}

func (p *recordingFlagProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	p.remoteCalls++
	return true, nil
}

func (p *recordingFlagProvider) IsFlagEnabledLocal(_ context.Context, _ feature.Flag, _ string, groups, _ map[string]string) (bool, error) {
	p.localCalls++
	p.lastGroups = groups
	return p.localResult, p.localErr
}

func (p *recordingFlagProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func TestFlagEnabled_UsesOnlyLocalEvaluation(t *testing.T) {
	t.Parallel()

	provider := &recordingFlagProvider{localResult: true}
	on := mcpskills.FlagEnabled(t.Context(), testenv.NewLogger(t), provider, "org_1", "acme")
	require.True(t, on)
	require.Equal(t, 1, provider.localCalls)
	require.Zero(t, provider.remoteCalls, "the handshake must never wait on a remote flag evaluation")
	require.Equal(t, map[string]string{"organization": "acme"}, provider.lastGroups)
}

func TestFlagEnabled_FailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("provider error", func(t *testing.T) {
		t.Parallel()
		provider := &recordingFlagProvider{localResult: true, localErr: errors.New("definitions unavailable")}
		require.False(t, mcpskills.FlagEnabled(t.Context(), testenv.NewLogger(t), provider, "org_1", "acme"))
		require.Zero(t, provider.remoteCalls)
	})

	t.Run("no provider", func(t *testing.T) {
		t.Parallel()
		require.False(t, mcpskills.FlagEnabled(t.Context(), testenv.NewLogger(t), nil, "org_1", "acme"))
	})

	t.Run("no organization", func(t *testing.T) {
		t.Parallel()
		provider := &recordingFlagProvider{localResult: true}
		require.False(t, mcpskills.FlagEnabled(t.Context(), testenv.NewLogger(t), provider, "", "acme"))
		require.Zero(t, provider.localCalls)
	})

	t.Run("unknown slug evaluates by id alone", func(t *testing.T) {
		t.Parallel()
		provider := &recordingFlagProvider{localResult: true}
		require.True(t, mcpskills.FlagEnabled(t.Context(), testenv.NewLogger(t), provider, "org_1", ""))
		require.Nil(t, provider.lastGroups)
	})
}
