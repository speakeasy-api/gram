package anthropicinference

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/stretchr/testify/require"
)

type scannerFunc func(context.Context, risk.RealtimeScanRequest) (*risk.ScanResult, error)

func (f scannerFunc) ScanForInferenceEnforcement(ctx context.Context, r risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	return f(ctx, r)
}

func TestAcceptedPrefix(t *testing.T) {
	t.Parallel()
	hashes := func(values ...string) [][]byte {
		msgs := make([]Message, len(values))
		for i, v := range values {
			msgs[i] = textMessage("user", v)
		}
		return transcriptHashes(msgs)
	}
	for _, tc := range []struct {
		name            string
		accepted, frame [][]byte
		want            int
	}{
		{"rollout", nil, hashes("a", "b"), 0},
		{"delta", hashes("a", "b"), hashes("a", "b", "c"), 2},
		{"retained tail", hashes("a", "b", "c"), hashes("b", "c", "d"), 2},
		{"edited early content", hashes("a", "b", "c"), hashes("edit", "b", "c"), 0},
		{"edit after prefix", hashes("a", "b", "c"), hashes("a", "edit", "c"), 1},
		{"summary before anchor", hashes("a", "b", "c"), hashes("summary", "b", "c"), 0},
		{"ambiguous tail", hashes("a", "b", "b"), hashes("b", "b", "c"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); require.Equal(t, tc.want, acceptedPrefix(tc.accepted, tc.frame)) })
	}
	repeated := make([]Message, 513)
	for i := range repeated {
		repeated[i] = textMessage("user", "continue")
	}
	require.Equal(t, 513, acceptedPrefix(transcriptHashes(repeated), transcriptHashes(repeated)))
	require.Equal(t, 513, acceptedPrefix(transcriptHashes(repeated), transcriptHashes(append(repeated, textMessage("user", "continue")))))
}

func TestDeniedToolRetryDoesNotBecomeAccepted(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{
		textMessage("user", "prompt"),
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call-example","name":"blocked_tool","input":{"path":"example"}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-example","content":"benign result"}]`)},
	}
	store := &memoryStore{}
	calls := 0
	service := &Service{store: store, scanner: scannerFunc(func(_ context.Context, r risk.RealtimeScanRequest) (*risk.ScanResult, error) {
		calls++
		if r.ToolName == "blocked_tool" {
			return &risk.ScanResult{Action: "block"}, nil
		}
		return nil, nil
	})}
	for range 2 {
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "deny", verdict.Action)
		require.Empty(t, store.accepted)
	}
	require.Equal(t, 4, calls)
	require.Len(t, store.saved, 2)
	// An edited retry recovers: denial is not sticky session state.
	frame.Messages[1] = textMessage("assistant", "safe reply")
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Equal(t, transcriptHashes(frame.Messages), store.accepted)
}

func TestPartialEvaluationDoesNotAdvanceCheckpoint(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "timeout", "cancel after last scan"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			frame := exampleFrame()
			frame.Messages = []Message{textMessage("user", "prompt"), textMessage("assistant", "reply"), textMessage("user", "result")}
			before := transcriptHashes(frame.Messages[:1])
			store := &memoryStore{accepted: before}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			service := &Service{store: store, scanner: scannerFunc(func(ctx context.Context, _ risk.RealtimeScanRequest) (*risk.ScanResult, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "error":
						return nil, errors.New("scanner unavailable")
					case "timeout":
						return nil, context.DeadlineExceeded
					default:
						cancel()
						return nil, nil
					}
				}
				return nil, nil
			})}
			_, err := service.Process(ctx, Config{}, frame)
			require.Error(t, err)
			require.Equal(t, before, store.accepted)
			require.Len(t, store.saved, 1)
			scanned := &recordingScanner{}
			service.scanner = scanned
			verdict, err := service.Process(t.Context(), Config{}, frame)
			require.NoError(t, err)
			require.Equal(t, "allow", verdict.Action)
			require.Len(t, scanned.inputs, 2)
			require.Equal(t, transcriptHashes(frame.Messages), store.accepted)
		})
	}
}

func TestArchivedHistoryWithoutCheckpointIsRescanned(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "prompt"), textMessage("assistant", "reply"), textMessage("user", "result")}
	store := &deltaStore{newStart: len(frame.Messages)}
	scanned := &recordingScanner{}
	service := &Service{store: store, scanner: scanned}
	_, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Len(t, scanned.inputs, 3)
	scanned.inputs = nil
	_, err = service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Len(t, scanned.inputs, 1)
}
