package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/anthropicinference"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	"github.com/speakeasy-api/gram/server/internal/transcript"
)

const (
	sourceClaude     = transcript.SourceClaudeTranscript
	sourceInference  = transcript.SourceAnthropicInference
	sourceCompliance = transcript.SourceCompliance
)

// input is one file turned into deliveries, the way its source would send
// them to the chat writer.
type input struct {
	path       string
	source     transcript.Source
	deliveries []delivery
	// chatID and lastMessageID are set for a Compliance page. The importer
	// fetches a chat's next page after the last message it stored, so a later
	// page of the same chat continues from lastMessageID.
	chatID        string
	lastMessageID string
	// notes say what the adapter had to work around in the file.
	notes []string
}

type delivery struct {
	// name labels the delivery in the output, such as "frame 2 (req_…)".
	name string
	// skip, when set, says why the delivery is not merged.
	skip     string
	delivery transcript.Delivery
}

// format is a file format the CLI reads.
type format string

const (
	formatClaude            format = "claude transcript"
	formatInference         format = "inference frames"
	formatComplianceChat    format = "compliance chat messages"
	formatComplianceSession format = "compliance session messages"
)

func readInput(path string) (*input, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a developer CLI reads the file named on its command line.
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	switch detect(data) {
	case formatClaude:
		return claudeInput(path, data)
	case formatInference:
		return inferenceInput(path, data)
	case formatComplianceChat:
		return complianceInput(path, data)
	case formatComplianceSession:
		return complianceSessionInput(path, data)
	default:
		return nil, fmt.Errorf("%s: unknown input: want a Claude Code session transcript (JSONL), Anthropic inference hook frames (JSONL, one frame per line), or a Compliance API chat messages or local session messages page (JSON)", path)
	}
}

// detect names the format of a file, or "" when it is none of them. A
// Compliance page is one JSON object: chat_messages for a chat, data and
// session for a session transcript. Otherwise the first JSONL line that tells
// decides: an inference frame has messages and a request_id; a Claude
// transcript line has a uuid, parentUuid or sessionId. Claude bookkeeping
// lines without those keys are passed over.
func detect(data []byte) format {
	var whole map[string]json.RawMessage
	if json.Unmarshal(data, &whole) == nil {
		if _, ok := whole["chat_messages"]; ok {
			return formatComplianceChat
		}
		_, items := whole["data"]
		_, session := whole["session"]
		if items && session {
			return formatComplianceSession
		}
	}
	for _, line := range lines(data) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) != nil {
			continue
		}
		_, messages := fields["messages"]
		_, requestID := fields["request_id"]
		if messages && requestID {
			return formatInference
		}
		for _, key := range []string{"uuid", "parentUuid", "sessionId"} {
			if _, ok := fields[key]; ok {
				return formatClaude
			}
		}
	}
	return ""
}

// lines yields the non-blank lines of a JSONL file with their 1-based line
// numbers.
func lines(data []byte) iter.Seq2[int, []byte] {
	return func(yield func(int, []byte) bool) {
		n := 0
		for line := range bytes.Lines(data) {
			n++
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			if !yield(n, line) {
				return
			}
		}
	}
}

