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

// tokenSource is the EMA provider selected to issue an ID-JAG for a vendor.
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

// sourceFactory builds the token source for a selected provider.
type sourceFactory func(remotesessions.EMAProvider) tokenSource

// providerUnavailable marks a request whose selected provider cannot serve,
// so the interactive path applies unchanged. No other provider is tried.
var providerUnavailable = newOutcome(StageAuthorization, ReasonProviderUnavailable, ConfidenceVerified, false)

// unavailableBecause is providerUnavailable carrying why, for logs, metrics
// and status views.
func unavailableBecause(reason remotesessions.EMAProviderUnavailableReason) Outcome {
	outcome := providerUnavailable
	outcome.Unavailable = reason
	return outcome
}

// providerSelection resolves sel's vendor provider for the endpoint. A
// provider that cannot serve is still returned, flagged, so views and logs
// can say why.
func (g *Governor) providerSelection(ctx context.Context, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProviderSelection, Outcome) {
	return g.providerSelectionWith(ctx, repo.New(g.db), logger, req, sel)
}

// readyProviderSelection decorates the selection with live Speakeasy
// readiness for status views. Acquisition reads the raw selection so
// transient authority failures keep their retryability.
func (g *Governor) readyProviderSelection(ctx context.Context, req Request, sel selection) (remotesessions.EMAProviderSelection, Outcome) {
	selection, outcome := g.providerSelection(ctx, g.logger, req, sel)
	if !outcome.Succeeded() {
		return selection, outcome
	}
	selection, err := remotesessions.ApplyEMAProviderReadiness(ctx, req.OrganizationID, selection, g.speakeasyReadiness)
	if err != nil {
		g.logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err))
	}
	return selection, success
}

// speakeasyReadiness is the status-view readiness of the organization's
// Speakeasy issuer, using the same checks as minting.
func (g *Governor) speakeasyReadiness(ctx context.Context, organizationID string, issuerID uuid.UUID) (remotesessions.EMAProviderUnavailableReason, error) {
	source, ok := g.source(remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: issuerID, Unavailable: ""}).(speakeasySource)
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

// providerSelectionWith reads the saved selection over q, such as a
// publishing transaction. Live source readiness is checked outside the
// transaction; the source's publish fence rechecks its issuer and sign-in
// under lock.
func (g *Governor) providerSelectionWith(ctx context.Context, q *repo.Queries, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProviderSelection, Outcome) {
	vendor, err := remotesessions.NormalizeVendorIssuer(sel.issuer)
	if err != nil {
		// No saved selection can name an issuer that does not normalize.
		vendor = sel.issuer
	}
	selection, err := remotesessions.EndpointEMAProvider(ctx, q, req.OrganizationID, req.UserSessionIssuerID, vendor, g.sourceAvailable(remotesessions.EMAProviderSpeakeasy))
	if err != nil {
		logger.ErrorContext(ctx, "resolve ema provider", attr.SlogError(err))
		return selection, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return selection, success
}

// servingProvider returns the provider that serves sel. A selected provider
// that cannot serve leaves the interactive path; nothing falls back.
func (g *Governor) servingProvider(ctx context.Context, logger *slog.Logger, req Request, sel selection) (remotesessions.EMAProvider, Outcome) {
	selection, outcome := g.providerSelection(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return selection.Provider, outcome
	}
	if p := selection.Provider; !p.Available() {
		logger.InfoContext(ctx, "selected ema provider cannot serve",
			attr.SlogOAuthIssuer(selection.VendorIssuer),
			attr.SlogIdentityChainingProvider(string(p.Kind)),
			attr.SlogIdentityChainingUnavailableReason(string(p.Unavailable)),
		)
		return p, unavailableBecause(p.Unavailable)
	}
	return selection.Provider, success
}

// stillServes rechecks that sel's provider is still the vendor's available
// selection before a token it produced is published or released, so an
// admin's change wins over an attempt already in flight.
func (g *Governor) stillServes(ctx context.Context, logger *slog.Logger, req Request, sel selection) Outcome {
	return g.stillServesWith(ctx, repo.New(g.db), logger, req, sel)
}

// stillServesWith is stillServes over q, such as a publishing transaction.
func (g *Governor) stillServesWith(ctx context.Context, q *repo.Queries, logger *slog.Logger, req Request, sel selection) Outcome {
	selection, outcome := g.providerSelectionWith(ctx, q, logger, req, sel)
	if !outcome.Succeeded() {
		return outcome
	}
	p, want := selection.Provider, sel.provider
	if p.Available() && p.Kind == want.Kind && p.RemoteSessionIssuerID == want.RemoteSessionIssuerID && p.InternalSessionIssuerID == want.InternalSessionIssuerID {
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
		// The endpoint's trust moved after the selection was resolved.
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
