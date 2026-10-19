package identitychaining

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// RefreshDecision is identity chaining's answer for an MCP grant refresh whose
// stored upstream sessions need a reconnect.
type RefreshDecision string

const (
	// RefreshNotChained: chaining does not serve the upstream, so the
	// interactive path and its reconnect apply. No ready binding names the
	// upstream, or no provider in its order can serve, which the runtime
	// answers with the same reconnect challenge.
	RefreshNotChained RefreshDecision = "none"

	// RefreshServed: a provider in the order can serve the grant without a
	// fresh sign-in.
	RefreshServed RefreshDecision = "served"

	// RefreshUnconfirmed: chaining may own the upstream but stored state
	// cannot confirm either way. A read failed or a fault hid the answer, the
	// discovery cache cannot name the runtime's resource, or the
	// configuration is ambiguous or needs an administrator; no reconnect
	// repairs any of these, so refusing the refresh would only sign the
	// client out.
	RefreshUnconfirmed RefreshDecision = "unconfirmed"

	// RefreshReauthenticate: stored state confirms that every provider that
	// can serve needs the human to sign in again, and that the sign-in
	// repairs it.
	RefreshReauthenticate RefreshDecision = "reauthenticate"

	// RefreshRefused: a provider recently refused chaining for good and no
	// stored credential serves, so the runtime answers with the reconnect
	// challenge and only the consent page's separate sign-in serves the
	// upstream.
	RefreshRefused RefreshDecision = "refused"
)

// RefreshVerdict is RefreshDecision with what produced it, for logs.
type RefreshVerdict struct {
	// Decision is the answer.
	Decision RefreshDecision

	// RemoteSessionIssuerID is the issuer of the binding chaining selected;
	// Nil when none was selected.
	RemoteSessionIssuerID uuid.UUID

	// Provider is the EMA provider kind that decided; empty when the answer
	// came before any provider was checked.
	Provider remotesessions.EMAProviderKind

	// Reason is the outcome behind the decision.
	Reason Reason
}

// RefreshDecision judges, from stored state alone, whether identity chaining
// would serve req's upstream on the grant being refreshed (req.UserSessionJTI)
// without the human signing in again. It selects the binding once, resolving
// the resource only from the shared discovery cache, then runs per provider
// the authority and reuse checks Acquire runs before releasing or issuing a
// token. It never acquires, retires or renews anything. Only a confirmed need
// to sign in again is RefreshReauthenticate; a failed read, a fault or a
// resource the cache cannot name is RefreshUnconfirmed, so an outage never
// demands a reconnect.
func (g *Governor) RefreshDecision(ctx context.Context, req Request) RefreshVerdict {
	verdict := RefreshVerdict{Decision: RefreshNotChained, RemoteSessionIssuerID: uuid.Nil, Provider: "", Reason: ReasonNotApplicable}
	if !req.complete() || req.UserID == "" {
		return verdict
	}
	logger := g.logger.With(
		attr.SlogOrganizationID(req.OrganizationID),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogUserSessionIssuerID(req.UserSessionIssuerID.String()),
		attr.SlogUserID(req.UserID),
	)
	configured, err := g.configured(ctx, req.OrganizationID, req.ProjectID, req.UserSessionIssuerID)
	switch {
	case err != nil:
		logger.WarnContext(ctx, "count identity chaining bindings for refresh", attr.SlogError(err))
		verdict.Decision = RefreshUnconfirmed
		verdict.Reason = ReasonTransientFailure
		return verdict
	case !configured:
		return verdict
	}
	sel, outcome, err := g.selectBindingWith(ctx, logger, req, g.storedResource)
	verdict.Reason = outcome.Reason
	switch {
	case err != nil:
		verdict.Decision = RefreshUnconfirmed
		verdict.Reason = ReasonTransientFailure
		return verdict
	case outcome.Succeeded():
	case !outcome.Applicable():
		return verdict
	default:
		verdict.Decision = RefreshUnconfirmed
		return verdict
	}
	verdict.RemoteSessionIssuerID = sel.remoteIssuerID
	order, outcome := g.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		verdict.Decision = RefreshUnconfirmed
		verdict.Reason = outcome.Reason
		return verdict
	}
	failures := make([]Outcome, 0, len(order.Providers))
	for _, p := range order.Providers {
		outcome := unavailableBecause(p.Unavailable)
		if p.Available() {
			outcome = g.refreshFrom(ctx, logger.With(attr.SlogIdentityChainingProvider(string(p.Kind))), req, sel, p)
		}
		outcome.Provider = p.Kind
		if outcome.Succeeded() || outcome.Retryable {
			verdict.Decision = RefreshServed
			if outcome.Retryable {
				verdict.Decision = RefreshUnconfirmed
			}
			verdict.Provider = p.Kind
			verdict.Reason = outcome.Reason
			return verdict
		}
		failures = append(failures, outcome)
	}
	outcome = exhausted(failures)
	verdict.Provider = outcome.Provider
	verdict.Reason = outcome.Reason
	verdict.Decision = refreshDecisionFor(outcome, failures)
	return verdict
}

