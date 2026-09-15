package presidiofp

import "slices"

// retiredRecognizers are Presidio entity types whose recognizer produces noise
// at a rate that makes every one of its findings untrustworthy, so the scanners
// drop them outright rather than filtering value by value.
//
// The live scanners already refuse these before this package is consulted (see
// findingLevelDropEntities in the risk_analysis package and _FINDING_LEVEL_DROP
// in the pystreams scanner). Listing them here as well is what lets the offline
// sweep reconcile history with that decision: findings stored before the live
// drop landed are still sitting in risk_results, and a value-only catalog entry
// is what marks them as false positives.
//
// TestRetiredRecognizersMatchScannerDrops in the risk_analysis package keeps
// this map and the live drop set in agreement.
var retiredRecognizers = map[EntityType]string{
	// Its patterns match a leading letter followed by a run of digits — the
	// shape of Figma file and node ids, short object ids, and countless other
	// opaque identifiers — at a score that any nearby "id", "number", "card"
	// or "lic" lemma (including the ones inside "public" and "duplicate")
	// lifts over the reporting threshold. Upstream has known about it since
	// 2023: microsoft/presidio#1063.
	EntityTypeUSDriverLicense: "US driver license recognizer retired: matches arbitrary letter-and-digit identifiers",
}

// retiredReason returns the retirement reason for an entity type, or "" when
// the recognizer is still trusted.
func retiredReason(entityType EntityType) string {
	return retiredRecognizers[entityType]
}

// RetiredEntityTypes returns, in a stable order, the entity types whose
// findings are always classified as false positives. Exported for the
// cross-package test that locks this set to the scanners' own drop list.
func RetiredEntityTypes() []EntityType {
	out := make([]EntityType, 0, len(retiredRecognizers))
	for entity := range retiredRecognizers {
		out = append(out, entity)
	}
	slices.Sort(out)
	return out
}
