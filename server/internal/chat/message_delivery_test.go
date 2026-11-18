package chat_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/transcript"
)

const deliveryOwner = "delivery-owner"

type deliveryEnv struct {
	ti     *chatTestInstance
	writer *chat.ChatMessageWriter
	chatID uuid.UUID
}

func newDeliveryEnv(t *testing.T) *deliveryEnv {
	t.Helper()
	ti := newTestChatService(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	return &deliveryEnv{ti: ti, writer: writer, chatID: seedChat(t, t.Context(), ti, deliveryOwner, "", "transcript delivery")}
}

func (env *deliveryEnv) target(chatID uuid.UUID) chat.DeliveryTarget {
	return chat.DeliveryTarget{
		ProjectID: env.ti.projectID,
		ChatID:    chatID,
		Sender:    chat.TranscriptAccount{UserAccountID: uuid.Nil, UserID: deliveryOwner, Email: ""},
	}
}

func deliveryWrite(d transcript.Delivery) chat.DeliveryWrite {
	return chat.DeliveryWrite{
		Delivery: d,
		Template: chat.ExternalMessageWrite{
			Params: repo.CreateExternalChatMessageParams{
				Model:  conv.ToPGText("claude-opus-5"),
				UserID: conv.ToPGText(deliveryOwner),
				Origin: conv.ToPGText(chat.OriginClaudeTranscript),
				Source: conv.ToPGText("claude-code"),
			},
			BillingUserID:  deliveryOwner,
			WorkloadSource: metering.WorkloadSourceHook,
			UserEmail:      "owner@example.test",
			Provider:       "anthropic",
		},
	}
}

func (env *deliveryEnv) deliver(t *testing.T, chatID uuid.UUID, d transcript.Delivery) chat.DeliveryResult {
	t.Helper()
	res, err := env.writer.WriteDelivery(t.Context(), env.target(chatID), deliveryWrite(d))
	require.NoError(t, err)
	return res
}

type treeRow = repo.ListChatTranscriptRowsForTestRow

func listTreeRows(t *testing.T, env *deliveryEnv, chatID uuid.UUID) []treeRow {
	t.Helper()
	rows, err := repo.New(env.ti.conn).ListChatTranscriptRowsForTest(t.Context(), repo.ListChatTranscriptRowsForTestParams{ChatID: chatID, ProjectID: env.ti.projectID})
	require.NoError(t, err)
	return rows
}

func rowByContent(t *testing.T, rows []treeRow, content string) treeRow {
	t.Helper()
	var found []treeRow
	for _, r := range rows {
		if r.Content == content {
			found = append(found, r)
		}
	}
	require.Len(t, found, 1, "rows with content %q", content)
	return found[0]
}

// naturalIDsOf returns a message's natural IDs as sorted "kind:value" strings.
func naturalIDsOf(t *testing.T, env *deliveryEnv, chatID, messageID uuid.UUID) []string {
	t.Helper()
	rows, err := repo.New(env.ti.conn).ListChatNaturalIDsForMessages(t.Context(), repo.ListChatNaturalIDsForMessagesParams{ChatID: chatID, ProjectID: env.ti.projectID, ChatMessageIds: []uuid.UUID{messageID}})
	require.NoError(t, err)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Kind+":"+r.Value)
	}
	slices.Sort(out)
	return out
}

func countNaturalIDs(t *testing.T, env *deliveryEnv, chatID uuid.UUID) int {
	t.Helper()
	rows, err := repo.New(env.ti.conn).ListChatNaturalIDs(t.Context(), repo.ListChatNaturalIDsParams{ChatID: chatID, ProjectID: env.ti.projectID})
	require.NoError(t, err)
	return len(rows)
}

// chatReadings returns the operation id of every meter reading for the chat.
func chatReadings(t *testing.T, env *deliveryEnv, chatID uuid.UUID) []string {
	t.Helper()
	var out []string
	for _, reading := range meterMessages(t, env.ti) {
		if reading.GetAttributes()[metering.AttributeChatID] == chatID.String() {
			out = append(out, reading.GetOperationId())
		}
	}
	return out
}

func chatEvents(t *testing.T, env *deliveryEnv, chatID uuid.UUID) int {
	t.Helper()
	n := 0
	for _, event := range conversationMessages(t, env.ti) {
		if event.GetConversationId() == chatID.String() {
			n++
		}
	}
	return n
}

