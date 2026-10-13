package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// assertionSigner only exposes the organization's internal signing operation.
type assertionSigner interface {
	Sign(context.Context, string, internalsessionissuers.Claims) (string, error)
	CheckAuthority(context.Context, string, internalsessionissuers.Status) error
}

type issuerStatus interface {
	Status(context.Context, string) (internalsessionissuers.Status, error)
}

// SetSpeakeasyIssuer enables the internal provider with the same lifecycle
// gate used by management. Call it before serving requests; nil disables it.
func (g *Governor) SetSpeakeasyIssuer(gate *internalsessionissuers.Gate, signer *internalsessionissuers.Signer) {
	if gate == nil || !signer.Available() {
		return
	}
	g.sources[remotesessions.EMAProviderSpeakeasy] = func(p remotesessions.EMAProvider) tokenSource {
		return speakeasySource{g: g, gate: gate, signer: signer, issuerID: p.InternalSessionIssuerID}
	}
}

type speakeasySource struct {
	g        *Governor
	gate     issuerStatus
	signer   assertionSigner
	issuerID uuid.UUID
}

func (speakeasySource) kind() remotesessions.EMAProviderKind {
	return remotesessions.EMAProviderSpeakeasy
}
func (s speakeasySource) available() bool { return s.g != nil && s.gate != nil && s.signer != nil }

