package chatevents

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// maxContentBytes caps what is read from a spilled content asset.
const maxContentBytes = 16 * constants.MiB

// rawText is the text of stored content blocks: a JSON string, or the text
// blocks of a content array joined by blank lines. Anything else has no text
// and the caller falls back to the text projection.
func rawText(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}
