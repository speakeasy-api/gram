package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// sanitizeContentJSON enforces the no-image-bytes-at-rest rule on the
// structured content JSON persisted to content_raw and asset storage: any
// image_url part whose URL is a data: URI is replaced with a text part
// naming the mime type and approximate decoded size. Remote-URL image parts
// (refs) and every other shape pass through byte-for-byte, so text-only
// content keeps its exact hash and asset path.
func sanitizeContentJSON(jsonData []byte) []byte {
	trimmed := bytes.TrimSpace(jsonData)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return jsonData
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(jsonData, &elements); err != nil {
		return jsonData
	}

	changed := false
	for i, element := range elements {
		var probe struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(element, &probe); err != nil {
			continue
		}
		if probe.Type != "image_url" || !strings.HasPrefix(probe.ImageURL.URL, "data:") {
			continue
		}
		replacement, err := json.Marshal(map[string]string{
			"type": "text",
			"text": dataURIPlaceholder(probe.ImageURL.URL),
		})
		if err != nil {
			continue
		}
		elements[i] = replacement
		changed = true
	}
	if !changed {
		return jsonData
	}

	out, err := json.Marshal(elements)
	if err != nil {
		return jsonData
	}
	return out
}

// dataURIPlaceholder summarizes a data: URI as "[image omitted: <mime>,
// ~<n> bytes]" without retaining any of the payload.
func dataURIPlaceholder(uri string) string {
	meta, data, found := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	if !found {
		data = ""
	}
	size := len(data)
	if strings.HasSuffix(meta, ";base64") {
		meta = strings.TrimSuffix(meta, ";base64")
		size = size * 3 / 4
	}
	mime, _, _ := strings.Cut(meta, ";")
	if mime == "" {
		mime = "image"
	}
	return fmt.Sprintf("[image omitted: %s, ~%d bytes]", mime, size)
}
