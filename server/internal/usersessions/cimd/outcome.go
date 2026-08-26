package cimd

// Outcome classifies how one resolution attempt ended. It is the management
// surface's view of what Resolve deliberately flattens into an opaque error;
// see the package doc for why the two callers disclose different amounts.
type Outcome string

const (
	// OutcomeValid means the document was fetched, parsed, and passed every
	// check the AS imposes. The client_id will be admitted (subject to the
	// issuer's admission policy, which this package does not evaluate).
	OutcomeValid Outcome = "valid"

	// OutcomeInvalidURL means the client_id itself violates -02 §3 syntax.
	// No fetch was attempted.
	OutcomeInvalidURL Outcome = "invalid_url"

	// OutcomeUnreachable means Gram could not obtain an HTTP 200 body from
	// the URL: connection failure, timeout, non-200 status (including a
	// redirect, which §5 forbids following), or a body over the size cap.
	OutcomeUnreachable Outcome = "unreachable"

	// OutcomeUnparseable means the endpoint answered 200 but the body is not
	// valid JSON.
	OutcomeUnparseable Outcome = "unparseable"

	// OutcomeInvalidDocument means the body parsed as JSON but the document
	// violates the spec or this server's policy rules. Reason carries the
	// specific rule.
	OutcomeInvalidDocument Outcome = "invalid_document"
)
