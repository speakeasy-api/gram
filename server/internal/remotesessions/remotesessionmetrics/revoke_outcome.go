package remotesessionmetrics

// RevokeOutcome labels the upstream-revoke metric series. Distinguishing
// "skipped" from "success" matters: an issuer advertising no revocation
// endpoint is the expected case for a large share of upstreams, and folding it
// into either success or failure would make the failure rate unreadable.
type RevokeOutcome string

const (
	RevokeOutcomeSuccess RevokeOutcome = "success"

	// RevokeOutcomeSkipped means Gram had nothing to send: the issuer advertises
	// no revocation endpoint, or the session stored no token to revoke.
	RevokeOutcomeSkipped RevokeOutcome = "skipped"

	// RevokeOutcomeRejected means the upstream answered and refused. Per RFC 7009
	// §2.2 an unknown or already-revoked token is a 200, so a non-2xx is a real
	// rejection (bad client auth, unsupported token type, 503) rather than
	// "nothing to do".
	RevokeOutcomeRejected RevokeOutcome = "rejected"

	// RevokeOutcomeUnreachable means the request never produced an answer —
	// DNS, TLS, connection, or the revoke path's timeout.
	RevokeOutcomeUnreachable RevokeOutcome = "unreachable"

	// RevokeOutcomeInternal means Gram could not even build the request: an
	// unreadable stored token, an unusable client auth configuration. Separated
	// from the upstream-fault outcomes because it is Gram's bug to fix, not an
	// upstream's behavior to tolerate.
	RevokeOutcomeInternal RevokeOutcome = "internal_error"

	// RevokeOutcomeDropped means a bulk batch ran out of budget before the
	// session was attempted at all. Its own series so the batch total still adds
	// up and a rising share is legible as "batches are outgrowing the budget"
	// rather than hiding inside skipped.
	RevokeOutcomeDropped RevokeOutcome = "dropped"
)
