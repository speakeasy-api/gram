package assistants

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

func TestClassifyTurnError(t *testing.T) {
	t.Parallel()

	// Verbatim copy of the marker stamped by chat.HandleCompletion on upstream
	// 4xx responses; chat keeps it package-private so the cross-package
	// contract here is the matched substring, not the symbol.
	const corruptionMarker = "history corrupted: upstream provider rejected the replayed transcript"
	require.True(t, chat.IsHistoryCorrupted(errors.New(corruptionMarker)),
		"corruptionMarker must stay in sync with chat package; chat.IsHistoryCorrupted is the source of truth")

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "nil",
			err:  nil,
			want: nil,
		},
		{
			name: "network drop",
			err:  errors.New("send assistant fly runtime request: dial tcp: connection refused"),
			want: ErrRuntimeUnhealthy,
		},
		{
			name: "runner crash",
			err:  errors.New("status=502 body=upstream connect failure"),
			want: ErrRuntimeUnhealthy,
		},
		{
			name: "generic provider failure",
			err:  errors.New("status=500 body=provider error: model unavailable"),
			want: ErrCompletionFailed,
		},
		{
			name: "gateway-stamped generic failure",
			err:  errors.New("loop error: completion failed: rate limit exceeded"),
			want: ErrCompletionFailed,
		},
		{
			name: "marker stamped by /chat/completions on upstream 4xx",
			err:  fmt.Errorf("execute fly turn request: status=422 body=provider error: {\"message\":%q}", corruptionMarker),
			want: ErrHistoryCorrupted,
		},
		{
			name: "marker wrapped through assistant runtime error chain",
			err: fmt.Errorf("run assistant turn: %w",
				errors.New("execute fly turn request: status=422 body=provider error: "+corruptionMarker)),
			want: ErrHistoryCorrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyTurnError(tt.err)
			if tt.want == nil {
				require.NoError(t, got)
				return
			}
			require.ErrorIs(t, got, tt.want)
		})
	}
}
