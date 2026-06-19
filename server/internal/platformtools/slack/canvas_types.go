package slack

type canvasDocumentContent struct {
	Type     string `json:"type" jsonschema:"Document content type. Slack currently accepts \"markdown\"."`
	Markdown string `json:"markdown" jsonschema:"Canvas body authored in Slack-flavoured markdown."`
}
