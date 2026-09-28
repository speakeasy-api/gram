package openrouter

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
	"github.com/stretchr/testify/require"
)

func TestPrefilterPayloadPreservesSmallEvidence(t *testing.T) {
	t.Parallel()
	msg := judgemessage.New(message.ToolResponse, "search", "ordinary content")
	trajectory := judgemessage.Trajectory{PriorUserRequest: "find the answer", RecentUntrustedContent: "previous result"}
	want, wantContent := prepareJudgePayload(msg, trajectory)
	got, content, truncated, err := preparePrefilterPayload(msg, trajectory, PrefilterQuestions(), maxPrefilterInputBytes)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, want, got)
	require.Equal(t, wantContent, content)
}

func TestPrefilterPayloadBoundsWholeMultiToolEvidence(t *testing.T) {
	t.Parallel()
	msg := judgemessage.New(message.ToolRequest, "", "")
	arguments := "HEAD" + strings.Repeat("界😀<&\x00", 4000) + "TAIL"
	for range 50 {
		msg.ToolCalls = append(msg.ToolCalls, judgemessage.NewToolCall("mcp__"+strings.Repeat("界", 300)+"__"+strings.Repeat("😀", 300), arguments))
	}
	trajectory := judgemessage.Trajectory{PriorUserRequest: strings.Repeat("界<&", 4000), RecentUntrustedContent: strings.Repeat("😀<&", 4000)}
	questions := PrefilterQuestions()
	questionJSON, err := json.Marshal(questions)
	require.NoError(t, err)
	prepared, content, truncated, err := preparePrefilterPayload(msg, trajectory, questions, maxPrefilterInputBytes)
	require.NoError(t, err)
	require.True(t, truncated)
	require.LessOrEqual(t, len(prepared)+len(questionJSON), maxPrefilterInputBytes)
	require.True(t, utf8.Valid(prepared))
	var payload judgePayload
	require.NoError(t, json.Unmarshal(prepared, &payload))
	require.Len(t, payload.Message.ToolCalls, 50)
	for _, call := range payload.Message.ToolCalls {
		require.True(t, call.ArgumentsTruncated)
		require.True(t, utf8.ValidString(call.Arguments))
		require.True(t, strings.HasPrefix(call.Arguments, "HEAD"))
		require.True(t, strings.HasSuffix(call.Arguments, "TAIL"))
		require.Contains(t, call.Arguments, "[truncated]")
	}
	require.Equal(t, judgePayloadContent(payload), content)
	// Preparing Jev evidence must not mutate the target used by Opus.
	require.Equal(t, arguments, msg.ToolCalls[0].Arguments)
}

func TestPrefilterPayloadRejectsOversizedQuestions(t *testing.T) {
	t.Parallel()
	questions := map[string]typesafe.Question{"q": {Type: "noul", Instructions: strings.Repeat("x", maxPrefilterInputBytes), Criteria: nil}}
	_, _, _, err := preparePrefilterPayload(judgemessage.New(message.User, "", "hello"), judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}, questions, maxPrefilterInputBytes)
	require.ErrorContains(t, err, "metadata exceeds input budget")
}
