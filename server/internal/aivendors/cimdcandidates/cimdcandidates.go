// Package cimdcandidates records CIMD clients that issuers admit without any
// catalog document covering them, as a staff review queue.
//
// A candidate is recorded once its document has been fetched and validated,
// next to the client row write, so writes happen only on first resolution or
// cache expiry. Recording never changes an admission result: failures are
// logged and dropped.
package cimdcandidates

import (
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// Status is a candidate's review state. Only StatusPending is open; the others
// are decisions and never return to pending.
type Status string

const (
	// StatusPending awaits staff review.
	StatusPending Status = "pending"

	// StatusApproved led to a catalog document.
	StatusApproved Status = "approved"

	// StatusDismissed is not a client the catalog should list.
	StatusDismissed Status = "dismissed"

	// StatusCustomerSpecific belongs to one customer, not the shared catalog.
	StatusCustomerSpecific Status = "customer_specific"
)

// MaxPendingPerIssuer caps the new candidates one issuer can raise. Anyone can
// point clients at an open-mode issuer, so without a bound one caller hosting
// many valid documents could fill the global queue. Real issuers see a handful
// of distinct uncovered clients; 50 leaves room for those while keeping a
// flood to a page of review. Candidates that already exist are still sighted
// past the cap. The cap is soft: concurrent writes can overshoot it slightly.
const MaxPendingPerIssuer = 50

// admitsUncovered reports whether mode lets in a client no catalog document
// covers without enforcing the catalog. Presets enforces it, admitting such a
// client only through the issuer's own allowlist; disabled admits nothing.
func admitsUncovered(mode admission.Mode) bool {
	return mode == admission.ModeOpen || mode == admission.ModeReporting
}

// Recordable reports whether a client admitted under mode belongs in the review
// queue: the mode admits uncovered clients, no catalog document matches the
// client ID, and it is not a document Speakeasy publishes for one of its
// assistants on assistantOrigin.
//
// A disabled document keeps its match, so its clients are covered: staff
// already decided about that URL.
//
// A nil assistantOrigin matches the assistant document path on any origin, for
// a caller that cannot know the deployment's outbound origin.
func Recordable(catalog *aivendors.Snapshot, mode admission.Mode, assistantOrigin *url.URL, clientID string) bool {
	if !admitsUncovered(mode) {
		return false
	}
	if _, ok := catalog.CatalogDocument(clientID); ok {
		return false
	}
	return !isAssistantDocument(assistantOrigin, clientID)
}

func isAssistantDocument(origin *url.URL, clientID string) bool {
	if origin == nil {
		u, err := url.Parse(clientID)
		if err != nil {
			return false
		}
		origin = &url.URL{Scheme: u.Scheme, Host: u.Host}
	}
	_, ok := assistants.ParseAssistantClientMetadataDocumentURL(origin, clientID)
	return ok
}
