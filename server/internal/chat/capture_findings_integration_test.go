package chat_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Synthetic provider-shaped credentials are never issued by a provider.
const captureFindingsSyntheticPAT = "ghp_b847c2d93a5e4f6071cb9368d014a2e75f69"

type captureFindingInspector struct {
	inputs []chat.CaptureInput
	err    error
}

func (c *captureFindingInspector) InspectCapture(_ context.Context, input chat.CaptureInput) ([]*riskv1.Finding, error) {
	c.inputs = append(c.inputs, input)
	if c.err != nil {
		return nil, c.err
	}
	if !strings.Contains(input.Content, captureFindingsSyntheticPAT) && !strings.Contains(string(input.ToolCalls), captureFindingsSyntheticPAT) {
		return nil, nil
	}
	anchor := input.MessageID
	if anchor == uuid.Nil {
		anchor = input.ContentPartID
	}
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(anchor.String()))
	fingerprint := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	finding := riskv1.Finding_builder{
		Id: new(id.String()), OrganizationId: new(input.OrgID), ProjectId: new(input.ProjectID.String()),
		CreatedAt: new(time.Now().UTC().Format(time.RFC3339)), RuleId: new("secret.github_pat"), Source: new("gitleaks"),
		Tags: []string{"capture_redaction"},
		SafeMatch: riskv1.Finding_SafeMatch_builder{
			Version: new(uint32(1)), PreparedAt: new(time.Now().UTC().Format(time.RFC3339)),
			MatchLen: new(uint32(len(captureFindingsSyntheticPAT))), MatchRedacted: new("[redacted]"),
			FingerprintPepperVersion: new("test"), FingerprintGlobalHs256: &fingerprint, FingerprintTenantHs256: &fingerprint,
			CaptureRedacted: new(true),
		}.Build(),
	}.Build()
	if input.MessageID != uuid.Nil {
		finding.SetChatMessageId(input.MessageID.String())
	}
	if input.ContentPartID != uuid.Nil {
		finding.SetContentPartId(input.ContentPartID.String())
	}
	return []*riskv1.Finding{finding}, nil
}

func captureFindingMessages(t *testing.T, ti *chatTestInstance) []*riskv1.Finding {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var messages []*riskv1.Finding
	for _, row := range rows {
		if row.Topic != string(proto.MessageName(&riskv1.Finding{})) {
			continue
		}
		finding := new(riskv1.Finding)
		require.NoError(t, proto.Unmarshal(row.Message, finding))
		require.NotContains(t, string(row.Message), captureFindingsSyntheticPAT)
		require.False(t, finding.HasMatch())
		require.NotNil(t, finding.GetSafeMatch())
		messages = append(messages, finding)
	}
	return messages
}

func captureWriter(t *testing.T, ti *chatTestInstance, inspector chat.CaptureInspector) *chat.ChatMessageWriter {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	return writer.WithCaptureInspector(inspector)
}

func TestCaptureFindingsCommitWithRedactedHookMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "captured hook")
	inspector := new(captureFindingInspector)
	writer := captureWriter(t, ti, inspector)
	text := "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content, params.ContentRaw = text, []byte(`"`+text+`"`)
	params.Source = conv.ToPGText("codex")
	writes := []chat.MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}}

	n, err := writer.Write(ctx, ti.projectID, writes)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Len(t, inspector.inputs, 1)
	require.Equal(t, text, inspector.inputs[0].Content)
	require.Equal(t, text, writes[0].Params.Content, "capture must leave the caller's input untouched")
	require.Equal(t, params.ContentRaw, writes[0].Params.ContentRaw)
	stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
	require.Len(t, stored, 1)
	require.NotContains(t, stored[0].Content, captureFindingsSyntheticPAT)
	require.NotContains(t, string(stored[0].ContentRaw), captureFindingsSyntheticPAT)
	findings := captureFindingMessages(t, ti)
	require.Len(t, findings, 1)
	require.Equal(t, stored[0].ID.String(), findings[0].GetChatMessageId())
}

func TestCaptureFindingsRollbackWithMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "rolled back capture")
	writer := captureWriter(t, ti, new(captureFindingInspector))
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	writes := []chat.MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}}
	tx := testenv.BeginTx(t, ctx, ti.conn)
	n, err := writer.WriteInTx(ctx, tx, writes)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.NoError(t, tx.Rollback(ctx))

	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, captureFindingMessages(t, ti), "finding and message must roll back together")
}

func TestCaptureFindingsInspectionFailureStoresNoMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "failed capture")
	writer := captureWriter(t, ti, &captureFindingInspector{err: errors.New("fingerprint preparation unavailable")})
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT

	n, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}})
	require.Error(t, err)
	require.Zero(t, n)
	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, captureFindingMessages(t, ti))
}

func TestCaptureFindingsCommitWithImportedMessageOnce(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "imported capture")
	inspector := new(captureFindingInspector)
	writer := captureWriter(t, ti, inspector)
	text := "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	params := repo.CreateExternalChatMessageParams{
		ID: uuid.New(), ChatID: chatID, ProjectID: ti.projectID, Role: "user", Content: text,
		ContentRaw: []byte(`"` + text + `"`), ExternalMessageID: conv.ToPGText("capture-import-1"),
	}
	writes := []chat.ExternalMessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceImport}}
	n, err := writer.WriteExternal(ctx, ti.projectID, writes)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	n, err = writer.WriteExternal(ctx, ti.projectID, writes)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, text, writes[0].Params.Content)
	require.Equal(t, text, inspector.inputs[0].Content)
	stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
	require.Len(t, stored, 1)
	require.NotContains(t, stored[0].Content, captureFindingsSyntheticPAT)
	require.NotContains(t, string(stored[0].ContentRaw), captureFindingsSyntheticPAT)
	findings := captureFindingMessages(t, ti)
	require.Len(t, findings, 1, "an imported retry must not duplicate its finding")
	require.Equal(t, stored[0].ID.String(), findings[0].GetChatMessageId())
}

