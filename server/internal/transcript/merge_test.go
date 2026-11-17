package transcript

import (
	"encoding/binary"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// sequentialIDs makes predictable node IDs.
func sequentialIDs() func() (uuid.UUID, error) {
	var n uint64
	return func() (uuid.UUID, error) {
		n++
		var id uuid.UUID
		binary.BigEndian.PutUint64(id[8:], n)
		return id, nil
	}
}

func line(id string) NaturalID { return NaturalID{Kind: KindClaudeEntry, Value: id, Block: 0} }

func parentLine(id string) *NaturalID {
	return &NaturalID{Kind: KindClaudeEntry, Value: id, Block: LastBlock}
}

func promptID(id string) NaturalID { return NaturalID{Kind: KindPrompt, Value: id, Block: 0} }

// transcriptPrompt is a Claude transcript user line.
func transcriptPrompt(uuid, parent, prompt, text string) Entry {
	e := Entry{Role: RoleUser, Text: text, IDs: []NaturalID{line(uuid), promptID(prompt)}}
	if parent != "" {
		e.Parent = parentLine(parent)
	}
	return e
}

// transcriptReply is a Claude transcript assistant text line.
func transcriptReply(uuid, parent, text string) Entry {
	return Entry{Role: RoleAssistant, Text: text, IDs: []NaturalID{line(uuid)}, Parent: parentLine(parent)}
}

func transcriptToolUse(uuid, parent, callID string) Entry {
	return Entry{
		Role:   RoleAssistant,
		Call:   &ToolCall{ID: callID, Name: "Bash", Input: []byte(`{"command":"ls"}`)},
		IDs:    []NaturalID{line(uuid), {Kind: KindToolCall, Value: callID}},
		Parent: parentLine(parent),
	}
}

func transcriptToolResult(uuid, parent, callID, out string) Entry {
	return Entry{
		Role:   RoleTool,
		Text:   out,
		Result: &ToolResult{CallID: callID, Text: out},
		IDs:    []NaturalID{line(uuid), {Kind: KindToolResult, Value: callID}},
		Parent: parentLine(parent),
	}
}

// inferenceFrame is a whole trajectory with no IDs on text entries.
func inferenceFrame(msgs ...Message) Delivery {
	d := Delivery{Source: SourceAnthropicInference}
	for _, m := range msgs {
		d.Entries = append(d.Entries, Split(m)...)
	}
	return d
}

func user(text string) Message {
	return Message{Role: RoleUser, Blocks: []Block{{Type: "text", Text: text}}}
}

func assistant(text string) Message {
	return Message{Role: RoleAssistant, Blocks: []Block{{Type: "text", Text: text}}}
}

func transcriptDelivery(entries ...Entry) Delivery {
	return Delivery{Source: SourceClaudeTranscript, Entries: entries}
}

func texts(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Entry.Text)
	}
	return out
}

func mustNode(t *testing.T, tr *Transcript, id NaturalID) *Node {
	t.Helper()
	n, ok := tr.lookup(id)
	require.True(t, ok, "no node for %v", id)
	return n
}

func TestMerge_InferenceFramesGrowWithoutDuplicates(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())

	res, err := tr.Merge(inferenceFrame(user("hi"), assistant("hello")))
	require.NoError(t, err)
	require.Len(t, res.Added, 2)

	res, err = tr.Merge(inferenceFrame(user("hi"), assistant("hello"), user("next"), assistant("done")))
	require.NoError(t, err)
	require.Len(t, res.Added, 2)
	require.Len(t, res.Seen, 2)
	last := res.Added[1]

	res, err = tr.Merge(inferenceFrame(user("hi"), assistant("hello"), user("next"), assistant("done")))
	require.NoError(t, err)
	require.Empty(t, res.Added, "a re-sent frame adds nothing")

	require.Equal(t, []string{"hi", "hello", "next", "done"}, texts(Path(last)))
	require.Len(t, tr.root.Children, 1, "one tree")
}

func TestMerge_RewindThatRepeatsTheSameTextBranches(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())

	res, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "hi"),
		transcriptReply("u2", "u1", "hello"),
		transcriptPrompt("u3", "u2", "p2", "yes"),
		transcriptReply("u4", "u3", "ok"),
		// The user rewinds to u2 and types "yes" again.
		transcriptPrompt("u5", "u2", "p3", "yes"),
		transcriptReply("u6", "u5", "sure"),
	))
	require.NoError(t, err)
	require.Len(t, res.Added, 6)

	hello := mustNode(t, tr, line("u2"))
	require.Len(t, hello.Children, 2, "two separate \"yes\" entries")
	require.Equal(t, []string{"yes", "yes"}, texts(hello.Children))

	sure := mustNode(t, tr, line("u6"))
	require.Equal(t, []string{"hi", "hello", "yes", "sure"}, texts(Path(sure)))
	ok := mustNode(t, tr, line("u4"))
	require.Equal(t, []string{"hi", "hello", "yes", "ok"}, texts(Path(ok)))
}

