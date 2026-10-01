package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// authority authorizes use of a human's retained delegation for one endpoint.
// It is the delegation service's Authorizer for identity chaining.
type authority struct {
	chainer *Chainer
	req     Request
}

// AuthorizeDelegation requires the endpoint's organization-level user session
// issuer to trust exactly b's registration, the project to be live in the
// organization, and the human to be the request's live organization member.
func (a authority) AuthorizeDelegation(ctx context.Context, b remotesessions.DelegationBinding) error {
	if b.OrganizationID != a.req.OrganizationID || b.HumanID != a.req.UserID || b.IssuerID == uuid.Nil || b.ClientID == uuid.Nil {
		return remotesessions.ErrDelegationConfiguration
	}
	authorized, err := repo.New(a.chainer.db).AuthorizeEMADelegation(ctx, repo.AuthorizeEMADelegationParams{
		UserSessionIssuerID: a.req.UserSessionIssuerID,
		OrganizationID:      conv.ToPGText(a.req.OrganizationID),
		TrustedIssuerID:     uuid.NullUUID{UUID: b.IssuerID, Valid: true},
		TrustedClientID:     uuid.NullUUID{UUID: b.ClientID, Valid: true},
		ProjectID:           a.req.ProjectID,
		UserID:              a.req.UserID,
	})
	if err != nil {
		return fmt.Errorf("authorize delegation: %w", err)
	}
	if !authorized {
		return remotesessions.ErrDelegationConfiguration
	}
	return nil
}

// checkDelegation confirms the human's retained delegation still belongs to the
// current trusted registration before any credential derived from it is
// released, so a rotated or reconfigured registration retires stored tokens.
func (c *Chainer) checkDelegation(ctx context.Context, req Request, sel selection) Outcome {
	binding := remotesessions.DelegationBinding{OrganizationID: req.OrganizationID, IssuerID: sel.trustedIssuerID, ClientID: sel.trustedClientID, HumanID: req.UserID}
	switch err := c.delegation.Check(ctx, binding, authority{chainer: c, req: req}); {
	case err == nil:
		return success
	case errors.Is(err, remotesessions.ErrDelegationReauthentication):
		return newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
	case errors.Is(err, remotesessions.ErrDelegationConfiguration):
		return newOutcome(StageDelegation, ReasonConfigurationRequired, ConfidenceVerified, false)
	default:
		return newOutcome(StageDelegation, ReasonTransientFailure, ConfidenceVerified, true)
	}
}

// authorize rechecks the endpoint's trusted identity provider registration
// and the human's live membership before any credential is released or
// minted, and records the trusted registration and ID-JAG audience on sel.
func (c *Chainer) authorize(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome {
	issuer, err := repo.New(c.db).GetEMAChainingUserIssuer(ctx, repo.GetEMAChainingUserIssuerParams{ID: req.UserSessionIssuerID, OrganizationID: conv.ToPGText(req.OrganizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return newOutcome(StageAuthorization, ReasonConfigurationRequired, ConfidenceVerified, false)
	case err != nil:
		logger.ErrorContext(ctx, "read identity chaining user session issuer", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	sel.trustedIssuerID = issuer.TrustedRemoteSessionIssuerID.UUID
	sel.trustedClientID = issuer.TrustedRemoteSessionClientID.UUID
	audience, err := repo.New(c.db).GetEMAChainingConfirmedAudience(ctx, repo.GetEMAChainingConfirmedAudienceParams{
		OrganizationID: req.OrganizationID, TrustedIssuerID: sel.trustedIssuerID, RemoteSessionIssuerID: sel.remoteIssuerID, Resource: sel.resource,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		sel.audience = sel.issuer
	case err != nil:
		logger.ErrorContext(ctx, "read identity chaining confirmed audience", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	default:
		sel.audience = audience
	}
	binding := remotesessions.DelegationBinding{OrganizationID: req.OrganizationID, IssuerID: sel.trustedIssuerID, ClientID: sel.trustedClientID, HumanID: req.UserID}
	if err := (authority{chainer: c, req: req}).AuthorizeDelegation(ctx, binding); err != nil {
		if errors.Is(err, remotesessions.ErrDelegationConfiguration) {
			return newOutcome(StageAuthorization, ReasonConfigurationRequired, ConfidenceVerified, false)
		}
		logger.ErrorContext(ctx, "authorize identity chaining delegation", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return success
}
