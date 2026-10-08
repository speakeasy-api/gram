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
	got, content, truncated, err := preparePrefilterPayload(msg, trajectory, PrefilterQuestions(), maxPrefilterInputTokens)
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
	prepared, content, truncated, err := preparePrefilterPayload(msg, trajectory, questions, maxPrefilterInputTokens)
	require.NoError(t, err)
	require.True(t, truncated)
	require.LessOrEqual(t, estimatePrefilterTokens(prepared, questionJSON), maxPrefilterInputTokens)
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
	// Preparing Jev evidence must not mutate the target used by the confirmer.
	require.Equal(t, arguments, msg.ToolCalls[0].Arguments)
}

func TestPrefilterPayloadRejectsOversizedQuestions(t *testing.T) {
	t.Parallel()
	questions := map[string]typesafe.Question{"q": {Type: "noul", Instructions: strings.Repeat("x", maxPrefilterInputTokens*4), Criteria: nil}}
	_, _, _, err := preparePrefilterPayload(judgemessage.New(message.User, "", "hello"), judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}, questions, maxPrefilterInputTokens)
	require.ErrorContains(t, err, "metadata exceeds input budget")
}

func TestPrefilterEstimateCountsCharactersAndRoundsUp(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2, estimatePrefilterTokens([]byte("界😀abc"), nil))
	require.Equal(t, 2, estimatePrefilterTokens([]byte("abc"), []byte("defgh")))
	require.Equal(t, 3, estimatePrefilterTokens([]byte("abc"), []byte("defghi")))
}

func TestPrefilterPreservesEvidenceAboveOldByteBudget(t *testing.T) {
	t.Parallel()
	msg := judgemessage.New(message.ToolRequest, "", "")
	for range 2 {
		msg.ToolCalls = append(msg.ToolCalls, judgemessage.NewToolCall("search", strings.Repeat("😀", 16000)))
	}
	trajectory := judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}
	want, wantContent := prepareJudgePayload(msg, trajectory)
	require.Greater(t, len(want), 28000)
	got, content, truncated, err := preparePrefilterPayload(msg, trajectory, PrefilterQuestions(), maxPrefilterInputTokens)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, want, got)
	require.Equal(t, wantContent, content)
}