func TestMerge_TranscriptAdoptsEntriesInferenceStoredFirst(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())

	res, err := tr.Merge(inferenceFrame(user("hi"), assistant("hello"), user("yes"), assistant("ok")))
	require.NoError(t, err)
	require.Len(t, res.Added, 4)

	res, err = tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "hi"),
		transcriptReply("u2", "u1", "hello"),
		transcriptPrompt("u3", "u2", "p2", "yes"),
		transcriptReply("u4", "u3", "ok"),
	))
	require.NoError(t, err)
	require.Empty(t, res.Added, "the transcript joins what inference stored")
	require.Len(t, res.Seen, 4)
	for _, s := range res.Seen {
		require.NotEmpty(t, s.NewIDs, "the stored node gains the transcript's IDs")
	}
	require.Equal(t, mustNode(t, tr, line("u4")), mustNode(t, tr, promptID("p2")).Children[0])
}

func TestMerge_InferenceJoinsTheNewestBranch(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "hi"),
		transcriptReply("u2", "u1", "hello"),
		transcriptPrompt("u3", "u2", "p2", "yes"),
		transcriptReply("u4", "u3", "ok"),
		transcriptPrompt("u5", "u2", "p3", "yes"),
		transcriptReply("u6", "u5", "sure"),
	))
	require.NoError(t, err)

	res, err := tr.Merge(inferenceFrame(user("hi"), assistant("hello"), user("yes"), assistant("sure")))
	require.NoError(t, err)
	require.Empty(t, res.Added)
	require.Equal(t, mustNode(t, tr, line("u5")), res.Seen[2].Node, "\"yes\" joins the newest branch")
}

func TestMerge_ToolCallsMatchAcrossSourcesByID(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "list files"),
		transcriptToolUse("u2", "u1", "toolu_1"),
		transcriptToolResult("u3", "u2", "toolu_1", "a.go"),
		transcriptReply("u4", "u3", "one file"),
	))
	require.NoError(t, err)

	res, err := tr.Merge(inferenceFrame(
		user("list files"),
		Message{Role: RoleAssistant, Blocks: []Block{{Type: "tool_use", ID: "toolu_1", Name: "Bash", Input: []byte(`{"command":"ls"}`)}}},
		Message{Role: RoleUser, Blocks: []Block{{Type: "tool_result", ToolUseID: "toolu_1", Text: "a.go"}}},
		assistant("one file"),
	))
	require.NoError(t, err)
	require.Empty(t, res.Added)
}

func TestMerge_SideEntriesOfOneStepStayOnOneLine(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "check both"),
		transcriptToolUse("u2", "u1", "toolu_a"),
		transcriptToolResult("u3", "u2", "toolu_a", "A"),
		// The next parallel tool call names the same parent as the result.
		transcriptToolUse("u4", "u2", "toolu_b"),
		transcriptToolResult("u5", "u4", "toolu_b", "B"),
	))
	require.NoError(t, err)

	callA := mustNode(t, tr, line("u2"))
	require.Len(t, callA.Children, 1, "no branch for a side entry")
	callB := mustNode(t, tr, line("u4"))
	require.Equal(t, mustNode(t, tr, line("u3")), callB.Parent, "chained after the result before it")
}

func TestMerge_CompactionStartsANewTreeWithTheSummaryAsRoot(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "hi"),
		transcriptReply("u2", "u1", "hello"),
	))
	require.NoError(t, err)

	summary := Entry{Role: RoleUser, Text: "summary of the chat", Synthetic: true, IDs: []NaturalID{line("s1")}, Parent: parentLine("b1")}
	res, err := tr.Merge(Delivery{
		Source:  SourceClaudeTranscript,
		Links:   []Link{{ID: line("b1"), To: parentLine("u2"), Compaction: true}},
		Entries: []Entry{summary, transcriptPrompt("u3", "s1", "p2", "go on"), transcriptReply("u4", "u3", "done")},
	})
	require.NoError(t, err)
	require.Len(t, res.Added, 3)

	root := mustNode(t, tr, line("s1"))
	require.False(t, root.ParentID().Valid, "the summary is a tree root")
	require.Equal(t, mustNode(t, tr, line("u2")).ID, root.ContinuesID().UUID)
	require.True(t, root.Entry.Synthetic)

	done := mustNode(t, tr, line("u4"))
	require.Equal(t, []string{"summary of the chat", "go on", "done"}, texts(Path(done)))
	require.Equal(t, []string{"hi", "hello", "summary of the chat", "go on", "done"}, texts(FullPath(done)))
}