// claudeLine is the part of a Claude Code transcript line the adapter reads.
// It mirrors claudeTranscriptLine in server/internal/hooks/claude_transcript.go
// and adds the fields that place a line in the tree.
type claudeLine struct {
	Type              string `json:"type"`
	Subtype           string `json:"subtype"`
	UUID              string `json:"uuid"`
	ParentUUID        string `json:"parentUuid"`
	LogicalParentUUID string `json:"logicalParentUuid"`
	Timestamp         string `json:"timestamp"`
	IsSidechain       bool   `json:"isSidechain"`
	IsMeta            bool   `json:"isMeta"`
	IsCompactSummary  bool   `json:"isCompactSummary"`
	PromptID          string `json:"promptId"`
	Message           struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// claudeBlock is one content block of a Claude transcript message, as in
// claudeTranscriptBlock in server/internal/hooks/claude_transcript.go.
type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// claudeInput turns a whole Claude Code transcript into one delivery. A real
// upload is a run of lines from one offset; the CLI sends the file at once.
//
// Every kept line becomes the entries Split makes of its content, each with
// the line's uuid and its block position as a claude_entry ID. The first
// entry of a line names the line's parent. A line with no entry of its own
// (meta, sidechain, attachment, system, thinking-only) becomes a Link, so a
// line that names it as its parent still finds its place. A compaction
// boundary links to the old leaf (logicalParentUuid), and the summary after
// it is a synthetic entry that starts the new tree.
func claudeInput(path string, data []byte) (*input, error) {
	var all []claudeLine
	// at is the position of the first line with each uuid.
	at := map[string]int{}
	for n, raw := range lines(data) {
		var line claudeLine
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, fmt.Errorf("%s:%d: decode transcript line: %w", path, n, err)
		}
		line.UUID = strings.TrimSpace(line.UUID)
		if line.UUID == "" {
			continue // bookkeeping, such as file-history-snapshot
		}
		if _, ok := at[line.UUID]; !ok {
			at[line.UUID] = len(all)
		}
		all = append(all, line)
	}

	order, late := parentsFirst(all, at)
	d := transcript.Delivery{Source: sourceClaude, Links: nil, Entries: nil}
	var missingParents, forwardCompactions, repeated int
	// Claude Code gives the prompt's promptId to every user line of the turn,
	// including local command output and interruption notices. Only the first
	// user text of a turn carries it, or the next would match it by ID.
	prompts := map[string]bool{}
	for _, i := range order {
		line := all[i]
		id := line.UUID
		if at[id] != i {
			// Claude Code writes a line again with its uuid after a partial
			// compaction. A second link would replace the first in Merge's
			// link map, so only entries are kept; the merge copies them into
			// the newer tree.
			repeated++
		}
		self := transcript.NaturalID{Kind: transcript.KindClaudeEntry, Value: id, Block: 0}
		parent := claudeRef(line.ParentUUID)
		if _, ok := at[strings.TrimSpace(line.ParentUUID)]; parent != nil && !ok {
			// The parent is in another file, such as the session this one
			// continues. The line starts the conversation here.
			missingParents++
			parent = nil
		}

		if line.Type == "system" && line.Subtype == "compact_boundary" {
			old := claudeRef(line.LogicalParentUUID)
			if pos, ok := at[strings.TrimSpace(line.LogicalParentUUID)]; old != nil && (!ok || pos >= i) {
				// The old leaf is not an earlier line of this file, so the
				// new tree's continuation cannot be named.
				forwardCompactions++
				old = nil
			}
			if at[id] == i {
				d.Links = append(d.Links, transcript.Link{ID: self, To: old, Compaction: true})
			}
			continue
		}
		var blocks []transcript.Block
		ok := (line.Type == "user" || line.Type == "assistant") && !line.IsSidechain && !line.IsMeta
		if ok {
			blocks, ok = claudeBlocks(line.Message.Content)
		}
		var entries []transcript.Entry
		if ok {
			msg := transcript.Message{Role: transcript.Role(line.Type), Blocks: blocks, MessageID: "", At: parseTime(line.Timestamp)}
			if line.Type == "assistant" {
				msg.MessageID = line.Message.ID
			}
			entries = transcript.Split(msg)
		}
		if len(entries) == 0 {
			if at[id] == i {
				d.Links = append(d.Links, transcript.Link{ID: self, To: parent, Compaction: false})
			}
			continue
		}

		for b := range entries {
			e := &entries[b]
			ids := []transcript.NaturalID{{Kind: transcript.KindClaudeEntry, Value: id, Block: b}}
			switch {
			case line.IsCompactSummary:
				e.Synthetic = true
			case e.Role == transcript.RoleUser && e.Call == nil && e.Result == nil && line.PromptID != "" && !prompts[line.PromptID]:
				prompts[line.PromptID] = true
				ids = append(ids, transcript.NaturalID{Kind: transcript.KindPrompt, Value: line.PromptID, Block: 0})
			}
			e.IDs = append(ids, e.IDs...)
		}
		entries[0].Parent = parent
		d.Entries = append(d.Entries, entries...)
	}

	var notes []string
	if late > 0 {
		notes = append(notes, fmt.Sprintf("%d lines come before the line they follow; they are merged after it", late))
	}
	if missingParents > 0 {
		notes = append(notes, fmt.Sprintf("%d lines name a parent that is not in this file; they start the conversation here", missingParents))
	}
	if forwardCompactions > 0 {
		notes = append(notes, fmt.Sprintf("%d compaction boundaries name an old leaf (logicalParentUuid) that is not an earlier line; their summaries continue from nothing", forwardCompactions))
	}
	if repeated > 0 {
		notes = append(notes, fmt.Sprintf("%d lines repeat the uuid of an earlier line (a partial compaction); the merge copies them into the newer tree", repeated))
	}
	return &input{
		path:          path,
		source:        sourceClaude,
		deliveries:    []delivery{{name: "upload", skip: "", delivery: d}},
		chatID:        "",
		lastMessageID: "",
		notes:         notes,
	}, nil
}