func claudeLine(id string) transcript.NaturalID {
	return transcript.NaturalID{Kind: transcript.KindClaudeEntry, Value: id, Block: 0}
}

func claudeParent(id string) *transcript.NaturalID {
	return &transcript.NaturalID{Kind: transcript.KindClaudeEntry, Value: id, Block: transcript.LastBlock}
}

func claudePrompt(line, parent, prompt, text string) transcript.Entry {
	e := transcript.Entry{Role: transcript.RoleUser, Text: text, IDs: []transcript.NaturalID{claudeLine(line), {Kind: transcript.KindPrompt, Value: prompt, Block: 0}}}
	if parent != "" {
		e.Parent = claudeParent(parent)
	}
	return e
}

func claudeReply(line, parent, text string) transcript.Entry {
	return transcript.Entry{Role: transcript.RoleAssistant, Text: text, IDs: []transcript.NaturalID{claudeLine(line)}, Parent: claudeParent(parent)}
}

func claudeToolUse(line, parent, callID string) transcript.Entry {
	return transcript.Entry{
		Role:   transcript.RoleAssistant,
		Call:   &transcript.ToolCall{ID: callID, Name: "Bash", Input: []byte(`{"command":"ls"}`)},
		IDs:    []transcript.NaturalID{claudeLine(line), {Kind: transcript.KindToolCall, Value: callID, Block: 0}},
		Parent: claudeParent(parent),
	}
}

func claudeToolResult(line, parent, callID, out string) transcript.Entry {
	return transcript.Entry{
		Role:   transcript.RoleTool,
		Text:   out,
		Result: &transcript.ToolResult{CallID: callID, Text: out},
		IDs:    []transcript.NaturalID{claudeLine(line), {Kind: transcript.KindToolResult, Value: callID, Block: 0}},
		Parent: claudeParent(parent),
	}
}

func claudeDelivery(entries ...transcript.Entry) transcript.Delivery {
	return transcript.Delivery{Source: transcript.SourceClaudeTranscript, Links: nil, Entries: entries}
}

func inferenceFrame(msgs ...transcript.Message) transcript.Delivery {
	d := transcript.Delivery{Source: transcript.SourceAnthropicInference, Links: nil, Entries: nil}
	for _, m := range msgs {
		d.Entries = append(d.Entries, transcript.Split(m)...)
	}
	return d
}

func userText(text string) transcript.Message {
	return transcript.Message{Role: transcript.RoleUser, Blocks: []transcript.Block{{Type: "text", Text: text}}}
}

func assistantText(text string) transcript.Message {
	return transcript.Message{Role: transcript.RoleAssistant, Blocks: []transcript.Block{{Type: "text", Text: text}}}
}

func TestWriteDelivery_SameDeliveryTwiceStoresOneSet(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	d := claudeDelivery(
		claudePrompt("u1", "", "p1", "list files"),
		claudeToolUse("u2", "u1", "toolu_1"),
		claudeToolResult("u3", "u2", "toolu_1", "a.go"),
		claudeReply("u4", "u3", "one file"),
	)

	first := env.deliver(t, env.chatID, d)
	require.Len(t, first.Added, 4)
	require.Empty(t, first.Seen)
	rows := listTreeRows(t, env, env.chatID)
	require.Len(t, rows, 4)
	require.Len(t, chatReadings(t, env, env.chatID), 4)
	require.Equal(t, 4, chatEvents(t, env, env.chatID))
	idCount := countNaturalIDs(t, env, env.chatID)

	second := env.deliver(t, env.chatID, d)
	require.Empty(t, second.Added, "a re-sent delivery adds nothing")
	require.Len(t, second.Seen, 4)
	for i, s := range second.Seen {
		require.Equal(t, first.Added[i].MessageID, s.MessageID)
	}
	require.Len(t, listTreeRows(t, env, env.chatID), 4)
	require.Len(t, chatReadings(t, env, env.chatID), 4, "a re-sent delivery meters nothing")
	require.Equal(t, 4, chatEvents(t, env, env.chatID), "a re-sent delivery announces nothing")
	require.Equal(t, idCount, countNaturalIDs(t, env, env.chatID))

	// The tool rows keep the shape the other capture paths store.
	require.Equal(t, first.Added[1].MessageID, rows[1].ID)
	require.JSONEq(t, `[{"id":"toolu_1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]`, string(rows[1].ToolCalls))
	require.Equal(t, "tool", rows[2].Role)
	require.Equal(t, "a.go", rows[2].Content)
}

