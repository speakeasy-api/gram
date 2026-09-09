package hooks

import (
	"encoding/xml"
	"strings"
)

// claudeTagTitle recognizes channel wake envelopes without interpreting their
// contents as markup or changing the captured transcript.
func claudeTagTitle(prompt string) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(prompt), "<wake") {
		return "", false
	}
	var wake struct {
		XMLName  xml.Name `xml:"wake"`
		Channels []struct {
			ID          string `xml:"id,attr"`
			Name        string `xml:"name,attr"`
			ChannelName string `xml:"channel-name,attr"`
			Messages    []struct {
				From    string `xml:"from,attr"`
				Trigger string `xml:"trigger,attr"`
				Text    string `xml:",chardata"`
			} `xml:"message"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(strings.TrimSpace(prompt)), &wake); err != nil {
		return "", false
	}
	first := ""
	for _, channel := range wake.Channels {
		if channel.ID == "" {
			continue
		}
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			name = strings.TrimSpace(channel.ChannelName)
		}
		if name == "" {
			name = channel.ID
		}
		title := "Claude Tag in #" + strings.TrimPrefix(name, "#")
		for _, message := range channel.Messages {
			if message.From != "human" {
				continue
			}
			text := strings.Join(strings.Fields(message.Text), " ")
			if first == "" && text != "" {
				first = title
			}
			if message.Trigger == "true" {
				return title, true
			}
		}
	}
	return first, first != ""
}
