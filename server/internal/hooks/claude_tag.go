package hooks

import "github.com/speakeasy-api/gram/server/internal/claudetag"

// claudeTagTitle uses the same delivery evidence as persisted attribution.
func claudeTagTitle(prompt string) (string, bool) {
	metadata := claudetag.Parse(prompt)
	return metadata.Title, metadata.Detected && metadata.Title != ""
}
