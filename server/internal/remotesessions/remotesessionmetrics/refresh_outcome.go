package remotesessionmetrics

// RefreshOutcome labels the upstream-refresh metric series. The values
// partition every return from a refresh attempt, so a breakdown by outcome
// accounts for every attempt, and they are also the outcome vocabulary the
// remotesessions package hands back to callers and writes to logs, so a log
// line can be joined to its metric series.
//
// Success and failure outcomes are kept apart from each other along the axis
// a monitor cares about: whose fault it is. invalid_grant, rejected, and
// rejected_unparsed are configuration or credential problems an operator can
// act on; upstream_error, rate_limited, and unreachable are the upstream
// having a bad minute; internal_error is a Gram bug; canceled is the caller
// going away.
type RefreshOutcome string

const (
	// RefreshOutcomeRefreshed: this caller executed the upstream refresh_token
	// grant and persisted the rotated pair.
	RefreshOutcomeRefreshed RefreshOutcome = "refreshed"

	// RefreshOutcomeAdoptedConcurrentWinner: another caller refreshed the row
	// while this one was acquiring or POSTing; its persisted token was adopted
	// and no (or a losing) upstream call was made by this caller. N racing
	// callers therefore record one refreshed and N-1 of these, so a sum over
	// outcomes counts attempts, not upstream POSTs.
	RefreshOutcomeAdoptedConcurrentWinner RefreshOutcome = "adopted_concurrent_winner"

	// RefreshOutcomeSessionInactive: the row was revoked or deleted before the
	// refresh could run. Not an error, there is simply nothing to refresh. Kept
	// apart from no_grant so a monitor on no_grant is not polluted by
	// revocations.
	RefreshOutcomeSessionInactive RefreshOutcome = "session_inactive"

	// RefreshOutcomeNoGrant: no upstream call was made because the session
	// holds no refresh grant, the grant was already cleared by an earlier
	// invalid_grant, or the grant is past its own expiry.
	RefreshOutcomeNoGrant RefreshOutcome = "no_grant"

	// RefreshOutcomeInvalidGrant: the upstream definitively rejected the grant
	// (RFC 6749 §5.2 invalid_grant, including the vendor bodies the oautherr
	// package recognizes) and the dead grant was cleared. Subsequent attempts
	// on the same session record no_grant.
	RefreshOutcomeInvalidGrant RefreshOutcome = "invalid_grant"

	// RefreshOutcomeRejected: a non-5xx response whose body parsed to an RFC
	// 6749 §5.2 code other than invalid_grant, such as invalid_client or
	// unauthorized_client. Operator-actionable configuration errors; the grant
	// is kept.
	RefreshOutcomeRejected RefreshOutcome = "rejected"

	// RefreshOutcomeRejectedUnparsed: a non-5xx response whose body carried no
	// recognizable error, so nothing definitive can be concluded and the grant
	// is kept. A sustained non-zero rate names an upstream whose error body
	// the oautherr package does not yet recognize.
	RefreshOutcomeRejectedUnparsed RefreshOutcome = "rejected_unparsed"

	// RefreshOutcomeUpstreamError: any 5xx, regardless of body. Status wins
	// over a parsed server_error or temporarily_unavailable body because the
	// signal is the same either way: the upstream, not Gram's configuration,
	// is at fault.
	RefreshOutcomeUpstreamError RefreshOutcome = "upstream_error"

	// RefreshOutcomeRateLimited: the upstream returned HTTP 429. The scheduled
	// sweep stops contacting that provider for the rest of its pass.
	RefreshOutcomeRateLimited RefreshOutcome = "rate_limited"

	// RefreshOutcomeUnreachable: the request never produced an answer (DNS,
	// TLS, connection refused, or the refresh POST's own timeout).
	RefreshOutcomeUnreachable RefreshOutcome = "unreachable"

	// RefreshOutcomeCanceled: the caller's context was canceled while the
	// attempt was in flight, most often an MCP client disconnecting mid-refresh
	// or a worker shutting down. Its own outcome so that neither unreachable
	// (upstream health) nor internal_error (Gram faults) absorbs routine
	// client disconnects.
	RefreshOutcomeCanceled RefreshOutcome = "canceled"

	// RefreshOutcomeInternalError: Gram could not build or persist the request.
	// No token endpoint configured, an unreadable stored token or secret, an
	// invalid client authentication method, an empty upstream response, a
	// database or encryption failure, or a lost compare-and-swap on persist.
	RefreshOutcomeInternalError RefreshOutcome = "internal_error"
)
