// Package provenance reads optional standard lifecycle and native publication evidence.
// Catalog membership is established by the caller's actual catalog match.
package provenance

import (
	"encoding/json"
	"time"
)

type Provenance struct {
	Status      string
	PublishedAt time.Time
	UpdatedAt   time.Time
}

type metaDocument struct {
	Version struct {
		Status      string `json:"status"`
		PublishedAt string `json:"publishedAt"`
		UpdatedAt   string `json:"updatedAt"`
	} `json:"io.modelcontextprotocol.registry/official"`
	Publication struct {
		PublishedAt string `json:"publishedAt"`
	} `json:"com.speakeasy.ai/registry"`
}

// Read parses metadata without inferring membership or vendor identity from namespaces.
func Read(meta any) Provenance {
	var zero Provenance
	encoded, err := json.Marshal(meta)
	if err != nil {
		return zero
	}
	var doc metaDocument
	if json.Unmarshal(encoded, &doc) != nil {
		return zero
	}
	published := parseTime(doc.Version.PublishedAt)
	if published.IsZero() {
		published = parseTime(doc.Publication.PublishedAt)
	}
	return Provenance{
		Status:      doc.Version.Status,
		PublishedAt: published,
		UpdatedAt:   parseTime(doc.Version.UpdatedAt),
	}
}

func parseTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}

	return parsed
}