func TestCaptureFindingsLeaveLiveAssistantHistoryUntouched(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "live assistant")
	inspector := &captureFindingInspector{err: errors.New("must not inspect execution state")}
	writer := captureWriter(t, ti, inspector)
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	params.Source = conv.ToPGText("codex")

	n, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceAssistant}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Empty(t, inspector.inputs)
	stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
	require.Len(t, stored, 1)
	require.Equal(t, params.Content, stored[0].Content)
	require.Empty(t, captureFindingMessages(t, ti))
}

func TestCaptureFindingsCommitWithRedactedAttachment(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "captured attachment")
	inspector := new(captureFindingInspector)
	writer := captureWriter(t, ti, inspector)
	original := []byte("MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT)
	assetURL, err := writer.WriteContentPartAsset(ctx, ti.projectID, chatID, original)
	require.NoError(t, err)
	part := repo.CreateChatContentPartParams{
		ChatID: chatID, ProjectID: ti.projectID, Kind: message.PromptAttachment,
		ContentAssetUrl: assetURL, Source: conv.ToPGText("codex"), CreatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
	}
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, writer.WriteCapturedContentParts(ctx, tx, ti.orgID, "u", []repo.CreateChatContentPartParams{part}, [][]byte{original}))
	require.NoError(t, tx.Commit(ctx))
	body, err := blobio.ReadAllString(ctx, ti.assets, assetURL, chat.MaxAssetReadSize)
	require.NoError(t, err)
	require.NotContains(t, body, captureFindingsSyntheticPAT)
	require.Equal(t, string(original), inspector.inputs[0].Content)
	findings := captureFindingMessages(t, ti)
	require.Len(t, findings, 1)
	parts, err := repo.New(ti.conn).ListChatContentPartsByChatID(ctx, repo.ListChatContentPartsByChatIDParams{ProjectID: ti.projectID, ChatID: chatID})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, parts[0].ID.String(), findings[0].GetContentPartId())
}

func TestCaptureFindingsCodingReplayKeepsGenerationAndWireInput(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := uuid.New()
	inspector := new(captureFindingInspector)
	writer := captureWriter(t, ti, inspector)
	strategy := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, writer)
	text := "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	first := makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser(text))
	first.UsageSource = billing.ModelUsageSource("codex")
	firstWire, err := json.Marshal(first.Messages)
	require.NoError(t, err)
	runTurn(t, ctx, strategy, first, makeResponse("done"))
	second := makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser(text), openrouter.CreateMessageAssistant("done"), openrouter.CreateMessageUser("follow-up"))
	second.UsageSource = billing.ModelUsageSource("codex")
	runTurn(t, ctx, strategy, second, makeResponse("finished"))
	afterWire, err := json.Marshal(first.Messages)
	require.NoError(t, err)
	require.Equal(t, firstWire, afterWire)
	require.Contains(t, string(afterWire), captureFindingsSyntheticPAT)
	stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
	require.Len(t, stored, 4)
	for _, row := range stored {
		require.Zero(t, row.Generation, "redaction must not create false replay divergence")
		require.NotContains(t, row.Content, captureFindingsSyntheticPAT)
	}
	require.Len(t, captureFindingMessages(t, ti), 1)
}

func TestCaptureFindingsCredentialOnlyEditStartsNewGeneration(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := uuid.New()
	writer := captureWriter(t, ti, nil)
	strategy := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, writer)
	const editedPAT = "ghp_4e2b9c736f8a105d6b03c172e59a64b8d07f"
	firstText := "MISE_GITHUB_TOKEN=" + captureFindingsSyntheticPAT
	secondText := "MISE_GITHUB_TOKEN=" + editedPAT
	first := makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser(firstText))
	first.UsageSource = billing.ModelUsageSource("codex")
	runTurn(t, ctx, strategy, first, makeResponse("done"))
	second := makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser(secondText))
	second.UsageSource = billing.ModelUsageSource("codex")
	runTurn(t, ctx, strategy, second, makeResponse("revised"))

	require.Equal(t, firstText, openrouter.GetText(first.Messages[0]))
	require.Equal(t, secondText, openrouter.GetText(second.Messages[0]))
	stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
	require.Len(t, stored, 4)
	users := make(map[int32]repo.ChatMessage)
	for _, row := range stored {
		require.NotContains(t, row.Content, captureFindingsSyntheticPAT)
		require.NotContains(t, row.Content, editedPAT)
		if row.Role == "user" {
			users[row.Generation] = row
		}
	}
	require.Len(t, users, 2)
	require.Equal(t, "MISE_GITHUB_TOKEN=[redacted]", users[0].Content)
	require.Equal(t, users[0].Content, users[1].Content, "stored placeholders intentionally match")
	require.NotEqual(t, users[0].ContentHash, users[1].ContentHash, "original capture identities must still distinguish the edit")
}