// parentsFirst orders the lines so each comes after its parent, keeping file
// order otherwise. Merge needs a parent before the entries that name it, and
// Claude Code does not always write that way: a reply can be written before
// the prompt and attachments it follows. It also returns how many lines
// moved. A line whose parent never comes (a cycle) keeps its place at the end.
func parentsFirst(all []claudeLine, at map[string]int) (order []int, late int) {
	done := make(map[string]bool, len(all))
	waiting := map[string][]int{}
	var emit func(i int)
	emit = func(i int) {
		order = append(order, i)
		id := all[i].UUID
		if done[id] {
			return
		}
		done[id] = true
		kids := waiting[id]
		delete(waiting, id)
		for _, k := range kids {
			emit(k)
		}
	}
	for i, line := range all {
		parent := strings.TrimSpace(line.ParentUUID)
		if _, inFile := at[parent]; parent == "" || !inFile || done[parent] {
			emit(i)
			continue
		}
		waiting[parent] = append(waiting[parent], i)
		late++
	}
	var stuck []int
	for _, kids := range waiting {
		stuck = append(stuck, kids...)
	}
	slices.Sort(stuck)
	order = append(order, stuck...)
	return order, late
}

// claudeRef names the last block of a transcript line, or nil for no line.
func claudeRef(uuid string) *transcript.NaturalID {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil
	}
	return &transcript.NaturalID{Kind: transcript.KindClaudeEntry, Value: uuid, Block: transcript.LastBlock}
}

// claudeBlocks decodes message content, a plain string or an array of
// blocks. A tool result's content is flattened the way the server's parser
// does it (claudeTranscriptToolResultText).
func claudeBlocks(content json.RawMessage) ([]transcript.Block, bool) {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return []transcript.Block{textBlock(text)}, true
	}
	var raw []claudeBlock
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, false
	}
	blocks := make([]transcript.Block, 0, len(raw))
	for _, b := range raw {
		block := transcript.Block{Type: b.Type, Text: b.Text, ID: b.ID, Name: b.Name, Input: b.Input, ToolUseID: b.ToolUseID}
		if b.Type == "tool_result" {
			block.Text = claudeToolResultText(b.Content)
		}
		blocks = append(blocks, block)
	}
	return blocks, true
}

