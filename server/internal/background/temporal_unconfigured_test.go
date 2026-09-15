package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// TestMCPRuntimeHelpersWithoutTemporal covers the Temporal dispatch helpers
// reachable from the MCP serving path: the chat writer observers and the
// completions client behind platform tools and dynamic tool search, and
// OpenRouter key provisioning. A process without a Temporal environment must
// get an error back rather than a nil pointer dereference, which would crash
// the process when the caller runs in a goroutine.
func TestMCPRuntimeHelpersWithoutTemporal(t *testing.T) {
	t.Parallel()

	var nilEnv *tenv.Environment
	cases := map[string]func(ctx context.Context) error{
		"chat analysis signal": func(ctx context.Context) error {
			return (&TemporalChatAnalysisSignaler{TemporalEnv: nilEnv}).Signal(ctx, uuid.New())
		},
		"chat title generation": func(ctx context.Context) error {
			return (&TemporalChatTitleGenerator{TemporalEnv: nilEnv}).ScheduleChatTitleGeneration(ctx, "chat", "org", "project")
		},
		"openrouter key refresh": func(ctx context.Context) error {
			return (&OpenRouterKeyRefresher{TemporalEnv: nilEnv}).ScheduleOpenRouterKeyRefresh(ctx, "org", openrouter.KeyType(""), nil)
		},
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, run(t.Context()), tenv.ErrNotConfigured)
		})
	}
}
