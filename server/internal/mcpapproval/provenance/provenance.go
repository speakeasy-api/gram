// Package provenance reads optional standard lifecycle and native publication evidence.
// Catalog membership is established by the caller's actual catalog match.
package provenance

import (
	"encoding/json"
	"time"
)

type Provenance struct {
	Catalogued  bool
	Status      string
	IsLatest    *bool
	PublishedAt time.Time
	UpdatedAt   time.Time
}

type metaDocument struct {
	Version struct {
		Status      string `json:"status"`
		IsLatest    *bool  `json:"isLatest"`
		PublishedAt string `json:"publishedAt"`
		UpdatedAt   string `json:"updatedAt"`
	} `json:"io.modelcontextprotocol.registry/official"`
	Publication struct {
		PublishedAt string `json:"publishedAt"`
	} `json:"com.speakeasy.ai/registry"`
}

// Read parses metadata without inferring membership or vendor identity from namespaces.
func Read(meta any) Provenance {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return Provenance{}
	}
	var doc metaDocument
	if json.Unmarshal(encoded, &doc) != nil {
		return Provenance{}
	}
	published := parseTime(doc.Version.PublishedAt)
	if published.IsZero() {
		published = parseTime(doc.Publication.PublishedAt)
	}
	return Provenance{
		Status:      doc.Version.Status,
		IsLatest:    doc.Version.IsLatest,
		PublishedAt: published,
		UpdatedAt:   parseTime(doc.Version.UpdatedAt),
	}
}

// Withdrawn reports that the registry no longer lists this version as active.
//
// A server withdrawn from the catalogue after someone started using it is
// worth an approver's attention, and it is the one status value that changes a
// decision on its own.
func (p Provenance) Withdrawn() bool {
	return p.Catalogued && p.Status != "" && p.Status != "active"
}

// StaleFor reports how long since the entry last changed, and whether that is
// knowable. Callers render the duration; deciding what counts as stale is a
// judgement for the person approving, not for this package.
func (p Provenance) StaleFor(now time.Time) (time.Duration, bool) {
	if p.UpdatedAt.IsZero() {
		return 0, false
	}

	return now.Sub(p.UpdatedAt), true
}

func parseTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}

	return parsed
}