func TestWriteDelivery_GrowingInferenceFrameAddsOnlyTheNewTurn(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)

	first := env.deliver(t, env.chatID, inferenceFrame(userText("hi"), assistantText("hello")))
	require.Len(t, first.Added, 2)

	second := env.deliver(t, env.chatID, inferenceFrame(userText("hi"), assistantText("hello"), userText("next"), assistantText("done")))
	require.Len(t, second.Seen, 2)
	require.Equal(t, []chat.DeliveryEntry{{Index: 2, MessageID: second.Added[0].MessageID}, {Index: 3, MessageID: second.Added[1].MessageID}}, second.Added)

	rows := listTreeRows(t, env, env.chatID)
	require.Len(t, rows, 4)
	hi, hello, next, done := rowByContent(t, rows, "hi"), rowByContent(t, rows, "hello"), rowByContent(t, rows, "next"), rowByContent(t, rows, "done")
	require.False(t, hi.ParentMessageID.Valid, "the first prompt is a root")
	require.Equal(t, hi.ID, hello.ParentMessageID.UUID)
	require.Equal(t, hello.ID, next.ParentMessageID.UUID)
	require.Equal(t, next.ID, done.ParentMessageID.UUID)
	for _, r := range rows {
		require.True(t, r.HasSiblingKey, "rows without natural IDs deduplicate by content")
	}
	require.Len(t, chatReadings(t, env, env.chatID), 4)
}

func TestWriteDelivery_TranscriptAdoptsWhatInferenceStored(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	env.deliver(t, env.chatID, inferenceFrame(userText("hi"), assistantText("hello"), userText("yes"), assistantText("ok")))
	before := listTreeRows(t, env, env.chatID)
	require.Len(t, before, 4)

	res := env.deliver(t, env.chatID, claudeDelivery(
		claudePrompt("u1", "", "p1", "hi"),
		claudeReply("u2", "u1", "hello"),
		claudePrompt("u3", "u2", "p2", "yes"),
		claudeReply("u4", "u3", "ok"),
	))
	require.Empty(t, res.Added)
	require.Len(t, res.Seen, 4)

	after := listTreeRows(t, env, env.chatID)
	require.Len(t, after, 4, "no new rows")
	for i, r := range after {
		require.Equal(t, before[i].ID, r.ID)
		require.False(t, r.HasSiblingKey, "a row with natural IDs leaves the sibling index")
	}
	require.Equal(t, []string{"claude_entry:u1", "prompt:p1"}, naturalIDsOf(t, env, env.chatID, rowByContent(t, after, "hi").ID))
	require.Equal(t, []string{"claude_entry:u4"}, naturalIDsOf(t, env, env.chatID, rowByContent(t, after, "ok").ID))
	require.Len(t, chatReadings(t, env, env.chatID), 4, "seen entries meter nothing")
}

func TestWriteDelivery_RewindWithTheSamePromptBranches(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	res := env.deliver(t, env.chatID, claudeDelivery(
		claudePrompt("u1", "", "p1", "hi"),
		claudeReply("u2", "u1", "hello"),
		claudePrompt("u3", "u2", "p2", "yes"),
		claudeReply("u4", "u3", "ok"),
		// The user rewinds to u2 and types "yes" again.
		claudePrompt("u5", "u2", "p3", "yes"),
		claudeReply("u6", "u5", "sure"),
	))
	require.Len(t, res.Added, 6)

	rows := listTreeRows(t, env, env.chatID)
	hello := rowByContent(t, rows, "hello")
	var yes []treeRow
	for _, r := range rows {
		if r.Content == "yes" {
			yes = append(yes, r)
		}
	}
	require.Len(t, yes, 2, "two separate prompts")
	require.Equal(t, hello.ID, yes[0].ParentMessageID.UUID)
	require.Equal(t, hello.ID, yes[1].ParentMessageID.UUID)
	require.Equal(t, yes[0].ID, rowByContent(t, rows, "ok").ParentMessageID.UUID)
	require.Equal(t, yes[1].ID, rowByContent(t, rows, "sure").ParentMessageID.UUID)
}

