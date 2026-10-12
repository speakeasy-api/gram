package identitychaining

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// issuedGrant is a validated ID-JAG plus the provenance stored with the
// credential redeemed from it.
type issuedGrant struct {
	value             string
	trustedSessionID  uuid.UUID
	trustedObtainedAt time.Time
}

// tokenSource is one EMA provider that can issue an ID-JAG for a selection.
// Sources are tried in the organization's order for the vendor.
type tokenSource interface {
	kind() remotesessions.EMAProviderKind

	// available reports whether the source can serve at all on this
	// deployment; an unavailable source is never presented as serving.
	available() bool

	// authorize rechecks the source's authority for req and records its
	// registration on sel before any credential is released or minted.
	authorize(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome

	// checkReuse confirms a stored credential derived from this source may
	// still be released.
	checkReuse(ctx context.Context, req Request, sel selection) Outcome

	// issue obtains a validated ID-JAG for sel. ready runs after the source's
	// local preparation and before any provider is contacted.
	issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome)
}

// sourceFactory builds the token source for one provider in an order.
type sourceFactory func(remotesessions.EMAProvider) tokenSource

// providerUnavailable marks a request no provider in the order can serve, so
// the interactive path applies unchanged.
var providerUnavailable = newOutcome(StageAuthorization, ReasonProviderUnavailable, ConfidenceVerified, false)

// providerOrder resolves sel's vendor order for the endpoint. Providers that
// cannot serve stay in the order, flagged, so views and logs can say why.
func (g *Governor) providerOrder(ctx context.Context, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProviderOrder, Outcome) {
	vendor, err := remotesessions.NormalizeVendorIssuer(sel.issuer)
	if err != nil {
		// No saved order can name an issuer that does not normalize.
		vendor = sel.issuer
	}
	order, err := remotesessions.EndpointEMAProviders(ctx, repo.New(g.db), req.OrganizationID, req.UserSessionIssuerID, vendor, g.sourceAvailable(remotesessions.EMAProviderSpeakeasy))
	if err != nil {
		logger.ErrorContext(ctx, "resolve ema provider order", attr.SlogError(err))
		return order, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return order, success
}

// servingProviders lists the providers that can serve sel, logging when a
// saved order leaves none.
func (g *Governor) servingProviders(ctx context.Context, logger *slog.Logger, req Request, sel selection) ([]remotesessions.EMAProvider, Outcome) {
	order, outcome := g.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return nil, outcome
	}
	for _, p := range order.Providers {
		if !p.Available() {
			logger.DebugContext(ctx, "skipping ema provider that cannot serve", attr.SlogIdentityChainingProvider(string(p.Kind)), attr.SlogReason(string(p.Unavailable)))
		}
	}
	available := order.Available()
	if len(available) == 0 {
		logger.InfoContext(ctx, "ema provider order leaves no provider that can serve",
			attr.SlogOAuthIssuer(order.VendorIssuer),
			attr.SlogIdentityChainingSkippedProviders(len(order.Providers)),
		)
		return nil, providerUnavailable
	}
	return available, success
}

// source builds the token source for p.
func (g *Governor) source(p remotesessions.EMAProvider) tokenSource {
	if build, ok := g.sources[p.Kind]; ok {
		return build(p)
	}
	return speakeasySource{}
}

// sourceAvailable reports whether kind's source can serve on this deployment.
func (g *Governor) sourceAvailable(kind remotesessions.EMAProviderKind) bool {
	return g.source(remotesessions.EMAProvider{Kind: kind, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}).available()
}

// externalSource exchanges the human's retained ID token for an ID-JAG at
// one external issuer (RFC 8693). Identity chaining holds a subject token only
// for the endpoint's trusted issuer, so issuer must still be that one.
type externalSource struct {
	c *Chainer
	// issuer is the provider's remote session issuer; Nil means the
	// endpoint's trusted issuer, whichever it is.
	issuer uuid.UUID
}

func (externalSource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderExternal
}

func (externalSource) available() bool { return true }

func (s externalSource) authorize(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome {
	if s.c == nil {
		return providerUnavailable
	}
	outcome := s.c.authorize(ctx, logger, req, sel)
	if outcome.Succeeded() && s.issuer != uuid.Nil && sel.trustedIssuerID != s.issuer {
		// The endpoint's trust moved after the order was resolved.
		return newOutcome(StageAuthorization, ReasonStaleConfiguration, ConfidenceVerified, true)
	}
	return outcome
}

func (s externalSource) checkReuse(ctx context.Context, req Request, sel selection) Outcome {
	return s.c.checkDelegation(ctx, req, sel)
}

func (s externalSource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	return s.c.exchangeGrant(ctx, logger, req, sel, ready)
}

// speakeasySource will sign ID-JAGs with the organization's internal session
// issuer (AIM-523). Until then it is unavailable and never presented as serving.
type speakeasySource struct{}

func (speakeasySource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}

func (speakeasySource) available() bool { return remotesessions.SpeakeasyEMASigning }

func (speakeasySource) authorize(context.Context, *slog.Logger, Request, *selection) Outcome {
	return providerUnavailable
}

func (speakeasySource) checkReuse(context.Context, Request, selection) Outcome {
	return providerUnavailable
}

func (speakeasySource) issue(context.Context, *slog.Logger, Request, selection, func() Outcome) (issuedGrant, Outcome) {
	return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, providerUnavailable
}