// refreshDecisionFor judges the refresh once every provider in the order
// failed; outcome is exhausted(failures). A provider that is unavailable only
// for now will serve again without a reconnect, so it keeps the refresh
// unconfirmed unless a fresh sign-in repairs another provider.
func refreshDecisionFor(outcome Outcome, failures []Outcome) RefreshDecision {
	switch {
	case outcome.Reason == ReasonReauthenticationRequired:
		return RefreshReauthenticate
	case slices.ContainsFunc(failures, func(f Outcome) bool { return f.Reason == ReasonProviderUnavailable && unavailableForNow(f.Unavailable) }):
		return RefreshUnconfirmed
	case slices.ContainsFunc(failures, func(f Outcome) bool { return f.Cached }):
		return RefreshRefused
	case !outcome.Applicable():
		return RefreshNotChained
	}
	return RefreshUnconfirmed
}

// unavailableForNow reports an unavailability that clears without anyone
// acting: a signing pause (startup, an indeterminate or stale kill switch
// read), a keyless issuer awaiting its replacement key, a readiness or
// sign-in enforcement read that failed. Every other reason is configuration
// an administrator set, and leaves the upstream on the interactive path.
func unavailableForNow(reason remotesessions.EMAProviderUnavailableReason) bool {
	switch reason {
	case remotesessions.EMAProviderSigningPaused, remotesessions.EMAProviderKeyless,
		remotesessions.EMAProviderReadinessUnknown, remotesessions.EMAProviderSignInEnforcement:
		return true
	case remotesessions.EMAProviderDeleted, remotesessions.EMAProviderDisabled, remotesessions.EMAProviderNotTrustedByEndpoint,
		remotesessions.EMAProviderSigningUnavailable, remotesessions.EMAProviderNotRolledOut,
		remotesessions.EMAProviderVerifiedDomain, remotesessions.EMAProviderSetupRequired:
		return false
	}
	return false
}

// refreshFrom runs, from stored state, the checks Acquire runs on p before it
// releases a stored credential or issues a new one: the source's authority
// and, for an external provider, the human's retained delegation, which must
// still yield an assertion unless a stored credential remains usable. The
// Speakeasy source's authority already checks the grant's sign-in.
//
// A retained delegation that predates the current trusted registration reads
// as needing reauthentication. The runtime reports it as configuration, but
// the human's next sign-in retains a delegation under the current
// registration, so a reconnect is exactly what repairs it. Every other
// configuration failure, such as a renewal the provider refused for the
// configured client or a retained secret that no longer decrypts, stays
// configuration: signing in again cannot repair it. A sign-in decrypts the
// retained subject before replacing anything and refuses when it cannot.
func (g *Governor) refreshFrom(ctx context.Context, logger *slog.Logger, req Request, sel selection, p remotesessions.EMAProvider) Outcome {
	src := g.source(p)
	current := sel
	current.provider = p
	if outcome := src.authorize(ctx, logger, req, &current); !outcome.Succeeded() {
		return outcome
	}
	if src.kind() == remotesessions.EMAProviderSpeakeasy {
		return g.refusedWithoutCredential(ctx, logger, req, current)
	}
	if g.delegation == nil {
		return delegationUnchecked
	}
	binding := remotesessions.DelegationBinding{OrganizationID: req.OrganizationID, IssuerID: current.trustedIssuerID, ClientID: current.trustedClientID, HumanID: req.UserID}
	auth := authority{db: g.db, req: req}
	err := g.delegation.Check(ctx, binding, auth)
	if err == nil {
		usable, readErr := g.storedCredentialUsable(ctx, req, current)
		switch {
		case readErr != nil:
			logger.WarnContext(ctx, "read identity chaining credential for refresh", attr.SlogError(readErr))
			return newOutcome(StagePersistence, ReasonTransientFailure, ConfidenceUnknown, true)
		case usable:
			return success
		}
		err = g.delegation.CheckResolvable(ctx, binding, auth)
	}
	if errors.Is(err, remotesessions.ErrDelegationStaleRegistration) {
		return newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	if outcome := delegationOutcome(err); !outcome.Succeeded() {
		return outcome
	}
	return g.refusedWithoutCredential(ctx, logger, req, current)
}

// refusedWithoutCredential is the provider's recent definitive refusal of sel
// when no stored credential would serve instead, as acquisition answers it;
// success otherwise.
func (g *Governor) refusedWithoutCredential(ctx context.Context, logger *slog.Logger, req Request, sel selection) Outcome {
	if g.recentRefusal == nil {
		return success
	}
	refused, ok := g.recentRefusal(ctx, req, sel)
	if !ok {
		return success
	}
	usable, err := g.storedCredentialUsable(ctx, req, sel)
	switch {
	case err != nil:
		logger.WarnContext(ctx, "read identity chaining credential for refresh", attr.SlogError(err))
		return newOutcome(StagePersistence, ReasonTransientFailure, ConfidenceUnknown, true)
	case usable:
		return success
	}
	return refused
}

// storedCredentialUsable reports whether sel's stored credential is usable now
// and still decrypts, as the runtime requires before releasing it. A missing
// or undecryptable credential is unusable; a failed read is an error.
func (g *Governor) storedCredentialUsable(ctx context.Context, req Request, sel selection) (bool, error) {
	row, err := readCredential(ctx, repo.New(g.db), req, sel, g.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	case !row.Usable || g.enc == nil:
		return false, nil
	}
	value, err := g.enc.Decrypt(row.AccessTokenEncrypted.String)
	return err == nil && value != "", nil
}
