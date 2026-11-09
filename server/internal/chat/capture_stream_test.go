package chat_test

import (
	"context"
	"encoding/json"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestCaptureRedactionPublishesOnlyCompleteSafeText(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "", "capture stream")
	stream := newTurnStream(t)
	writer, closeWriter := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { require.NoError(t, closeWriter(context.WithoutCancel(t.Context()))) })
	writer.WithTurnStream(stream)
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Role = "assistant"
	params.Content = "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT + " safe suffix"
	params.FinishReason = conv.ToPGText("stop")
	params.Source = conv.ToPGText("codex")
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}})
	require.NoError(t, err)
	frames, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, frames, 3)
	require.Equal(t, chat.TurnFrameText, frames[0].Kind)
	require.NotContains(t, frames[0].Text, captureFindingsSyntheticPAT)
	require.Contains(t, frames[0].Text, "safe suffix")
	require.Equal(t, chat.TurnFrameMessage, frames[1].Kind)
	require.Equal(t, chat.TurnFrameDone, frames[2].Kind)
	require.Contains(t, params.Content, captureFindingsSyntheticPAT, "the execution-side value is unchanged")
}

func TestCaptureStreamUsesStoredCrossFieldRedaction(t *testing.T) {
	t.Parallel()
	// This synthetic pair requires the access identifier as detector context.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	for _, live := range []bool{false, true} {
		name := "passive capture"
		if live {
			name = "live assistant"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			chatID := seedChat(t, ctx, ti, "", "", "capture stream")
			stream := newTurnStream(t)
			writer := captureWriter(t, ti, nil).WithTurnStream(stream)
			strategy := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, writer)
			msg := openrouter.CreateMessageAssistant("AWS_ACCESS_KEY_ID=" + accessID)
			msg.ChatAssistantMessage.ToolCalls = []or.ChatToolCall{{
				ID: "call_capture", Type: or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{Name: "shell", Arguments: `{"SecretAccessKey":"` + secret + `"}`},
			}}
			request := makeRequest(chatID, ti.projectID, ti.orgID, msg)
			request.UsageSource = billing.ModelUsageSource("codex")
			if live {
				request.UsageSource = billing.ModelUsageSourceAssistants
			}
			before, err := json.Marshal(request.Messages)
			require.NoError(t, err)

			_, err = strategy.StartOrResumeChat(ctx, request)
			require.NoError(t, err)
			frames, err := stream.Replay(ctx, chatID, "")
			require.NoError(t, err)
			encoded, err := json.Marshal(frames)
			require.NoError(t, err)
			if live {
				require.Len(t, frames, 1)
				require.Equal(t, chat.TurnFrameMessage, frames[0].Kind)
				require.Contains(t, string(encoded), secret)
			} else {
				require.Len(t, frames, 2)
				require.Equal(t, chat.TurnFrameText, frames[0].Kind)
				require.Equal(t, chat.TurnFrameMessage, frames[1].Kind)
				require.NotContains(t, string(encoded), secret)
				require.Contains(t, frames[0].Text, accessID)
			}
			after, err := json.Marshal(request.Messages)
			require.NoError(t, err)
			require.Equal(t, before, after, "capture must not mutate execution input")
		})
	}
}

func TestCaptureStreamUsesStoredPendingToolOutput(t *testing.T) {
	t.Parallel()
	const content = `{"AccessKeyId":"ASIAZ2XY3WNBQR5TUVWX","SecretAccessKey":"wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd","note":"keep useful output"}`
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "", "pending capture stream")
	stream := newTurnStream(t)
	writer := captureWriter(t, ti, nil).WithTurnStream(stream)
	strategy := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, writer)
	request := makeRequest(chatID, ti.projectID, ti.orgID, or.CreateChatMessagesTool(or.ChatToolMessage{
		Role: or.ChatToolMessageRoleTool, Content: or.CreateChatToolMessageContentStr(content), ToolCallID: "call_capture",
	}))
	request.UsageSource = billing.ModelUsageSource("codex")
	session := chat.TestingNewPoisonedSession(chatID, ti.projectID, "", "", request.Model, request.UsageSource, 0, request.Messages)
	response := makeResponse("done")
	response.FinishReason = new("stop")

	require.NoError(t, strategy.CaptureMessage(ctx, session, request, response))
	frames, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, frames, 4)
	require.Equal(t, chat.TurnFrameToolOutput, frames[0].Kind)
	require.Contains(t, string(frames[0].Output), "keep useful output")
	require.NotContains(t, string(frames[0].Output), "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd")
	require.Equal(t, chat.TurnFrameText, frames[1].Kind)
	require.Equal(t, chat.TurnFrameMessage, frames[2].Kind)
	require.Equal(t, chat.TurnFrameDone, frames[3].Kind)
	require.JSONEq(t, content, openrouter.GetText(request.Messages[0]))
}
