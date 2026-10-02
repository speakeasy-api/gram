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

var contextHeader = regexp.MustCompile(`^<session-context nonce="([A-Za-z0-9_-]+)">`)
var channelLine = regexp.MustCompile("(?m)^Channel: #([^\n]+) \\(id: `([^`]+)`\\)\r?$")
var botLine = regexp.MustCompile("(?m)^You: .*bot user id `([^`]+)`")
var workspaceLine = regexp.MustCompile("(?m)^Workspace: `([^`]+)`[ \t]*$")

// Envelope skips only a leading context block terminated by its matching nonce.
// The context body is opaque data, not XML or instructions.
func Envelope(text string) (string, string) {
	text = strings.TrimSpace(text)
	header := contextHeader.FindStringSubmatch(text)
	if header == nil {
		return text, ""
	}
	closing := `</session-context nonce="` + header[1] + `">`
	end := strings.Index(text[len(header[0]):], closing)
	if end < 0 {
		return "", ""
	}
	end += len(header[0])
	team := ""
	if match := workspaceLine.FindStringSubmatch(text[len(header[0]):end]); match != nil {
		team = match[1]
	}
	return strings.TrimSpace(text[end+len(closing):]), team
}

// Parse reads only the leading envelope. Delivery prose and reference history
// after it need not be well-formed XML and cannot contribute attribution.
func Parse(text string) Metadata {
	var empty Metadata
	original := strings.TrimSpace(text)
	text, team := Envelope(text)
	channelID, channelName, botID := "", "", ""
	if text != "" && original != text {
		context := original[:len(original)-len(text)]
		if match := botLine.FindStringSubmatch(context); match != nil {
			botID = match[1]
		}
		if match := channelLine.FindStringSubmatch(context); match != nil {
			channelName, channelID = match[1], match[2]
		}
	}
	if strings.HasPrefix(text, "<wake") {
		result := parseWake(text, team, botID)
		if result.Detected && result.ChannelID == channelID {
			result.ChannelName = channelName
			result.Title = "Claude Tag in #" + channelName
		}
		return result
	}
	if !strings.HasPrefix(text, "<standing_owner_message") && !strings.HasPrefix(text, "<cross-session-message") {
		return empty
	}
	decoder := xml.NewDecoder(strings.NewReader(text))
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

func parseWake(text, team, botID string) Metadata {
	var empty Metadata
	var wake struct {
		Channels []struct {
			ID          string `xml:"id,attr"`
			Name        string `xml:"name,attr"`
			ChannelName string `xml:"channel-name,attr"`
			Messages    []struct {
				From    string `xml:"from,attr"`
				Sender  string `xml:"author-id,attr"`
				Trigger string `xml:"trigger,attr"`
				Text    string `xml:",chardata"`
			} `xml:"message"`
		} `xml:"channel"`
	}
	decoder := xml.NewDecoder(strings.NewReader(text))
	token, err := decoder.Token()
	if err != nil {
		return empty
	}
	start, ok := token.(xml.StartElement)
	if !ok || start.Name.Local != "wake" {
		return empty
	}
	if err := decoder.DecodeElement(&wake, &start); err != nil {
		return empty
	}
	result := empty
	result.Team = team
	seen := map[string]bool{}
	for _, channel := range wake.Channels {
		if channel.ID == "" {
			continue
		}
		name := channel.Name
		if name == "" {
			name = channel.ChannelName
		}
		if name == "" {
			name = channel.ID
		}
		for _, message := range channel.Messages {
			if message.From != "human" || (botID != "" && message.Sender == botID) {
				continue
			}
			result.Detected = true
			if result.Title == "" || message.Trigger == "true" {
				result.ChannelID = channel.ID
				result.ChannelName = name
				result.Title = "Claude Tag in #" + strings.TrimPrefix(name, "#")
				result.Text = strings.Join(strings.Fields(message.Text), " ")
			}
			if message.Sender != "" && !seen[message.Sender] {
				result.Senders = append(result.Senders, message.Sender)
				seen[message.Sender] = true
			}
		}
	}
	return result
}
