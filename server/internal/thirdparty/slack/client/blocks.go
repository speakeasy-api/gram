package client

// Block Kit primitives. Only the subset Gram callers currently need:
// section, actions (with button), context, divider. See
// https://docs.slack.dev/reference/block-kit/blocks for the full spec.
//
// Block is a tagged union; each concrete block sets Type via its constructor.
// Callers compose blocks and hand the slice to PostMessage / PostEphemeralMessage
// via SlackPostMessageInput.Blocks.
//
// Interactive components (the Button element below) carry an ActionID and
// optional Value. Slack delivers clicks back as a block_actions interaction
// payload to the configured trigger webhook; the Gram slack trigger definition
// surfaces ActionID/ActionValue/BlockID on the normalized event so handlers
// can filter and the assistant adapter can route the click as a new turn on
// the same thread.
type Block struct {
	Type     string      `json:"type"`
	BlockID  string      `json:"block_id,omitempty"`
	Text     *TextObject `json:"text,omitempty"`
	Elements []any       `json:"elements,omitempty"`
}

type TextObject struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji *bool  `json:"emoji,omitempty"`
}

type ButtonElement struct {
	Type     string      `json:"type"`
	ActionID string      `json:"action_id"`
	Text     *TextObject `json:"text"`
	Value    string      `json:"value,omitempty"`
	URL      string      `json:"url,omitempty"`
	Style    string      `json:"style,omitempty"`
}

func PlainText(text string) *TextObject {
	return &TextObject{Type: "plain_text", Text: text, Emoji: nil}
}

func Markdown(text string) *TextObject {
	return &TextObject{Type: "mrkdwn", Text: text, Emoji: nil}
}

func SectionBlock(text *TextObject) Block {
	return Block{Type: "section", BlockID: "", Text: text, Elements: nil}
}

func DividerBlock() Block {
	return Block{Type: "divider", BlockID: "", Text: nil, Elements: nil}
}

func ContextBlock(elements ...any) Block {
	return Block{Type: "context", BlockID: "", Text: nil, Elements: elements}
}

func ActionsBlock(blockID string, elements ...any) Block {
	return Block{Type: "actions", BlockID: blockID, Text: nil, Elements: elements}
}

func Button(actionID, label, value string) ButtonElement {
	return ButtonElement{
		Type:     "button",
		ActionID: actionID,
		Text:     PlainText(label),
		Value:    value,
		URL:      "",
		Style:    "",
	}
}