func (s speakeasySource) authorize(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome {
	if !s.available() {
		return providerUnavailable
	}
	if req.UserSessionJTI == "" {
		return newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	q := repo.New(s.g.db)
	grant, err := q.GetSpeakeasyEMAGrant(ctx, repo.GetSpeakeasyEMAGrantParams{
		OrganizationID: conv.ToPGText(req.OrganizationID), ProjectID: req.ProjectID,
		UserSessionIssuerID: req.UserSessionIssuerID, Jti: req.UserSessionJTI, SubjectUrn: urn.NewUserSubject(req.UserID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	if err != nil {
		logger.ErrorContext(ctx, "read authenticated EMA grant", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	result, err := ssosignins.CheckSignInIdentity(ctx, s.g.db, req.OrganizationID, grant.ID, s.g.now(), ssosignins.MaxAge)
	if err != nil {
		logger.ErrorContext(ctx, "check EMA sign-in", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	sel.userSessionID = grant.ID
	return s.authorizeIdentity(ctx, logger, req, sel, result)
}

func (s speakeasySource) checkConsent(ctx context.Context, logger *slog.Logger, req Request, sel selection, signInID uuid.UUID) Outcome {
	result, err := ssosignins.CheckConsentIdentity(ctx, s.g.db, req.OrganizationID, req.UserID, signInID, s.g.now())
	if err != nil {
		logger.ErrorContext(ctx, "check EMA consent sign-in", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return s.authorizeIdentity(ctx, logger, req, &sel, result)
}

func (s speakeasySource) authorizeIdentity(ctx context.Context, logger *slog.Logger, req Request, sel *selection, result ssosignins.Result) Outcome {
	if !result.Eligible() || result.Identity.UserID != req.UserID {
		return newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
	}
	status, outcome := s.configuration(ctx, logger, req.OrganizationID)
	if !outcome.Succeeded() {
		return outcome
	}
	sel.signInID = result.Identity.SignInID
	sel.email = result.Identity.Email
	sel.internalIssuerVersion = status.Issuer.UpdatedAt.Time
	sel.internalKeyID = status.ActiveKey.ID
	sel.audience = sel.issuer
	return success
}

// configuration checks live issuer readiness without requiring a user's grant
// or signing an assertion. Provider discovery uses the same authority as minting.
func (s speakeasySource) configuration(ctx context.Context, logger *slog.Logger, organizationID string) (internalsessionissuers.Status, Outcome) {
	if !s.available() {
		return internalsessionissuers.Status{State: internalsessionissuers.StateUnavailable, Issuer: nil, RolledOut: false, Entitled: false, ActiveKey: nil}, providerUnavailable
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "check EMA issuer", attr.SlogError(err))
		return status, newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	if status.State != internalsessionissuers.StateEnabled || status.Issuer == nil || status.ActiveKey == nil || status.Issuer.ID != s.issuerID {
		return status, providerUnavailable
	}
	if err := s.signer.CheckAuthority(ctx, organizationID, status); err != nil {
		logger.WarnContext(ctx, "check EMA deployment authority", attr.SlogError(err))
		if errors.Is(err, internalsessionissuers.ErrMisconfigured) {
			return status, newOutcome(StageAuthorization, ReasonConfigurationRequired, ConfidenceVerified, false)
		}
		return status, newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return status, success
}

func (s speakeasySource) checkReuse(ctx context.Context, req Request, sel selection) Outcome {
	current := sel
	outcome := s.authorize(ctx, s.g.logger, req, &current)
	if !outcome.Succeeded() {
		return outcome
	}
	if current.signInID != sel.signInID || current.userSessionID != sel.userSessionID || current.email != sel.email || current.internalKeyID != sel.internalKeyID || !current.internalIssuerVersion.Equal(sel.internalIssuerVersion) {
		return newOutcome(StagePersistence, ReasonStaleConfiguration, ConfidenceVerified, true)
	}
	return success
}

func (s speakeasySource) issue(ctx context.Context, logger *slog.Logger, req Request, sel selection, ready func() Outcome) (issuedGrant, Outcome) {
	var none issuedGrant
	if outcome := s.checkReuse(ctx, req, sel); !outcome.Succeeded() {
		return none, outcome
	}
	if outcome := ready(); !outcome.Succeeded() {
		return none, outcome
	}
	assertion, err := s.signer.Sign(ctx, req.OrganizationID, internalsessionissuers.Claims{
		Subject: req.UserID, Email: sel.email, Audience: sel.audience, ClientID: sel.externalClientID,
		Resource: sel.resource, Scope: strings.Join(sel.scopes, " "), ID: uuid.NewString(),
		Expiry: s.g.now().Add(time.Minute), Extra: nil,
	})
	if err != nil {
		logger.WarnContext(ctx, "sign EMA assertion", attr.SlogError(err))
		if _, ok := errors.AsType[*internalsessionissuers.SigningRefusedError](err); ok {
			return none, providerUnavailable
		}
		if errors.Is(err, internalsessionissuers.ErrMisconfigured) {
			return none, newOutcome(StageExchange, ReasonConfigurationRequired, ConfidenceVerified, false)
		}
		return none, newOutcome(StageExchange, ReasonTransientFailure, ConfidenceVerified, true)
	}
	if outcome := s.checkReuse(ctx, req, sel); !outcome.Succeeded() {
		return none, outcome
	}
	return issuedGrant{value: assertion, trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, success
}

// checkSpeakeasyPublish fences revocation against publication. The transaction
// holds the sign-in row before grant changes, and the issuer key set before
// issuer state, matching containment's key-write lock order.
func (c *Chainer) checkSpeakeasyPublish(ctx context.Context, tx pgx.Tx, req Request, sel selection) error {
	q := repo.New(tx)
	if _, err := q.LockSpeakeasyEMASignIn(ctx, repo.LockSpeakeasyEMASignInParams{ID: sel.signInID, OrganizationID: req.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errStale
		}
		return fmt.Errorf("lock EMA sign-in: %w", err)
	}
	if _, err := q.LockSpeakeasyEMAKeySet(ctx, repo.LockSpeakeasyEMAKeySetParams{ID: sel.provider.InternalSessionIssuerID, OrganizationID: req.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errStale
		}
		return fmt.Errorf("lock EMA issuer key set: %w", err)
	}
	if _, err := q.LockSpeakeasyEMAIssuer(ctx, repo.LockSpeakeasyEMAIssuerParams{ID: sel.provider.InternalSessionIssuerID, OrganizationID: req.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errStale
		}
		return fmt.Errorf("lock EMA issuer: %w", err)
	}
	result, err := ssosignins.CheckSignInIdentity(ctx, tx, req.OrganizationID, sel.userSessionID, c.now(), ssosignins.MaxAge)
	if err != nil {
		return fmt.Errorf("recheck EMA sign-in: %w", err)
	}
	if !result.Eligible() || result.Identity.SignInID != sel.signInID || result.Identity.UserID != req.UserID || result.Identity.Email != sel.email {
		return errStale
	}
	current, err := q.GetSpeakeasyEMAIssuerVersion(ctx, repo.GetSpeakeasyEMAIssuerVersionParams{ID: sel.provider.InternalSessionIssuerID, OrganizationID: req.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errStale
	}
	if err != nil {
		return fmt.Errorf("recheck EMA issuer: %w", err)
	}
	if current.KeyID != sel.internalKeyID || !current.UpdatedAt.Time.Equal(sel.internalIssuerVersion) {
		return errStale
	}
	return nil
}
