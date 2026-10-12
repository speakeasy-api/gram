package enrich

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelNamesTheSpeakeasyProducers(t *testing.T) {
	t.Parallel()

	require.Equal(t, ChannelGateway, Channel(GatewayScopeName))
	require.Equal(t, ChannelHooks, Channel(HooksScopeName))
	require.Equal(t, ChannelChat, Channel(ChatScopeName))
}

func TestChannelTreatsEveryOtherScopeAsOTel(t *testing.T) {
	t.Parallel()

	require.Equal(t, ChannelOTel, Channel("com.anthropic.claude_code.events"))
	require.Equal(t, ChannelOTel, Channel("codex_otel.log_only"))
	require.Equal(t, ChannelOTel, Channel(""))
}