func TestMerge_DroppedLinesLinkThroughAndPersistAsAliases(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(
		transcriptPrompt("u1", "", "p1", "read it"),
		transcriptToolUse("u2", "u1", "toolu_1"),
	))
	require.NoError(t, err)

	// An attachment line (dropped by the shipper) sits between the call and its result.
	res, err := tr.Merge(Delivery{
		Source:  SourceClaudeTranscript,
		Links:   []Link{{ID: line("att"), To: parentLine("u2")}},
		Entries: []Entry{transcriptToolResult("u3", "att", "toolu_1", "contents")},
	})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	require.Equal(t, mustNode(t, tr, line("u2")), res.Added[0].Parent)
	require.Len(t, res.Aliases, 1)
	require.Equal(t, mustNode(t, tr, line("u2")), res.Aliases[0].Node)

	// A later delivery can name the dropped line without resending the link.
	res, err = tr.Merge(transcriptDelivery(transcriptReply("u4", "att", "done")))
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
}

func TestMerge_UnknownParentIsAnError(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(transcriptReply("u2", "missing", "hello")))
	var unknown *UnknownParentError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, "missing", unknown.ID.Value)
}

func TestMerge_SystemRemindersDoNotChangeTheKey(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	_, err := tr.Merge(transcriptDelivery(transcriptPrompt("u1", "", "p1", "fix the bug")))
	require.NoError(t, err)

	res, err := tr.Merge(inferenceFrame(user("<system-reminder>\nToday is Monday.\n</system-reminder>\nfix the bug")))
	require.NoError(t, err)
	require.Empty(t, res.Added)
}

func TestMerge_ComplianceContinuesAfterTheCursor(t *testing.T) {
	t.Parallel()
	tr := New(sequentialIDs())
	msg := func(id string, role Role, text string) Entry {
		return Entry{Role: role, Text: text, IDs: []NaturalID{{Kind: KindComplianceMessage, Value: id}}}
	}
	_, err := tr.Merge(Delivery{Source: SourceCompliance, Entries: []Entry{msg("c1", RoleUser, "q"), msg("c2", RoleAssistant, "a")}})
	require.NoError(t, err)

	next := msg("c3", RoleUser, "follow up")
	next.Parent = &NaturalID{Kind: KindComplianceMessage, Value: "c2"}
	page := Delivery{Source: SourceCompliance, Entries: []Entry{next, msg("c4", RoleAssistant, "answer")}}

	res, err := tr.Merge(page)
	require.NoError(t, err)
	require.Len(t, res.Added, 2)
	require.Equal(t, []string{"q", "a", "follow up", "answer"}, texts(Path(res.Added[1])))

	res, err = tr.Merge(page)
	require.NoError(t, err)
	require.Empty(t, res.Added)
}

func TestMerge_ContinuesFromStoredNodes(t *testing.T) {
	t.Parallel()
	first := New(sequentialIDs())
	res, err := first.Merge(inferenceFrame(user("hi"), assistant("hello")))
	require.NoError(t, err)

	stored := make([]Stored, 0, len(res.Added))
	for _, n := range res.Added {
		stored = append(stored, Stored{
			ID:          n.ID,
			ParentID:    n.ParentID(),
			ContinuesID: n.ContinuesID(),
			Role:        n.Entry.Role,
			ContentKey:  n.ContentKey,
			PathKey:     n.PathKey,
			IDs:         n.Entry.IDs,
		})
	}

	ids := sequentialIDs()
	for range stored {
		_, _ = ids()
	}
	second := New(ids)
	second.Load(stored)

	res, err = second.Merge(inferenceFrame(user("hi"), assistant("hello"), user("more")))
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	require.Equal(t, stored[1].ID, res.Added[0].ParentID().UUID)
	require.Equal(t, PathKey(stored[1].PathKey, res.Added[0].ContentKey), res.Added[0].PathKey)
}

func TestSplit_OneEntryPerBlock(t *testing.T) {
	t.Parallel()
	entries := Split(Message{Role: RoleAssistant, MessageID: "msg_1", Blocks: []Block{
		{Type: "thinking", Text: "hmm"},
		{Type: "text", Text: "Let me look."},
		{Type: "tool_use", ID: "toolu_1", Name: "Read", Input: []byte(`{}`)},
		{Type: "tool_result", ToolUseID: "toolu_0", Text: "earlier output"},
	}})
	require.Len(t, entries, 3, "thinking is not an entry")
	require.Equal(t, RoleAssistant, entries[0].Role)
	require.Equal(t, []NaturalID{{Kind: KindToolCall, Value: "toolu_1"}}, entries[1].IDs)
	require.Equal(t, RoleTool, entries[2].Role)
	require.Equal(t, []NaturalID{{Kind: KindToolResult, Value: "toolu_0"}}, entries[2].IDs)
	for _, e := range entries {
		require.Equal(t, "msg_1", e.MessageID)
	}
}
