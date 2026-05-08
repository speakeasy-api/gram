package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractDataPayload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		event   string
		wantOK  bool
		wantPay string
	}{
		{name: "simple LF", event: "data: {\"x\":1}\n\n", wantOK: true, wantPay: `{"x":1}`},
		{name: "CRLF", event: "data: {\"x\":1}\r\n\r\n", wantOK: true, wantPay: `{"x":1}`},
		{name: "DONE sentinel", event: "data: [DONE]\n\n", wantOK: true, wantPay: "[DONE]"},
		{name: "id then data", event: "id: abc\ndata: {\"x\":1}\n\n", wantOK: true, wantPay: `{"x":1}`},
		{name: "no data line", event: ": comment\n\n", wantOK: false},
		{name: "empty", event: "", wantOK: false},
	}

	for _, tc := range cases {
		line, payload, ok := extractDataPayload(tc.event)
		require.Equal(t, tc.wantOK, ok, tc.name)
		if tc.wantOK {
			require.Equal(t, tc.wantPay, payload, tc.name)
			require.Contains(t, tc.event, line, "%s: line %q must be substring of event", tc.name, line)
		}
	}
}

func TestIsFinalFrame(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "delta only", payload: `{"choices":[{"delta":{"content":"hi"}}]}`, want: false},
		{name: "finish_reason without usage", payload: `{"choices":[{"finish_reason":"stop"}]}`, want: false},
		{name: "usage present", payload: `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, want: true},
		{name: "usage null", payload: `{"choices":[],"usage":null}`, want: false},
		{name: "finish_reason and usage", payload: `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, want: true},
		{name: "no choices, no usage", payload: `{"id":"abc"}`, want: false},
	}

	for _, tc := range cases {
		var obj map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(tc.payload), &obj), tc.name)
		require.Equal(t, tc.want, isFinalFrame(obj), tc.name)
	}
}

func TestMaybeInjectContextWindow_SkipsDoneSentinel(t *testing.T) {
	t.Parallel()

	got, ok := maybeInjectContextWindow("data: [DONE]\n\n", func() int { return 200000 })
	require.False(t, ok)
	require.Equal(t, "data: [DONE]\n\n", got)
}

func TestMaybeInjectContextWindow_SkipsNonFinalFrame(t *testing.T) {
	t.Parallel()

	event := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 200000 })
	require.False(t, ok)
	require.Equal(t, event, got)
}

func TestMaybeInjectContextWindow_SkipsWhenContextWindowZero(t *testing.T) {
	t.Parallel()

	event := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 0 })
	require.False(t, ok)
	require.Equal(t, event, got)
}

func TestMaybeInjectContextWindow_SkipsFinishReasonOnly(t *testing.T) {
	t.Parallel()

	event := "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 200000 })
	require.False(t, ok)
	require.Equal(t, event, got)
}

func TestMaybeInjectContextWindow_InjectsOnUsageFrame(t *testing.T) {
	t.Parallel()

	event := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 8192 })
	require.True(t, ok)

	payload := strings.TrimSuffix(strings.TrimPrefix(got, "data: "), "\n\n")
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(payload), &obj))
	require.Contains(t, obj, "gram_metadata")
	require.Contains(t, obj, "usage")
}

func TestMaybeInjectContextWindow_PreservesCRLF(t *testing.T) {
	t.Parallel()

	event := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\r\n\r\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 4096 })
	require.True(t, ok)
	require.True(t, strings.HasSuffix(got, "\r\n\r\n"))
}

func TestMaybeInjectContextWindow_SkipsMalformedJSON(t *testing.T) {
	t.Parallel()

	event := "data: {not json\n\n"
	got, ok := maybeInjectContextWindow(event, func() int { return 4096 })
	require.False(t, ok)
	require.Equal(t, event, got)
}
