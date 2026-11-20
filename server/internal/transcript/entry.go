// Package transcript stores an agent session as a tree of entries.
//
// Every capture source turns its own payload into a Delivery. Merge places
// each entry in the tree exactly once, whatever the source and whatever order
// the deliveries arrive in. A rewind becomes a branch; a compaction starts a
// new tree that links back to where the old one ended. A trajectory, the path
// the model actually saw, is read with Path.
//
// The package holds no database code. The chat writer loads the part of the
// tree a delivery needs, calls Merge, and stores the Result.
package transcript

import "time"

// Kind names what a natural ID identifies.
type Kind string

const (
	// KindToolCall is the model-minted id of a tool_use block (toolu_…).
	KindToolCall Kind = "tool_call"
	// KindToolResult is the id of the tool call a tool_result answers. A call
	// and its result share the id but stay two entries.
	KindToolResult Kind = "tool_result"
	// KindPrompt is the client's id for a user turn, such as Claude Code's
	// promptId.
	KindPrompt Kind = "prompt"
	// KindClaudeEntry is the uuid of a Claude Code transcript line. One line
	// can hold several blocks, so the id is paired with a block index.
	KindClaudeEntry Kind = "claude_entry"
	// KindComplianceMessage is the Anthropic Compliance API message id.
	KindComplianceMessage Kind = "compliance_message"
)

// LastBlock as NaturalID.Block refers to the last block of the message the
// id names. A Claude transcript line names its parent line, not a block.
const LastBlock = -1

// NaturalID is an id that a model or a client made. Speakeasy never makes
// one: an id a capture source invents must not be passed as a NaturalID.
type NaturalID struct {
	Kind  Kind
	Value string
	// Block is the block position inside the message the id names. It is 0
	// for ids that name a single block.
	Block int
}

// Role is the speaker of an entry.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Source names the capture source of a delivery.
type Source string

const (
	SourceClaudeTranscript   Source = "claude_transcript"
	SourceAnthropicInference Source = "anthropic_inference"
	SourceCompliance         Source = "compliance"
)

// ToolCall is the tool request an assistant entry carries.
type ToolCall struct {
	ID    string
	Name  string
	Input []byte
}

// ToolResult is the output a tool entry carries.
type ToolResult struct {
	CallID string
	Text   string
}

// Entry is one block of a conversation: one prompt, one reply text, one tool
// call or one tool result. One entry becomes one chat_messages row.
type Entry struct {
	IDs []NaturalID
	// Parent names the entry this one follows. Nil means the entry before it
	// in the delivery, or the start of the conversation for the first entry.
	Parent *NaturalID
	// Continues is set on the first entry after a compaction: the old leaf
	// the new tree continues from. The entry becomes a tree root.
	Continues *NaturalID
	// Synthetic marks generated history, such as a compaction summary. It is
	// stored but not scanned or metered again.
	Synthetic bool
	Role      Role
	Text      string
	Call      *ToolCall
	Result    *ToolResult
	// MessageID is the model message (msg_…) the block came from, when known.
	MessageID string
	At        time.Time
}

// Link stands for a source line that has no entry of its own, such as a
// transcript line the shipper drops. Entries can name it as their parent.
type Link struct {
	ID NaturalID
	// To is the nearest kept ancestor. Nil means the line starts the
	// conversation.
	To *NaturalID
	// Compaction marks a compaction boundary. To is then the old leaf, and an
	// entry that names this link as its parent starts a new tree.
	Compaction bool
}

// Delivery is what an adapter gives to Merge. It translates a payload and
// never decides what is new.
type Delivery struct {
	Source  Source
	Links   []Link
	Entries []Entry
}

var zeroTime time.Time

// isTurn reports whether e is a user prompt: the only kind of entry whose
// siblings are real alternatives (a rewind).
func isTurn(e Entry) bool {
	return e.Role == RoleUser && e.Call == nil && e.Result == nil && !e.Synthetic
}
