// Package provenance reads the maturity and popularity signals an MCP registry
// publishes alongside a server entry.
//
// These answer two of an approver's questions — is this real, and is it
// maintained — and they are the best-covered signals available: across a live
// catalogue page, 45 of 50 servers carried both the official flag and visitor
// estimates. They are also already fetched and already cached; only the reading
// was missing.
//
// The input is the registry's `_meta` object, which reaches callers as an
// untyped blob. Parsing it here keeps the shape in one place rather than having
// every consumer index into a map.
//
// Two limits worth stating. Everything is the registry's claim, not ours: an
// official flag means the registry believes a publisher is who they say, and a
// visitor estimate is a proxy for popularity, not for safety. And a server the
// registry has never heard of yields nothing at all, which for this workflow is
// the common case and must read as "not catalogued" rather than as a finding.
package provenance

import (
	"encoding/json"
	"time"
)

// Provenance is what a registry publishes about a server's standing.
type Provenance struct {
	// Catalogued reports that the registry had an entry at all. False means
	// every other field is meaningless, and the surface must say the server is
	// not catalogued rather than showing empty maturity.
	Catalogued bool

	// Official is the registry's claim that the entry belongs to the vendor it
	// appears to belong to. It is the registry vouching, not us.
	Official bool

	// Status is the registry's lifecycle state for this version, such as
	// `active` or `deleted`. Empty when unpublished.
	Status string

	// IsLatest reports whether the entry is the newest published version.
	IsLatest bool

	// PublishedAt is when this version was published, zero when unknown.
	PublishedAt time.Time

	// UpdatedAt is when the entry last changed, zero when unknown. Paired with
	// PublishedAt it is the maintenance-recency signal an approver reads.
	UpdatedAt time.Time

	// VisitorsLastWeek, VisitorsLastFourWeeks and VisitorsTotal are the
	// registry's traffic estimates. Rough, and a popularity proxy only — a
	// widely used server and one nobody has touched are different approval
	// decisions, but neither is evidence about behaviour.
	VisitorsLastWeek      int
	VisitorsLastFourWeeks int
	VisitorsTotal         int
}

// metaDocument mirrors the registry's `_meta` object. The keys are namespaced
// per the registry extension convention, which is why they read as domains.
type metaDocument struct {
	Server struct {
		IsOfficial                     bool `json:"isOfficial"`
		VisitorsEstimateMostRecentWeek int  `json:"visitorsEstimateMostRecentWeek"`
		VisitorsEstimateLastFourWeeks  int  `json:"visitorsEstimateLastFourWeeks"`
		VisitorsEstimateTotal          int  `json:"visitorsEstimateTotal"`
	} `json:"com.pulsemcp/server"`

	Version struct {
		Status      string `json:"status"`
		IsLatest    bool   `json:"isLatest"`
		PublishedAt string `json:"publishedAt"`
		UpdatedAt   string `json:"updatedAt"`
	} `json:"com.pulsemcp/server-version"`
}

// Read parses a registry entry's `_meta` blob.
//
// The blob arrives as `any` because it is carried through the cache untyped.
// Anything unparseable yields a zero Provenance with Catalogued false, since a
// meta object we cannot read tells us no more than an absent one.
func Read(meta any) Provenance {
	none := Provenance{
		Catalogued: false, Official: false, Status: "", IsLatest: false,
		PublishedAt: time.Time{}, UpdatedAt: time.Time{},
		VisitorsLastWeek: 0, VisitorsLastFourWeeks: 0, VisitorsTotal: 0,
	}

	if meta == nil {
		return none
	}

	// Round-tripping through JSON is what makes this work against both a
	// freshly-decoded response and a cache hit, which returns plain maps
	// rebuilt by msgpack rather than the original struct.
	encoded, err := json.Marshal(meta)
	if err != nil {
		return none
	}

	var doc metaDocument
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return none
	}

	return Provenance{
		Catalogued:            true,
		Official:              doc.Server.IsOfficial,
		Status:                doc.Version.Status,
		IsLatest:              doc.Version.IsLatest,
		PublishedAt:           parseTime(doc.Version.PublishedAt),
		UpdatedAt:             parseTime(doc.Version.UpdatedAt),
		VisitorsLastWeek:      doc.Server.VisitorsEstimateMostRecentWeek,
		VisitorsLastFourWeeks: doc.Server.VisitorsEstimateLastFourWeeks,
		VisitorsTotal:         doc.Server.VisitorsEstimateTotal,
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
