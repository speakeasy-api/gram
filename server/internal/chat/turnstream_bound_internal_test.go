package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSplitTurnTextCutsOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	// Four-byte runes: a limit of 10 bytes cannot hold three, so each piece
	// must stop at eight rather than cut the third.
	text := strings.Repeat("😀", 7)
	parts := splitTurnText(text, 10)

	require.Equal(t, text, strings.Join(parts, ""))
	for _, p := range parts {
		require.LessOrEqual(t, len(p), 10)
		require.True(t, utf8.ValidString(p))
	}
}

func TestSplitTurnTextLeavesShortTextWhole(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"hello"}, splitTurnText("hello", 10))
	require.Equal(t, []string{""}, splitTurnText("", 10), "an empty delta stays one frame")
}

// bigToolCalls returns tool-call JSON for n calls whose arguments total about
// argBytes each.
func bigToolCalls(t *testing.T, n, argBytes int) json.RawMessage {
	t.Helper()
	calls := make([]map[string]any, 0, n)
	for i := range n {
		args, err := json.Marshal(map[string]string{"q": strings.Repeat("a", argBytes)})
		require.NoError(t, err)
		calls = append(calls, map[string]any{
			"id": fmt.Sprintf("call-%04d", i), "type": "function",
			"function": map[string]string{"name": "search_the_knowledge_base", "arguments": string(args)},
		})
	}
	raw, err := json.Marshal(calls)
	require.NoError(t, err)
	return raw
}

// Every frame stored must be within the cap once encoded, whatever escaping
// does to its body. Control characters are the worst case: each encodes as
// six bytes.
func TestEncodeTurnFrameKeepsEveryEncodedFrameUnderTheCap(t *testing.T) {
	t.Parallel()

	escapes := strings.Repeat("\x01", 3*turnFrameMaxBytes)
	bigOutput, err := json.Marshal(escapes)
	require.NoError(t, err)

	inputs := []TurnFrame{
		{Kind: TurnFrameText, Text: strings.Repeat("t", 3*turnFrameMaxBytes+5)},
		{Kind: TurnFrameText, Text: escapes},
		{Kind: TurnFrameText, Text: strings.Repeat("<&>", turnFrameMaxBytes)},
		{Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: bigOutput},
		{Kind: TurnFrameMessage, MessageID: "m1", ToolCalls: bigToolCalls(t, 1, 3*turnFrameMaxBytes)},
		{Kind: TurnFrameMessage, MessageID: "m1", ToolCalls: bigToolCalls(t, 500, 1)},
	}
	for _, in := range inputs {
		encoded, _, err := encodeTurnFrame(in)
		require.NoError(t, err)
		var text strings.Builder
		for _, e := range encoded {
			require.LessOrEqual(t, len(e.payload), turnFrameMaxBytes, "kind %s", in.Kind)
			text.WriteString(e.frame.Text)
		}
		if in.Kind == TurnFrameText {
			require.Equal(t, in.Text, text.String(), "split text must rejoin to the original")
		}
	}
}

// Text needing no escaping is split once, at the piece size, not halved.
func TestEncodeTurnTextSplitsPlainTextOnce(t *testing.T) {
	t.Parallel()

	encoded, _, err := encodeTurnFrame(TurnFrame{Kind: TurnFrameText, Text: strings.Repeat("t", 2*turnTextPieceBytes)})
	require.NoError(t, err)
	require.Len(t, encoded, 2)
}

// HTML characters are stored as they are rather than as six-byte escapes.
func TestEncodeTurnFrameDoesNotEscapeHTML(t *testing.T) {
	t.Parallel()

	encoded, _, err := encodeTurnFrame(TurnFrame{
		Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: json.RawMessage(`"<b>&</b>"`),
	})
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.Contains(t, string(encoded[0].payload), `"<b>&</b>"`)
}

func TestEncodeTurnFrameMarksANilToolOutput(t *testing.T) {
	t.Parallel()

	// The designed case where a tool row's content would not encode: the
	// frame still answers the call, and the marker sends the client to the
	// row instead of rendering an empty result.
	encoded, omitted, err := encodeTurnFrame(TurnFrame{Kind: TurnFrameToolOutput, ToolCallID: "c1"})
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.True(t, encoded[0].frame.BodyOmitted)
	require.Equal(t, "c1", encoded[0].frame.ToolCallID)
	require.Zero(t, omitted)
}

