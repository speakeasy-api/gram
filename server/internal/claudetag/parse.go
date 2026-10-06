// Package claudetag extracts provenance from Claude Tag delivery envelopes.
package claudetag

import (
	"encoding/xml"
	"regexp"
	"strings"
)

// Metadata contains delivery evidence, never instructions from message bodies.
type Metadata struct {
	Detected     bool
	Sender       string
	Senders      []string
	Title        string
	ChannelID    string
	ChannelName  string
	Team         string
	ChildSession string
	Text         string
}

var framingHeader = regexp.MustCompile(`^<(system-reminder|session-context)(?:\s[^>]*|)>`)
var contextClosing = regexp.MustCompile(`</session-context(?:\s[^>]*|)>`)
var slackMarkup = regexp.MustCompile(`<(?:[@#][^<>\s]+|https?://[^<>\s]+)>`)
var cdataSection = regexp.MustCompile(`(?s)<!\[CDATA\[.*?\]\]>`)
var xmlEntity = regexp.MustCompile(`^&(?:amp|lt|gt|quot|apos|#[0-9]+|#x[0-9A-Fa-f]+);`)
var channelLine = regexp.MustCompile("(?m)^Channel: #([^\n]+) \\(id: `([^`]+)`\\)\r?$")
var botLine = regexp.MustCompile("(?m)^You: .*bot user id `([^`]+)`")
var workspaceLine = regexp.MustCompile("(?m)^Workspace: `([^`]+)`[ \t]*$")

// Envelope skips leading harness framing, treating reminder and context bodies
// as opaque text. A nonce-bearing context ends only at its matching nonce.
func Envelope(text string) (string, string) {
	body, context := deliveryEnvelope(text)
	team := ""
	if match := workspaceLine.FindStringSubmatch(context); match != nil {
		team = match[1]
	}
	return body, team
}

func deliveryEnvelope(text string) (string, string) {
	text = strings.TrimSpace(text)
	var context strings.Builder
	for {
		header := framingHeader.FindStringSubmatch(text)
		if header == nil {
			return text, context.String()
		}
		nonce, valid := elementAttribute(header[0], "nonce")
		if !valid {
			return "", ""
		}
		name := header[1]
		closingStart, closingEnd := -1, -1
		if name == "session-context" {
			for _, loc := range contextClosing.FindAllStringIndex(text[len(header[0]):], -1) {
				start, end := loc[0]+len(header[0]), loc[1]+len(header[0])
				closing := strings.Replace(text[start:end], "</", "<", 1)
				closingNonce, valid := elementAttribute(closing, "nonce")
				if valid && closingNonce == nonce {
					closingStart, closingEnd = start, end
					break
				}
			}
		} else {
			closing := "</" + name + ">"
			if end := strings.Index(text[len(header[0]):], closing); end >= 0 {
				closingStart = end + len(header[0])
				closingEnd = closingStart + len(closing)
			}
		}
		if closingStart < 0 {
			return "", ""
		}
		if name == "session-context" {
			context.WriteString(text[len(header[0]):closingStart])
			context.WriteByte('\n')
		}
		text = strings.TrimSpace(text[closingEnd:])
	}
}

func elementAttribute(header, name string) (string, bool) {
	token, err := xml.NewDecoder(strings.NewReader(header)).Token()
	if err != nil {
		return "", false
	}
	start, ok := token.(xml.StartElement)
	if !ok {
		return "", false
	}
	for _, attr := range start.Attr {
		if attr.Name.Local == name {
			return strings.TrimSpace(attr.Value), true
		}
	}
	return "", true
}

// Delivery bodies sometimes contain raw Slack mentions, links and ampersands.
// Escape those text forms without repairing broken envelope structure.
func deliveryXML(text string) string {
	var result strings.Builder
	start := 0
	for _, loc := range cdataSection.FindAllStringIndex(text, -1) {
		result.WriteString(escapeDeliveryText(text[start:loc[0]]))
		result.WriteString(text[loc[0]:loc[1]])
		start = loc[1]
	}
	result.WriteString(escapeDeliveryText(text[start:]))
	return result.String()
}

func escapeDeliveryText(text string) string {
	text = slackMarkup.ReplaceAllStringFunc(text, func(value string) string {
		return "&lt;" + value[1:len(value)-1] + "&gt;"
	})
	var result strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '<' && (i+1 == len(text) || !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_:/!?", rune(text[i+1]))) {
			// Tag-like markup must remain intact so malformed envelopes fail parsing.
			result.WriteString("&lt;")
		} else if text[i] == '&' && !xmlEntity.MatchString(text[i:]) {
			result.WriteString("&amp;")
		} else {
			result.WriteByte(text[i])
		}
	}
	return result.String()
}

