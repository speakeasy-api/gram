package slack

// Block Kit subset exposed to the assistant via the platform_slack_send_message
// tool. Models the section / actions / context / divider blocks plus the
// button + text-object children — the only elements outbound assistant
// messages currently use. See https://docs.slack.dev/reference/block-kit/blocks.
//
// We intentionally publish a single permissive struct per node rather than a
// discriminated union: the LLM produces JSON directly and ignores enum
// constraints often enough that a strict union just makes the schema noisier
// without adding safety. Slack itself rejects malformed blocks with a typed
// error that surfaces back through the tool.
type slackBlock struct {
	Type     string              `json:"type" jsonschema:"Block kind. Supported: section, actions, context, divider."`
	BlockID  string              `json:"block_id,omitempty" jsonschema:"Stable id for the block. Required for actions blocks so button click callbacks reference it."`
	Text     *slackTextObject    `json:"text,omitempty" jsonschema:"Section block text. Required when type=section."`
	Elements []slackBlockElement `json:"elements,omitempty" jsonschema:"Child elements. Buttons for actions blocks, plain_text/mrkdwn objects for context blocks."`
}

type slackTextObject struct {
	Type string `json:"type" jsonschema:"plain_text or mrkdwn."`
	Text string `json:"text" jsonschema:"Visible text."`
}

type slackBlockElement struct {
	Type     string           `json:"type" jsonschema:"Element kind. Supported: button, plain_text, mrkdwn."`
	ActionID string           `json:"action_id,omitempty" jsonschema:"Required for button. Stable identifier the assistant uses to recognise the click in the resulting block_actions trigger event."`
	Text     *slackTextObject `json:"text,omitempty" jsonschema:"Button label, or text content for plain_text/mrkdwn elements inside a context block."`
	Value    string           `json:"value,omitempty" jsonschema:"Optional payload for button. Returned as action_value on the block_actions trigger event when the user clicks."`
	URL      string           `json:"url,omitempty" jsonschema:"Optional URL the button opens when clicked."`
	Style    string           `json:"style,omitempty" jsonschema:"Optional button style: primary or danger."`
}
