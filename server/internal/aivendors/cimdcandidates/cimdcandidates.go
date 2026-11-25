// Package cimdcandidates records CIMD clients that open-mode issuers admit
// without any catalog document covering them, as a staff review queue.
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

// MaxPendingPerIssuer caps the pending candidates one issuer can raise. Anyone
// can point clients at an open-mode issuer, so without a bound one caller
// hosting many valid documents could fill the global queue. Real issuers see a
// handful of distinct uncovered clients; 50 leaves room for those while
// keeping a flood to a page of review. Candidates that already exist are still
// sighted past the cap.
const MaxPendingPerIssuer = 50

// Recordable reports whether a client admitted under mode belongs in the review
// queue: the issuer is open, no catalog document matches the client ID, and it
// is not a document Speakeasy publishes for one of its assistants.
//
// A disabled document keeps its match, so its clients are covered: staff
// already decided about that URL.
func Recordable(catalog *aivendors.Snapshot, mode admission.Mode, clientID string) bool {
	if mode != admission.ModeOpen {
		return false
	}
	if _, ok := catalog.CatalogDocument(clientID); ok {
		return false
	}
	return !isAssistantDocument(clientID)
}

// isAssistantDocument matches the assistant document path on any origin, so
// the backfill, which does not know the deployment's outbound origin, applies
// the same rule as sign-in.
func isAssistantDocument(clientID string) bool {
	u, err := url.Parse(clientID)
	if err != nil {
		return false
	}
	_, ok := assistants.ParseAssistantClientMetadataDocumentURL(&url.URL{Scheme: u.Scheme, Host: u.Host}, clientID)
	return ok
}
