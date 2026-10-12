package identitychaining

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

// tokenSource is one identity provider that can issue an ID-JAG for a
// selection. Sources are tried in the organization's order for the vendor.
type tokenSource interface {
	kind() remotesessions.EMAProviderKind

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

// providerUnavailable marks a source that cannot serve the request at all, so
// the next source is tried and the outcome never masks a real failure.
var providerUnavailable = newOutcome(StageAuthorization, ReasonProviderUnavailable, ConfidenceVerified, false)

// providerOrder lists the provider kinds allowed for sel's vendor, in the
// order they are tried. With no policy rows the default is the endpoint's
// external identity provider, then Speakeasy. A row naming a deleted or
// disabled supplier, or an external issuer other than the one the endpoint
// trusts, cannot serve it and is skipped; a policy left with nothing serves
// nobody.
func (g *Governor) providerOrder(ctx context.Context, logger *slog.Logger, req Request, sel selection) ([]remotesessions.EMAProviderKind, Outcome) {
	defaults := []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal, remotesessions.EMAProviderSpeakeasy}
	vendor, err := remotesessions.NormalizeVendorIssuer(sel.issuer)
	if err != nil {
		// A policy is keyed by a normalizable issuer, so none can name this one.
		return defaults, success
	}
	q := repo.New(g.db)
	rows, err := q.ListEMAVendorIdentityProviders(ctx, repo.ListEMAVendorIdentityProvidersParams{OrganizationID: req.OrganizationID, VendorIssuer: vendor})
	if err != nil {
		logger.ErrorContext(ctx, "list identity chaining provider policy", attr.SlogError(err))
		return nil, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceVerified, true)
	}
	if len(rows) == 0 {
		return defaults, success
	}
	trusted, outcome := g.endpointTrustedIssuer(ctx, logger, req)
	if !outcome.Succeeded() {
		return nil, outcome
	}
	var kinds []remotesessions.EMAProviderKind
	for _, row := range rows {
		kind := remotesessions.EMAProviderSpeakeasy
		if row.RemoteSessionIssuerID.Valid {
			kind = remotesessions.EMAProviderExternal
		}
		switch {
		case row.ProviderUnavailable:
			continue
		case kind == remotesessions.EMAProviderExternal && row.RemoteSessionIssuerID != trusted:
			continue
		}
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return nil, providerUnavailable
	}
	return kinds, success
}

// endpointTrustedIssuer is the remote session issuer the endpoint's user
// session issuer trusts, which external chaining exchanges at; invalid when
// there is none.
func (g *Governor) endpointTrustedIssuer(ctx context.Context, logger *slog.Logger, req Request) (uuid.NullUUID, Outcome) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	issuer, err := repo.New(g.db).GetEMAChainingUserIssuer(ctx, repo.GetEMAChainingUserIssuerParams{ID: req.UserSessionIssuerID, OrganizationID: conv.ToPGText(req.OrganizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, success
	case err != nil:
		logger.ErrorContext(ctx, "read identity chaining user session issuer", attr.SlogError(err))
		return none, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return issuer.TrustedRemoteSessionIssuerID, success
}

// externalSource exchanges the human's retained identity provider ID token for
// an ID-JAG at the endpoint's trusted identity provider (RFC 8693).
type externalSource struct{ c *Chainer }

func (externalSource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderExternal
}

func (s externalSource) authorize(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome {
	return s.c.authorize(ctx, logger, req, sel)
}

func (s externalSource) checkReuse(ctx context.Context, req Request, sel selection) Outcome {
	return s.c.checkDelegation(ctx, req, sel)
}

func (s externalSource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	return s.c.exchangeGrant(ctx, logger, req, sel, ready)
}

// speakeasySource will sign ID-JAGs with the organization's Speakeasy issuer
// (AIM-523). Until then it never serves, so the next source is tried.
type speakeasySource struct{}

func (speakeasySource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}

func (speakeasySource) authorize(context.Context, *slog.Logger, Request, *selection) Outcome {
	return providerUnavailable
}

func (speakeasySource) checkReuse(context.Context, Request, selection) Outcome {
	return providerUnavailable
}

func (speakeasySource) issue(context.Context, *slog.Logger, Request, selection, func() Outcome) (issuedGrant, Outcome) {
	return issuedGrant{value: "", trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, providerUnavailable
}

// source returns the token source for kind.
func (c *Chainer) source(kind remotesessions.EMAProviderKind) tokenSource {
	if src, ok := c.sourceOverrides[kind]; ok {
		return src
	}
	if kind == remotesessions.EMAProviderExternal {
		return externalSource{c: c}
	}
	return speakeasySource{}
}