func TestWriteDelivery_CompactionStartsASyntheticRoot(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	env.deliver(t, env.chatID, claudeDelivery(
		claudePrompt("u1", "", "p1", "hi"),
		claudeReply("u2", "u1", "hello"),
	))

	summary := transcript.Entry{Role: transcript.RoleUser, Text: "summary of the chat", Synthetic: true, IDs: []transcript.NaturalID{claudeLine("s1")}, Parent: claudeParent("b1")}
	res := env.deliver(t, env.chatID, transcript.Delivery{
		Source:  transcript.SourceClaudeTranscript,
		Links:   []transcript.Link{{ID: claudeLine("b1"), To: claudeParent("u2"), Compaction: true}},
		Entries: []transcript.Entry{summary, claudePrompt("u3", "s1", "p2", "go on"), claudeReply("u4", "u3", "done")},
	})
	require.Len(t, res.Added, 3)

	rows := listTreeRows(t, env, env.chatID)
	root := rowByContent(t, rows, "summary of the chat")
	require.False(t, root.ParentMessageID.Valid, "the summary is a tree root")
	require.Equal(t, rowByContent(t, rows, "hello").ID, root.ContinuesMessageID.UUID)
	require.Equal(t, chat.CaptureSynthetic, root.CaptureKind.String)
	require.True(t, root.RiskAnalyzed, "a synthetic row is stored already analyzed")
	require.Equal(t, root.ID, rowByContent(t, rows, "go on").ParentMessageID.UUID)
	require.False(t, rowByContent(t, rows, "go on").RiskAnalyzed)

	readings := chatReadings(t, env, env.chatID)
	require.NotContains(t, readings, "chat_message:"+root.ID.String(), "a synthetic row is not metered")
	require.Contains(t, readings, "chat_message:"+rowByContent(t, rows, "go on").ID.String())
	require.Len(t, readings, 4)
	require.Equal(t, 4, chatEvents(t, env, env.chatID), "a synthetic row is not announced")
}

func TestWriteDelivery_ConcurrentDeliveriesStoreOneSet(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	d := inferenceFrame(userText("hi"), assistantText("hello"), userText("again"), assistantText("sure"))

	const writers = 4
	errs := make([]error, writers)
	added := make([]int, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			res, err := env.writer.WriteDelivery(t.Context(), env.target(env.chatID), deliveryWrite(d))
			errs[i], added[i] = err, len(res.Added)
		})
	}
	wg.Wait()
	total := 0
	for i := range writers {
		require.NoError(t, errs[i])
		total += added[i]
	}
	require.Equal(t, 4, total, "exactly one writer adds the entries")
	require.Len(t, listTreeRows(t, env, env.chatID), 4)
	require.Len(t, chatReadings(t, env, env.chatID), 4)
}

func TestWriteDelivery_ADifferentAccountWritesNothing(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	d := claudeDelivery(claudePrompt("u1", "", "p1", "hi"))

	for name, sender := range map[string]chat.TranscriptAccount{
		"different user":         {UserAccountID: uuid.Nil, UserID: "someone-else", Email: ""},
		"no comparable identity": {UserAccountID: uuid.New(), UserID: "", Email: "owner@example.test"},
		"no identity":            {UserAccountID: uuid.Nil, UserID: "", Email: ""},
	} {
		target := env.target(env.chatID)
		target.Sender = sender
		_, err := env.writer.WriteDelivery(t.Context(), target, deliveryWrite(d))
		require.ErrorIs(t, err, chat.ErrAccountMismatch, name)
	}
	require.Empty(t, listTreeRows(t, env, env.chatID))
	require.Zero(t, countNaturalIDs(t, env, env.chatID))
	require.Empty(t, chatReadings(t, env, env.chatID))

	// A chat in another project is refused before the account is compared.
	target := env.target(seedChatInProject(t, env.ti, createProjectInSameOrg(t, env.ti), "other project"))
	_, err := env.writer.WriteDelivery(t.Context(), target, deliveryWrite(d))
	require.ErrorIs(t, err, chat.ErrChatNotInProject)
}

func TestWriteDelivery_NaturalIDsMatchOnlyInTheirChat(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	other := seedChat(t, t.Context(), env.ti, deliveryOwner, "", "other session")
	d := claudeDelivery(
		claudePrompt("u1", "", "p1", "list files"),
		claudeToolUse("u2", "u1", "toolu_shared"),
	)
	env.deliver(t, other, d)

	res := env.deliver(t, env.chatID, d)
	require.Len(t, res.Added, 2, "IDs stored in another chat do not match")
	rows := listTreeRows(t, env, env.chatID)
	require.Len(t, rows, 2)
	require.Contains(t, naturalIDsOf(t, env, env.chatID, rows[1].ID), "tool_call:toolu_shared")
	require.Len(t, listTreeRows(t, env, other), 2)
}

