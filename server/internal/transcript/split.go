package transcript

import "time"

// Block is one content block of a model message, in the shape every Claude
// source shares.
type Block struct {
	Type      string
	Text      string
	ID        string
	Name      string
	Input     []byte
	ToolUseID string
}

// Message is one model message, before it is split into entries.
type Message struct {
	Role      Role
	Blocks    []Block
	MessageID string
	At        time.Time
}

// Split is the only split rule: one entry for each text, tool_use and
// tool_result block. Other blocks (thinking, attachments) are not entries.
// Every adapter calls it, so the same message splits the same way whatever
// the source, which lets content keys match across sources.
func Split(msg Message) []Entry {
	var out []Entry
	for _, b := range msg.Blocks {
		switch b.Type {
		case "text":
			out = append(out, newEntry(msg, msg.Role, b.Text, nil, nil, nil))
		case "tool_use":
			var ids []NaturalID
			if b.ID != "" {
				ids = []NaturalID{{Kind: KindToolCall, Value: b.ID, Block: 0}}
			}
			call := &ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}
			out = append(out, newEntry(msg, RoleAssistant, "", call, nil, ids))
		case "tool_result":
			var ids []NaturalID
			if b.ToolUseID != "" {
				ids = []NaturalID{{Kind: KindToolResult, Value: b.ToolUseID, Block: 0}}
			}
			result := &ToolResult{CallID: b.ToolUseID, Text: b.Text}
			out = append(out, newEntry(msg, RoleTool, b.Text, nil, result, ids))
		default:
		}
	}
	return out
}

func newEntry(msg Message, role Role, text string, call *ToolCall, result *ToolResult, ids []NaturalID) Entry {
	return Entry{
		IDs:       ids,
		Parent:    nil,
		Continues: nil,
		Synthetic: false,
		Role:      role,
		Text:      text,
		Call:      call,
		Result:    result,
		MessageID: msg.MessageID,
		At:        msg.At,
	}
}