// Parse reads only the leading envelope. Delivery prose and reference history
// after it need not be well-formed XML and cannot contribute attribution.
func Parse(text string) Metadata {
	var empty Metadata
	text, context := deliveryEnvelope(text)
	team := ""
	if match := workspaceLine.FindStringSubmatch(context); match != nil {
		team = match[1]
	}
	channelID, channelName, botID := "", "", ""
	if context != "" {
		if match := botLine.FindStringSubmatch(context); match != nil {
			botID = strings.TrimSpace(match[1])
		}
		if match := channelLine.FindStringSubmatch(context); match != nil {
			channelName, channelID = match[1], match[2]
		}
	}
	if strings.HasPrefix(text, "<wake") {
		result := parseWake(text, team, botID)
		if result.Detected && channelID != "" && result.ChannelID == channelID {
			result.ChannelName = channelName
			result.Title = "Claude Tag in #" + channelName
		}
		return result
	}
	if !strings.HasPrefix(text, "<standing_owner_message") && !strings.HasPrefix(text, "<cross-session-message") {
		return empty
	}
	decoder := xml.NewDecoder(strings.NewReader(deliveryXML(text)))
	token, err := decoder.Token()
	if err != nil {
		return empty
	}
	start, ok := token.(xml.StartElement)
	if !ok {
		return empty
	}
	attrs := map[string]string{}
	for _, a := range start.Attr {
		attrs[a.Name.Local] = strings.TrimSpace(a.Value)
	}
	var body struct {
		Text string `xml:",chardata"`
	}
	if err := decoder.DecodeElement(&body, &start); err != nil {
		return empty
	}
	switch start.Name.Local {
	case "standing_owner_message":
		if attrs["sender"] == "" {
			return empty
		}
		if attrs["team-id"] != "" {
			team = attrs["team-id"]
		}
		if channelID == "" {
			channelID = attrs["channel-id"]
		}
		return Metadata{Detected: true, Sender: attrs["sender"], Senders: []string{attrs["sender"]}, Title: "", Team: team, ChannelID: channelID, ChannelName: channelName, ChildSession: "", Text: strings.Join(strings.Fields(body.Text), " ")}
	case "cross-session-message":
		if attrs["standing-audience"] != "parent" || attrs["from-session"] == "" {
			return empty
		}
		return Metadata{Detected: true, Sender: "", Senders: nil, Title: "", Team: team, ChannelID: channelID, ChannelName: channelName, ChildSession: attrs["from-session"], Text: ""}
	default:
		return empty
	}
}

// deliveryNode preserves wrapper ordering so new grouping elements do not
// silently discard human deliveries. Reference and harness subtrees are skipped.
type deliveryNode struct {
	XMLName  xml.Name       `xml:""`
	Attrs    []xml.Attr     `xml:",any,attr"`
	Text     string         `xml:",chardata"`
	Children []deliveryNode `xml:",any"`
}

func (n deliveryNode) attr(names ...string) string {
	for _, name := range names {
		for _, attr := range n.Attrs {
			if attr.Name.Local == name && strings.TrimSpace(attr.Value) != "" {
				return strings.TrimSpace(attr.Value)
			}
		}
	}
	return ""
}

func (n deliveryNode) messages() []deliveryNode {
	var messages []deliveryNode
	for _, child := range n.Children {
		switch child.XMLName.Local {
		case "message":
			messages = append(messages, child)
		case "participants", "system-note", "system-reminder", "session-context", "history", "reference", "thread_activity", "channel":
			continue
		default:
			messages = append(messages, child.messages()...)
		}
	}
	return messages
}

func parseWake(text, team, botID string) Metadata {
	var empty Metadata
	var wake deliveryNode
	decoder := xml.NewDecoder(strings.NewReader(deliveryXML(text)))
	if err := decoder.Decode(&wake); err != nil || wake.XMLName.Local != "wake" {
		return empty
	}
	result := empty
	result.Team = team
	seen := map[string]bool{}
	for _, channel := range wake.Children {
		channelID := channel.attr("id", "channel-id")
		if channel.XMLName.Local != "channel" || channelID == "" {
			continue
		}
		name := channel.attr("name", "channel-name")
		if name == "" {
			name = channelID
		}
		for _, message := range channel.messages() {
			sender := message.attr("author-id", "sender", "slack-id")
			if message.attr("from") != "human" || (botID != "" && sender == botID) {
				continue
			}
			result.Detected = true
			messageText := strings.Join(strings.Fields(message.Text), " ")
			if messageText != "" && (result.Title == "" || message.attr("trigger") == "true") {
				result.ChannelID = channelID
				result.ChannelName = strings.TrimPrefix(name, "#")
				result.Title = "Claude Tag in #" + strings.TrimPrefix(name, "#")
				result.Text = messageText
			}
			if sender != "" && !seen[sender] {
				result.Senders = append(result.Senders, sender)
				seen[sender] = true
			}
		}
	}
	return result
}