func claudeToolResultText(content json.RawMessage) string {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	var items []claudeBlock
	if err := json.Unmarshal(content, &items); err != nil {
		return ""
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type == "text" && item.Text != "" {
			texts = append(texts, item.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// inferenceBlock mirrors contentBlock in
// server/internal/anthropicinference/protocol.go: a tool_result's content
// is one string, and a tool_use names its tool in tool_name (name is the
// Messages API spelling, accepted as a fallback).
type inferenceBlock struct {
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Content   string          `json:"content"`
	Name      string          `json:"name"`
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
}

// inferenceInput turns each line, one frame in the wire format
// anthropicinference decodes, into one delivery: the whole trajectory from
// the root, with no IDs on text. A tool_call frame carries only the latest
// assistant message and the server does not archive it, so it is skipped.
func inferenceInput(path string, data []byte) (*input, error) {
	in := &input{path: path, source: sourceInference, deliveries: nil, chatID: "", lastMessageID: "", notes: nil}
	for n, raw := range lines(data) {
		var frame anthropicinference.Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, fmt.Errorf("%s:%d: decode inference frame: %w", path, n, err)
		}
		d := delivery{
			name:     fmt.Sprintf("frame %d (%s)", len(in.deliveries)+1, frame.RequestID),
			skip:     "",
			delivery: transcript.Delivery{Source: sourceInference, Links: nil, Entries: nil},
		}
		if frame.Type != "prompt" {
			d.skip = frame.Type + " frame, not archived"
			in.deliveries = append(in.deliveries, d)
			continue
		}
		for _, m := range frame.Messages {
			if m.Role != "user" && m.Role != "assistant" {
				continue
			}
			blocks, err := inferenceBlocks(m.Content)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n, err)
			}
			msg := transcript.Message{Role: transcript.Role(m.Role), Blocks: blocks, MessageID: "", At: time.Time{}}
			d.delivery.Entries = append(d.delivery.Entries, transcript.Split(msg)...)
		}
		in.deliveries = append(in.deliveries, d)
	}
	return in, nil
}

// inferenceBlocks decodes the blocks the server reads (knownBlocks in
// protocol.go) and passes others over.
func inferenceBlocks(content json.RawMessage) ([]transcript.Block, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(content, &raws); err != nil {
		return nil, fmt.Errorf("decode inference content: %w", err)
	}
	blocks := make([]transcript.Block, 0, len(raws))
	for _, raw := range raws {
		var b inferenceBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("decode inference content block: %w", err)
		}
		name := b.ToolName
		if name == "" {
			name = b.Name
		}
		block := transcript.Block{Type: b.Type, Text: b.Text, ID: b.ID, Name: name, Input: b.Input, ToolUseID: b.ToolUseID}
		switch b.Type {
		case "text", "attachment", "tool_use":
		case "tool_result":
			block.Text = b.Content
		default:
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// complianceBlock mirrors complianceContentBlock in
// server/internal/aiintegrations/compliance_import.go.
type complianceBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// complianceInput turns one chat messages page into one delivery, in order
// from the start of the page. Each entry carries the Compliance message ID
// and its block position.
func complianceInput(path string, data []byte) (*input, error) {
	var page anthropicapi.ChatMessagesPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("%s: decode compliance page: %w", path, err)
	}
	d := transcript.Delivery{Source: sourceCompliance, Links: nil, Entries: nil}
	last := ""
	for _, msg := range page.Messages {
		// The importer stores only user and assistant messages with an id.
		if msg.ID == "" || (msg.Role != "user" && msg.Role != "assistant") {
			continue
		}
		blocks, err := complianceBlocks(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("%s: message %s: %w", path, msg.ID, err)
		}
		at, err := msg.CreatedAtTime()
		if err != nil {
			return nil, fmt.Errorf("%s: message %s: %w", path, msg.ID, err)
		}
		entries := transcript.Split(transcript.Message{Role: transcript.Role(msg.Role), Blocks: blocks, MessageID: "", At: at})
		for i := range entries {
			id := transcript.NaturalID{Kind: transcript.KindComplianceMessage, Value: msg.ID, Block: i}
			entries[i].IDs = append([]transcript.NaturalID{id}, entries[i].IDs...)
		}
		if len(entries) > 0 {
			last = msg.ID
		}
		d.Entries = append(d.Entries, entries...)
	}
	return &input{
		path:          path,
		source:        sourceCompliance,
		deliveries:    []delivery{{name: "page", skip: "", delivery: d}},
		chatID:        page.ID,
		lastMessageID: last,
		notes:         nil,
	}, nil
}

// complianceSessionPage is one page of a Compliance API local session
// transcript (GET /v1/compliance/apps/sessions/local/{id}/messages), the
// endpoint that covers Cowork in Claude Desktop. Speakeasy does not import it
// yet, so it has no type in server/internal/thirdparty/anthropic; this one
// follows the API reference.
type complianceSessionPage struct {
	Data []struct {
		ID         string          `json:"id"`
		Role       string          `json:"role"`
		CreatedAt  string          `json:"created_at"`
		Content    json.RawMessage `json:"content"`
		Provenance *struct {
			Type string `json:"type"`
		} `json:"provenance"`
	} `json:"data"`
	Session struct {
		ID             string `json:"id"`
		ProductSurface string `json:"product_surface"`
	} `json:"session"`
}

// complianceSessionInput turns one session transcript page into one
// delivery, like a chat messages page. A synthetic marker is text the
// endpoint made up, not something either party sent, so it is skipped.
func complianceSessionInput(path string, data []byte) (*input, error) {
	var page complianceSessionPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("%s: decode compliance session page: %w", path, err)
	}
	d := transcript.Delivery{Source: sourceCompliance, Links: nil, Entries: nil}
	last := ""
	markers := 0
	for _, msg := range page.Data {
		if msg.ID == "" || (msg.Role != "user" && msg.Role != "assistant") {
			continue
		}
		if msg.Provenance != nil && msg.Provenance.Type == "synthetic_marker" {
			markers++
			continue
		}
		blocks, err := complianceBlocks(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("%s: message %s: %w", path, msg.ID, err)
		}
		entries := transcript.Split(transcript.Message{Role: transcript.Role(msg.Role), Blocks: blocks, MessageID: "", At: parseTime(msg.CreatedAt)})
		for i := range entries {
			id := transcript.NaturalID{Kind: transcript.KindComplianceMessage, Value: msg.ID, Block: i}
			entries[i].IDs = append([]transcript.NaturalID{id}, entries[i].IDs...)
		}
		if len(entries) > 0 {
			last = msg.ID
		}
		d.Entries = append(d.Entries, entries...)
	}
	var notes []string
	if markers > 0 {
		notes = append(notes, fmt.Sprintf("%d synthetic marker messages skipped", markers))
	}
	return &input{
		path:          path,
		source:        sourceCompliance,
		deliveries:    []delivery{{name: "page (" + page.Session.ProductSurface + ")", skip: "", delivery: d}},
		chatID:        page.Session.ID,
		lastMessageID: last,
		notes:         notes,
	}, nil
}

// complianceBlocks decodes a message's content. A tool_use input arrives as
// a JSON-encoded string; it is unwrapped so the call's content key matches
// the same call from another source.
func complianceBlocks(content json.RawMessage) ([]transcript.Block, error) {
	if len(content) == 0 || string(content) == "null" {
		return nil, nil
	}
	var raw []complianceBlock
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("decode compliance content: %w", err)
	}
	blocks := make([]transcript.Block, 0, len(raw))
	for _, b := range raw {
		block := transcript.Block{Type: b.Type, Text: b.Text, ID: b.ID, Name: b.Name, Input: b.Input, ToolUseID: b.ToolUseID}
		switch b.Type {
		case "tool_use":
			var encoded string
			if json.Unmarshal(b.Input, &encoded) == nil {
				block.Input = []byte(encoded)
			}
		case "tool_result":
			texts := make([]string, 0, len(b.Content))
			for _, item := range b.Content {
				if item.Type == "text" && item.Text != "" {
					texts = append(texts, item.Text)
				}
			}
			block.Text = strings.Join(texts, "\n")
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func textBlock(text string) transcript.Block {
	return transcript.Block{Type: "text", Text: text, ID: "", Name: "", Input: nil, ToolUseID: ""}
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
