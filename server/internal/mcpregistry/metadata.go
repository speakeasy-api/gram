package mcpregistry

import (
	"encoding/json"
	"fmt"
	"time"
)

const publicationNamespace = "com.speakeasy.ai/registry"

// canonicalMetadata keeps the column authoritative while retaining unknown JSON
// as raw values, never rounding publisher numbers through float64. Missing owned
// metadata is accepted on save, but an explicitly supplied value must round-trip.
// Persist this projection with its authoritative column so SQL admission and page
// budgets measure exactly the metadata returned to every registry consumer.
func canonicalMetadata(data json.RawMessage, previous, publishedAt time.Time) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode registry metadata: %w", err)
	}
	var meta map[string]json.RawMessage
	if raw := root["_meta"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decode registry metadata: %w", err)
		}
	}
	if meta == nil {
		meta = make(map[string]json.RawMessage)
	}
	owned := func(at time.Time) json.RawMessage {
		if at.IsZero() {
			return nil
		}
		raw, _ := json.Marshal(struct {
			PublishedAt string `json:"publishedAt"`
		}{at.UTC().Format(time.RFC3339Nano)})
		return raw
	}
	if supplied, ok := meta[publicationNamespace]; ok && !equalJSON(supplied, owned(previous)) {
		return nil, &InvalidError{Issues: []Issue{{Path: "/_meta/com.speakeasy.ai~1registry", Message: "publication metadata is backend-owned"}}}
	}
	delete(meta, publicationNamespace)
	if raw := owned(publishedAt); raw != nil {
		meta[publicationNamespace] = raw
	}
	if len(meta) > 0 {
		raw, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("encode registry metadata: %w", err)
		}
		root["_meta"] = raw
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode registry record: %w", err)
	}
	return raw, nil
}
