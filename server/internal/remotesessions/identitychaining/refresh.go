package identitychaining

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

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
	// fresh sign-in, or may once a transient fault clears.
	RefreshServed RefreshDecision = "served"

	// RefreshUnconfirmed: chaining owns the upstream but cannot confirm it
	// serves. Either a transient fault hid the answer, or the configuration
	// is ambiguous or broken; the runtime answers the latter with a hard
	// forbidden that no reconnect repairs, so refusing the refresh would only
	// sign the client out.
	RefreshUnconfirmed RefreshDecision = "unconfirmed"

	// RefreshReauthenticate: every provider that can serve needs the human
	// to sign in again, which the runtime answers with a reconnect challenge.
	RefreshReauthenticate RefreshDecision = "reauthenticate"
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
// token. It never acquires, retires or renews anything. Retryable outcomes
// read as served, so an outage never demands a reconnect.
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
	sel, outcome := g.selectBindingWith(ctx, logger, req, g.storedResource)
	verdict.Reason = outcome.Reason
	switch {
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
			verdict.Provider = p.Kind
			verdict.Reason = outcome.Reason
			return verdict
		}
		failures = append(failures, outcome)
	}
	outcome = exhausted(failures)
	verdict.Provider = outcome.Provider
	verdict.Reason = outcome.Reason
	switch {
	case outcome.Reason == ReasonReauthenticationRequired:
		verdict.Decision = RefreshReauthenticate
	case !outcome.Applicable():
		verdict.Decision = RefreshNotChained
	default:
		verdict.Decision = RefreshUnconfirmed
	}
	return verdict
}

// refreshFrom runs, from stored state, the checks Acquire runs on p before it
// releases a stored credential or issues a new one: the source's authority
// and, for an external provider, the human's retained delegation, which must
// still yield an assertion unless a stored credential remains usable. The
// Speakeasy source's authority already checks the grant's sign-in.
//
// A retained delegation that no longer matches the trusted registration reads
// as needing reauthentication. The runtime reports it as configuration, but
// the human's next sign-in retains a delegation under the current
// registration, so a reconnect is exactly what repairs it.
func (g *Governor) refreshFrom(ctx context.Context, logger *slog.Logger, req Request, sel selection, p remotesessions.EMAProvider) Outcome {
	src := g.source(p)
	current := sel
	current.provider = p
	if outcome := src.authorize(ctx, logger, req, &current); !outcome.Succeeded() {
		return outcome
	}
	if _, local := src.(speakeasySource); local {
		return success
	}
	outcome := src.checkReuse(ctx, req, current)
	if outcome.Succeeded() {
		if g.storedCredentialUsable(ctx, req, current) {
			return success
		}
		outcome = g.checkResolvable(ctx, req, current)
	}
	if outcome.Stage == StageDelegation && outcome.Reason == ReasonConfigurationRequired && g.delegation != nil {
		return newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	return outcome
}

// storedCredentialUsable reports whether sel's stored credential is usable now
// and still decrypts, as the runtime requires before releasing it.
func (g *Governor) storedCredentialUsable(ctx context.Context, req Request, sel selection) bool {
	row, err := readCredential(ctx, repo.New(g.db), req, sel, g.now())
	if err != nil || !row.Usable || g.enc == nil {
		return false
	}
	value, err := g.enc.Decrypt(row.AccessTokenEncrypted.String)
	return err == nil && value != ""
}
