package identitychaining

import (
	"context"
	"fmt"
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

// tokenSource is one EMA provider in a vendor's order, able to issue an
// ID-JAG for it.
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

// sourceFactory builds the token source for a provider in the order.
type sourceFactory func(remotesessions.EMAProvider) tokenSource

// providerUnavailable marks a provider that cannot serve the request. The
// next provider in the order is tried; when none can serve, the interactive
// path applies unchanged.
var providerUnavailable = newOutcome(StageAuthorization, ReasonProviderUnavailable, ConfidenceVerified, false)

// unavailableBecause is providerUnavailable carrying why, for logs, metrics
// and status views.
func unavailableBecause(reason remotesessions.EMAProviderUnavailableReason) Outcome {
	outcome := providerUnavailable
	outcome.Unavailable = reason
	return outcome
}

// providerOrder resolves sel's vendor provider order for the endpoint.
// Providers that cannot serve are kept, flagged, so attempts skip them and
// views and logs can say why.
func (g *Governor) providerOrder(ctx context.Context, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProviderOrder, Outcome) {
	return g.providerOrderWith(ctx, repo.New(g.db), logger, req, sel)
}

// readyProviderOrder decorates the order with live Speakeasy readiness for
// status views. Acquisition reads the raw order so transient authority
// failures keep their retryability.
func (g *Governor) readyProviderOrder(ctx context.Context, req Request, sel selection) (remotesessions.EMAProviderOrder, Outcome) {
	order, outcome := g.providerOrder(ctx, g.logger, req, sel)
	if !outcome.Succeeded() {
		return order, outcome
	}
	order, err := remotesessions.ApplyEMAProviderReadiness(ctx, req.OrganizationID, order, g.speakeasyReadiness)
	if err != nil {
		g.logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err))
	}
	return order, success
}

// speakeasyReadiness is the status-view readiness of the organization's
// Speakeasy issuer, using the same checks as minting.
func (g *Governor) speakeasyReadiness(ctx context.Context, organizationID string, issuerID uuid.UUID) (remotesessions.EMAProviderUnavailableReason, error) {
	source, ok := g.source(remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: issuerID, Unavailable: "", LastAcceptedAt: nil}).(speakeasySource)
	if !ok || !source.available() {
		return remotesessions.EMAProviderSigningUnavailable, nil
	}
	_, outcome := source.configuration(ctx, g.logger, organizationID)
	switch {
	case outcome.Succeeded():
		return "", nil
	case outcome.Retryable:
		return remotesessions.EMAProviderReadinessUnknown, fmt.Errorf("issuer readiness: %s", outcome.Reason)
	case outcome.Unavailable != "":
		return outcome.Unavailable, nil
	}
	return remotesessions.EMAProviderSigningUnavailable, nil
}

// providerOrderWith reads the saved order over q, such as a publishing
// transaction. Live source readiness is checked outside the transaction; the
// source's publish fence rechecks its issuer and sign-in under lock.
func (g *Governor) providerOrderWith(ctx context.Context, q *repo.Queries, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProviderOrder, Outcome) {
	vendor, err := remotesessions.NormalizeVendorIssuer(sel.issuer)
	if err != nil {
		// No saved order can name an issuer that does not normalize.
		vendor = sel.issuer
	}
	order, err := remotesessions.EndpointEMAProviders(ctx, q, req.OrganizationID, req.UserSessionIssuerID, vendor, g.sourceAvailable(remotesessions.EMAProviderSpeakeasy))
	if err != nil {
		logger.ErrorContext(ctx, "resolve ema provider order", attr.SlogError(err))
		return order, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return order, success
}

// stillServes rechecks that sel's provider is still in the vendor's order and
// can serve before a token it produced is published or released, so an
// admin's change wins over an attempt already in flight. A provider other
// than the order's first may still serve: the order prefers providers for new
// credentials and never discards one a provider in it issued.
func (g *Governor) stillServes(ctx context.Context, logger *slog.Logger, req Request, sel selection) Outcome {
	return g.stillServesWith(ctx, repo.New(g.db), logger, req, sel)
}

// stillServesWith is stillServes over q, such as a publishing transaction.
func (g *Governor) stillServesWith(ctx context.Context, q *repo.Queries, logger *slog.Logger, req Request, sel selection) Outcome {
	order, outcome := g.providerOrderWith(ctx, q, logger, req, sel)
	if !outcome.Succeeded() {
		return outcome
	}
	if order.Serves(sel.provider) {
		return success
	}
	return newOutcome(StagePersistence, ReasonStaleConfiguration, ConfidenceVerified, true)
}

// source builds the token source for p.
func (g *Governor) source(p remotesessions.EMAProvider) tokenSource {
	if build, ok := g.sources[p.Kind]; ok {
		return build(p)
	}
	return new(speakeasySource)
}

// sourceAvailable reports whether kind's source can serve on this deployment.
func (g *Governor) sourceAvailable(kind remotesessions.EMAProviderKind) bool {
	return g.source(remotesessions.EMAProvider{Kind: kind, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil}).available()
}

// externalSource exchanges the human's retained ID token for an ID-JAG at
// one external issuer (RFC 8693). Identity chaining holds a subject token only
// for the endpoint's trusted issuer, so issuer must still be that one.
type externalSource struct {
	// g checks the endpoint's trust and the human's retained delegation.
	g *Governor

	// c exchanges and redeems; nil on a Governor, which never issues.
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
	if s.g == nil {
		return providerUnavailable
	}
	outcome := s.g.authorize(ctx, logger, req, sel)
	if outcome.Succeeded() && s.issuer != uuid.Nil && sel.trustedIssuerID != s.issuer {
		// The endpoint's trust moved after the order was resolved; this
		// provider reports stale configuration and the next one is tried.
		return newOutcome(StageAuthorization, ReasonStaleConfiguration, ConfidenceVerified, true)
	}
	return outcome
}

func (s externalSource) checkReuse(ctx context.Context, req Request, sel selection) Outcome {
	if s.g == nil {
		return providerUnavailable
	}
	return s.g.checkDelegation(ctx, req, sel)
}

func (s externalSource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	if s.c == nil {
		return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, providerUnavailable
	}
	return s.c.exchangeGrant(ctx, logger, req, sel, ready)
}
