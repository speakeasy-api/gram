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
	// AdmitOpen: the issuer skips admission entirely. Document validation
	// still ran, but no policy was consulted.
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
)
