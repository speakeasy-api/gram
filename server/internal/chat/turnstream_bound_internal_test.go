package chat

import (
	"encoding/json"
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

func TestBoundTurnFrameKeepsEveryFrameUnderTheCap(t *testing.T) {
	t.Parallel()

	bigOutput, err := json.Marshal(strings.Repeat("o", 3*turnFrameMaxBodyBytes))
	require.NoError(t, err)
	bigArgs, err := json.Marshal(map[string]string{"q": strings.Repeat("a", 3*turnFrameMaxBodyBytes)})
	require.NoError(t, err)
	bigCalls, err := json.Marshal([]map[string]any{{
		"id": "c1", "type": "function",
		"function": map[string]string{"name": "search", "arguments": string(bigArgs)},
	}})
	require.NoError(t, err)

	inputs := []TurnFrame{
		{Kind: TurnFrameText, Text: strings.Repeat("t", 3*turnFrameMaxBodyBytes+5)},
		{Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: bigOutput},
		{Kind: TurnFrameMessage, MessageID: "m1", ToolCalls: bigCalls},
	}
	for _, in := range inputs {
		frames, _ := boundTurnFrame(in)
		for _, f := range frames {
			payload, err := json.Marshal(f)
			require.NoError(t, err)
			// The body is capped; the envelope (kind, ids) adds a little.
			require.Less(t, len(payload), turnFrameMaxBodyBytes+512, "kind %s", in.Kind)
		}
	}
}

func TestBoundTurnFrameMarksANilToolOutput(t *testing.T) {
	t.Parallel()

	// The designed case where a tool row's content would not encode: the
	// frame still answers the call, and the marker sends the client to the
	// row instead of rendering an empty result.
	frames, omitted := boundTurnFrame(TurnFrame{Kind: TurnFrameToolOutput, ToolCallID: "c1"})
	require.Len(t, frames, 1)
	require.True(t, frames[0].BodyOmitted)
	require.Equal(t, "c1", frames[0].ToolCallID)
	require.Zero(t, omitted)
}

func TestBoundTurnFrameReportsOmittedBytes(t *testing.T) {
	t.Parallel()

	output := json.RawMessage(`"` + strings.Repeat("o", turnFrameMaxBodyBytes) + `"`)
	frames, omitted := boundTurnFrame(TurnFrame{Kind: TurnFrameToolOutput, ToolCallID: "c1", Output: output})
	require.Len(t, frames, 1)
	require.Nil(t, frames[0].Output)
	require.Equal(t, len(output), omitted)
}

func TestBoundTurnFrameLeavesSmallFramesAlone(t *testing.T) {
	t.Parallel()

	in := TurnFrame{
		Kind: TurnFrameMessage, MessageID: "m1", FinishReason: "tool_calls",
		ToolCalls: json.RawMessage(`[{"id":"c1","type":"function","function":{"name":"search","arguments":"{}"}}]`),
	}
	frames, omitted := boundTurnFrame(in)
	require.Equal(t, []TurnFrame{in}, frames)
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
	require.Equal(t, int64(50_000), sums[meterTurnStreamOmittedBodyBytes])
}

func TestTurnStreamMetricsToleratesNil(t *testing.T) {
	t.Parallel()

	var m *turnStreamMetrics
	m.recordAppend(t.Context(), TurnFrame{Kind: TurnFrameText}, 1)
	m.recordOmitted(t.Context(), TurnFrameText, 1)
}
