package admission

// AdmitReason is the low-cardinality label recorded on
// cimd.admission.decisions when a client_id is permitted. It is the
// counterpart to DenialReason, and exists because "admitted" alone answers
// none of the questions worth asking after rollout.
//
// Specifically, it separates the two ways a presets-mode issuer can admit a
// client. A rising AdmitCustom share means the curated catalog is missing
// clients that customers actually use — the same warning as
// DenialNotListed, but from the requests that succeeded rather than the
// ones that failed.
//
// It also separates exact from pattern catalog matches, which is the only
// telemetry that can show whether a wildcard entry is load-bearing: the
// matched URL cannot go in a metric dimension (unbounded), and admissions
// are too high-volume to log individually.
type AdmitReason string

const (
	// AdmitOpen: open mode admitted the client and the shadow could not
	// reach a verdict, because the lookup it needed failed. Every other
	// open-mode admission records what the shadow decided, so this value
	// means "admitted, verdict unavailable" rather than "admitted, nothing
	// consulted". A sustained rise in it is a broken measurement, not a
	// policy signal, and it is paired with an error log naming the cause.
	AdmitOpen AdmitReason = "admitted_open"

	// AdmitCatalogExact: an exact entry in Gram's curated catalog.
	AdmitCatalogExact AdmitReason = "admitted_catalog_exact"

	// AdmitCatalogPattern: a wildcard catalog entry, i.e. a vendor whose
	// client_id namespace cannot be enumerated. Watch this to confirm that
	// pattern-only vendors are reaching the authorization server at all.
	AdmitCatalogPattern AdmitReason = "admitted_catalog_pattern"

	// AdmitCustom: one of the issuer's own user_session_issuer_cimd_clients
	// rows. Unlike the others this cannot come from Evaluate — the custom
	// URL lookup is a database query the caller performs — so the caller
	// supplies it.
	AdmitCustom AdmitReason = "admitted_custom"

	// AdmitOpenNotListed: open mode admitted the client, and the shadow
	// evaluation found no rule anywhere that covers it — not an enabled
	// catalog entry, not one of the issuer's own URLs. This is the same
	// signal DenialNotListed carries, from a request that succeeded, and it
	// is the value worth alerting on now that the resting state refuses
	// nobody. A rise means the catalog is missing a client customers use.
	AdmitOpenNotListed AdmitReason = "admitted_open_not_listed"

	// AdmitOpenOversized: open mode admitted the client, and the shadow
	// refused to evaluate a client_id longer than MaxClientIDLength rather
	// than hand it to the database. Kept apart from AdmitOpen so that value
	// keeps meaning "the measurement broke": this one is working exactly as
	// intended, and on an unauthenticated endpoint a rise in it is a
	// probing campaign rather than a fault.
	AdmitOpenOversized AdmitReason = "admitted_open_oversized"
)