// A Claude transcript chains parallel tool calls one after another, while an
// inference frame lists both calls before both results. Entries matched by
// tool ID must not make the text that follows them a duplicate.
func TestWriteDelivery_InferenceAfterTranscriptFollowsMatchedToolCalls(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	env.deliver(t, env.chatID, claudeDelivery(
		claudePrompt("u1", "", "p1", "check both"),
		claudeToolUse("u2", "u1", "toolu_a"),
		claudeToolResult("u3", "u2", "toolu_a", "A"),
		claudeToolUse("u4", "u2", "toolu_b"),
		claudeToolResult("u5", "u4", "toolu_b", "B"),
		claudeReply("u6", "u5", "both done"),
	))
	require.Len(t, listTreeRows(t, env, env.chatID), 6)

	call := func(id string) transcript.Block {
		return transcript.Block{Type: "tool_use", ID: id, Name: "Bash", Input: []byte(`{"command":"ls"}`)}
	}
	result := func(id, out string) transcript.Block {
		return transcript.Block{Type: "tool_result", ToolUseID: id, Text: out}
	}
	res := env.deliver(t, env.chatID, inferenceFrame(
		userText("check both"),
		transcript.Message{Role: transcript.RoleAssistant, Blocks: []transcript.Block{call("toolu_a"), call("toolu_b")}},
		transcript.Message{Role: transcript.RoleUser, Blocks: []transcript.Block{result("toolu_a", "A"), result("toolu_b", "B")}},
		assistantText("both done"),
		userText("thanks"),
	))
	require.Len(t, res.Seen, 6)
	require.Len(t, res.Added, 1)

	rows := listTreeRows(t, env, env.chatID)
	require.Len(t, rows, 7)
	require.Equal(t, rowByContent(t, rows, "both done").ID, rowByContent(t, rows, "thanks").ParentMessageID.UUID)
}

func TestWriteDelivery_PartsAndRowFieldsFollowNewEntriesOnly(t *testing.T) {
	t.Parallel()
	env := newDeliveryEnv(t)
	urls, err := env.writer.WriteContentPartAssets(t.Context(), env.ti.projectID, env.chatID, [][]byte{[]byte("attached notes")})
	require.NoError(t, err)

	write := deliveryWrite(inferenceFrame(userText("read the notes"), assistantText("done")))
	write.Rows = []chat.DeliveryRow{
		{Parts: []repo.CreateChatContentPartParams{{Kind: "prompt_attachment", ContentAssetUrl: urls[0], ExternalID: conv.ToPGText("attachment-1")}}},
		{Model: "claude-sonnet-5", FinishReason: "end_turn"},
	}
	res, err := env.writer.WriteDelivery(t.Context(), env.target(env.chatID), write)
	require.NoError(t, err)
	require.Len(t, res.Added, 2)

	listParts := func() []repo.ListChatContentPartsByChatIDRow {
		parts, err := repo.New(env.ti.conn).ListChatContentPartsByChatID(t.Context(), repo.ListChatContentPartsByChatIDParams{
			RiskContentPartIds: []uuid.UUID{}, ChatID: env.chatID, ProjectID: env.ti.projectID, ParentChatMessageIds: []uuid.UUID{res.Added[0].MessageID},
		})
		require.NoError(t, err)
		return parts
	}
	parts := listParts()
	require.Len(t, parts, 1)
	require.Equal(t, res.Added[0].MessageID, parts[0].ParentChatMessageID.UUID)

	rows := listTreeRows(t, env, env.chatID)
	require.Equal(t, "claude-opus-5", rows[0].Model.String, "the template sets the model")
	require.Equal(t, "claude-sonnet-5", rows[1].Model.String, "a row overrides the template model")
	require.Equal(t, "end_turn", rows[1].FinishReason.String)

	// The same frame again: its entries are stored, so its parts are dropped.
	_, err = env.writer.WriteDelivery(t.Context(), env.target(env.chatID), write)
	require.NoError(t, err)
	require.Len(t, listParts(), 1)
}