// An output that is not valid JSON cannot be stored as is; it is left out
// rather than the frame being dropped.
func TestEncodeTurnFrameOmitsAnOutputThatWillNotEncode(t *testing.T) {
	t.Parallel()

	encoded, omitted, err := encodeTurnFrame(TurnFrame{Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: json.RawMessage(`"\x"`)})
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.True(t, encoded[0].frame.BodyOmitted)
	require.Positive(t, omitted)
}

func TestEncodeTurnFrameReportsTheOmittedFrameSize(t *testing.T) {
	t.Parallel()

	frame := TurnFrame{Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: json.RawMessage(`"` + strings.Repeat("o", turnFrameMaxBytes) + `"`)}
	full, err := marshalTurnFrame(frame)
	require.NoError(t, err)

	encoded, omitted, err := encodeTurnFrame(frame)
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.Nil(t, encoded[0].frame.Output)
	require.Equal(t, len(full), omitted)
}

func TestEncodeTurnFrameKeepsTheSkeletonWhenItFits(t *testing.T) {
	t.Parallel()

	frame := TurnFrame{Kind: TurnFrameMessage, MessageID: "m1", ToolCalls: bigToolCalls(t, 2, turnFrameMaxBytes)}
	full, err := marshalTurnFrame(frame)
	require.NoError(t, err)

	encoded, omitted, err := encodeTurnFrame(frame)
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.True(t, encoded[0].frame.BodyOmitted)
	require.JSONEq(t,
		`[{"id":"call-0000","type":"function","function":{"name":"search_the_knowledge_base"}},`+
			`{"id":"call-0001","type":"function","function":{"name":"search_the_knowledge_base"}}]`,
		string(encoded[0].frame.ToolCalls))
	require.Equal(t, len(full), omitted)
}

// A row with so many calls that even their ids and names are over the cap
// publishes none of them; the client reads each call from the row.
func TestEncodeTurnFrameDropsASkeletonOverTheCap(t *testing.T) {
	t.Parallel()

	encoded, omitted, err := encodeTurnFrame(TurnFrame{Kind: TurnFrameMessage, MessageID: "m1", ToolCalls: bigToolCalls(t, 500, 1)})
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.True(t, encoded[0].frame.BodyOmitted)
	require.Nil(t, encoded[0].frame.ToolCalls)
	require.Equal(t, "m1", encoded[0].frame.MessageID)
	require.Greater(t, omitted, turnFrameMaxBytes)
}

func TestEncodeTurnFrameLeavesSmallFramesAlone(t *testing.T) {
	t.Parallel()

	in := TurnFrame{
		Kind: TurnFrameMessage, MessageID: "m1", FinishReason: "tool_calls",
		ToolCalls: json.RawMessage(`[{"id":"c1","type":"function","function":{"name":"search","arguments":"{}"}}]`),
	}
	encoded, omitted, err := encodeTurnFrame(in)
	require.NoError(t, err)
	require.Len(t, encoded, 1)
	require.Equal(t, in, encoded[0].frame)
	require.Zero(t, omitted)
}

func TestTurnStreamMetricsRecordFrameAndOmittedBytes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m := newTurnStreamMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	m.recordAppend(t.Context(), TurnFrame{Kind: TurnFrameToolOutput, BodyOmitted: true}, 120)
	m.recordAppend(t.Context(), TurnFrame{Kind: TurnFrameText}, 40)
	m.recordOmitted(t.Context(), TurnFrameToolOutput, 50_000)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	sums := map[string]int64{}
	counts := map[string]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			hist, ok := metric.Data.(metricdata.Histogram[int64])
			require.True(t, ok, "%s must be an int64 histogram", metric.Name)
			for _, dp := range hist.DataPoints {
				sums[metric.Name] += dp.Sum
				counts[metric.Name] += dp.Count
			}
		}
	}
	require.Equal(t, int64(160), sums[meterTurnStreamFrameBytes])
	require.Equal(t, uint64(2), counts[meterTurnStreamFrameBytes])
	require.Equal(t, int64(50_000), sums[meterTurnStreamOmittedFrameBytes])
}

func TestTurnStreamMetricsToleratesNil(t *testing.T) {
	t.Parallel()

	var m *turnStreamMetrics
	m.recordAppend(t.Context(), TurnFrame{Kind: TurnFrameText}, 1)
	m.recordOmitted(t.Context(), TurnFrameText, 1)
}
