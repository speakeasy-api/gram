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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
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
// gate used by management. flags evaluates sign-in enforcement, which
// Speakeasy signing requires. Call it before serving requests; a nil gate,
// or a signer this deployment cannot use, leaves the provider unavailable.
func (g *Governor) SetSpeakeasyIssuer(gate *internalsessionissuers.Gate, signer *internalsessionissuers.Signer, flags feature.Provider) {
	if gate == nil || !signer.Available() {
		return
	}
	g.sources[remotesessions.EMAProviderSpeakeasy] = func(p remotesessions.EMAProvider) tokenSource {
		return speakeasySource{g: g, gate: gate, signer: signer, flags: flags, issuerID: p.InternalSessionIssuerID}
	}
}

// SpeakeasyReadiness is the management API's readiness check for the
// Speakeasy provider, with the same checks runtime minting applies.
func SpeakeasyReadiness(logger *slog.Logger, gate *internalsessionissuers.Gate, signer *internalsessionissuers.Signer, flags feature.Provider) remotesessions.EMAProviderReadiness {
	g := NewGovernor(logger, nil, nil, nil)
	g.SetSpeakeasyIssuer(gate, signer, flags)
	return g.speakeasyReadiness
}

type speakeasySource struct {
	g        *Governor
	gate     issuerStatus
	signer   assertionSigner
	flags    feature.Provider
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
	if req.consent() {
		return s.checkConsent(ctx, logger, req, sel)
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
	result, err := signins.CheckSignInIdentity(ctx, s.g.db, req.OrganizationID, grant.ID, s.g.now(), signins.MaxAge)
	if err != nil {
		logger.ErrorContext(ctx, "check EMA sign-in", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	sel.userSessionID = grant.ID
	return s.authorizeIdentity(ctx, logger, req, sel, result)
}

func (s speakeasySource) checkConsent(ctx context.Context, logger *slog.Logger, req Request, sel *selection) Outcome {
	result, err := signins.CheckConsentIdentity(ctx, s.g.db, req.OrganizationID, req.UserID, req.consentSignInID, s.g.now())
	if err != nil {
		logger.ErrorContext(ctx, "check EMA consent sign-in", attr.SlogError(err))
		return newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return s.authorizeIdentity(ctx, logger, req, sel, result)
}

func (s speakeasySource) authorizeIdentity(ctx context.Context, logger *slog.Logger, req Request, sel *selection, result signins.Result) Outcome {
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
	sel.internalKeyPublishedAt = status.ActiveKey.CreatedAt.Time
	sel.audience = sel.issuer
	return success
}

// configuration checks live issuer readiness without requiring a user's grant
// or signing an assertion. Provider discovery uses the same authority as
// minting. Speakeasy vouches for sign-ins only while the organization's
// sign-in enforcement is on, so it fails closed without it.
func (s speakeasySource) configuration(ctx context.Context, logger *slog.Logger, organizationID string) (internalsessionissuers.Status, Outcome) {
	status := internalsessionissuers.Status{State: internalsessionissuers.StateUnavailable, Issuer: nil, RolledOut: false, HasVerifiedDomain: false, VerifiedDomains: nil, ActiveKey: nil, PauseReason: ""}
	if !s.available() {
		return status, unavailableBecause(remotesessions.EMAProviderSigningUnavailable)
	}
	if !signins.Enforced(ctx, s.flags, organizationID) {
		logger.WarnContext(ctx, "Speakeasy EMA provider needs sign-in enforcement", attr.SlogIdentityChainingUnavailableReason(string(remotesessions.EMAProviderSignInEnforcement)))
		return status, unavailableBecause(remotesessions.EMAProviderSignInEnforcement)
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "check EMA issuer", attr.SlogError(err))
		return status, newOutcome(StageAuthorization, ReasonTransientFailure, ConfidenceVerified, true)
	}
	if reason := issuerUnavailableReason(status, s.issuerID); reason != "" {
		logger.WarnContext(ctx, "Speakeasy EMA provider cannot serve",
			attr.SlogIdentityChainingUnavailableReason(string(reason)),
			attr.SlogReason(string(status.PauseReason)),
		)
		return status, unavailableBecause(reason)
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
	assertion, outcome := s.sign(ctx, logger, req.OrganizationID, internalsessionissuers.Claims{
		Subject: req.UserID, Email: sel.email, Audience: sel.audience, ClientID: sel.externalClientID,
		Resource: sel.resource, Scope: strings.Join(sel.scopes, " "), ID: uuid.NewString(),
		Expiry: s.g.now().Add(time.Minute), Extra: nil,
	})
	if !outcome.Succeeded() {
		return none, outcome
	}
	if outcome := s.checkReuse(ctx, req, sel); !outcome.Succeeded() {
		return none, outcome
	}
	return issuedGrant{value: assertion, trustedSessionID: uuid.Nil, trustedObtainedAt: time.Time{}}, success
}

// sign maps the signer's refusals to outcomes. Refusals that retrying cannot
// change are never reported as transient.
func (s speakeasySource) sign(ctx context.Context, logger *slog.Logger, organizationID string, claims internalsessionissuers.Claims) (string, Outcome) {
	assertion, err := s.signer.Sign(ctx, organizationID, claims)
	if err == nil {
		return assertion, success
	}
	logger.WarnContext(ctx, "sign EMA assertion", attr.SlogError(err))
	if refused, ok := errors.AsType[*internalsessionissuers.SigningRefusedError](err); ok {
		return "", unavailableBecause(stateUnavailableReason(refused.State, true))
	}
	switch {
	case errors.Is(err, internalsessionissuers.ErrInvalidClaims):
		// Sign refuses an email outside the organization's verified
		// domains; signing in again cannot fix it.
		return "", newOutcome(StageExchange, ReasonIneligibleIdentity, ConfidenceVerified, false)
	case errors.Is(err, internalsessionissuers.ErrMisconfigured):
		return "", newOutcome(StageExchange, ReasonConfigurationRequired, ConfidenceVerified, false)
	}
	return "", newOutcome(StageExchange, ReasonTransientFailure, ConfidenceVerified, true)
}

// issuerUnavailableReason says why status cannot sign for issuerID; empty
// when it can.
func issuerUnavailableReason(status internalsessionissuers.Status, issuerID uuid.UUID) remotesessions.EMAProviderUnavailableReason {
	if status.State == internalsessionissuers.StateEnabled && (status.Issuer == nil || status.ActiveKey == nil) {
		return remotesessions.EMAProviderSigningUnavailable
	}
	if status.State == internalsessionissuers.StateEnabled && status.Issuer.ID != issuerID {
		// The selection names an issuer the organization replaced.
		return remotesessions.EMAProviderDeleted
	}
	return stateUnavailableReason(status.State, status.RolledOut)
}

// stateUnavailableReason maps an issuer state that cannot sign to the reason
// status views show; empty for an enabled issuer.
func stateUnavailableReason(state internalsessionissuers.State, rolledOut bool) remotesessions.EMAProviderUnavailableReason {
	switch state {
	case internalsessionissuers.StateEnabled:
		return ""
	case internalsessionissuers.StateUnavailable:
		if !rolledOut {
			return remotesessions.EMAProviderNotRolledOut
		}
		return remotesessions.EMAProviderSigningUnavailable
	case internalsessionissuers.StateVerifiedDomainRequired:
		return remotesessions.EMAProviderVerifiedDomain
	case internalsessionissuers.StateSetupRequired:
		return remotesessions.EMAProviderSetupRequired
	case internalsessionissuers.StateDisabled:
		return remotesessions.EMAProviderDisabled
	case internalsessionissuers.StateSigningPaused:
		return remotesessions.EMAProviderSigningPaused
	case internalsessionissuers.StateKeyless:
		return remotesessions.EMAProviderKeyless
	}
	return remotesessions.EMAProviderSigningUnavailable
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
	var result signins.Result
	var err error
	switch {
	case req.consent():
		result, err = signins.CheckConsentIdentity(ctx, tx, req.OrganizationID, req.UserID, req.consentSignInID, c.now())
	case sel.userSessionID != uuid.Nil:
		result, err = signins.CheckSignInIdentity(ctx, tx, req.OrganizationID, sel.userSessionID, c.now(), signins.MaxAge)
	default:
		return errStale
	}
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

// credentialSource says where a released Speakeasy credential came from, for
// support to tell reuse, adoption and minting apart.
type credentialSource string

const (
	// credentialNone marks a release with no source worth logging: a
	// consent acceptance without a token, or a waiter adopting a holder's.
	credentialNone         credentialSource = ""
	credentialCached       credentialSource = "cached"
	credentialAdopted      credentialSource = "adopted"
	credentialMinted       credentialSource = "minted"
	credentialStaleDropped credentialSource = "stale_dropped"
)

// adoptConsentCredential binds a credential minted on the consent page to a
// grant backed by the same sign-in, so the first tool call reuses it instead
// of signing again. The ID-JAG names no MCP client, so any such grant could
// mint the identical token itself. It takes the publish locks and rechecks the
// grant's sign-in, so a revocation or rotation since consent leaves the
// credential unadopted. The source is credentialCached when a concurrent
// first use adopted it first.
func (c *Chainer) adoptConsentCredential(ctx context.Context, logger *slog.Logger, req Request, sel selection) (Token, credentialSource, bool) {
	var none Token
	if sel.provider.Kind != remotesessions.EMAProviderSpeakeasy || req.consent() || sel.userSessionID == uuid.Nil {
		return none, credentialNone, false
	}
	pending := req
	pending.UserSessionJTI = ""
	pending.consentSignInID = sel.signInID
	// Most misses have no consent credential; skip the locks for them.
	judged, err := readCredential(ctx, repo.New(c.db), pending, sel, c.now())
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "read consent identity chaining credential", attr.SlogError(err))
		}
		return none, credentialNone, false
	}
	adopted, err := c.adopt(ctx, logger, req, pending, sel, judged)
	switch {
	case errors.Is(err, errStale):
		logger.InfoContext(ctx, "consent identity chaining credential not adopted under current provenance", attr.SlogIdentityChainingCredentialSource(credentialStaleDropped))
		return none, credentialNone, false
	case err != nil:
		logger.WarnContext(ctx, "adopt consent identity chaining credential", attr.SlogError(err))
		return none, credentialNone, false
	}
	// A concurrent first use of the same grant may have adopted it first.
	token, ok := c.cachedToken(ctx, logger, req, sel)
	source := credentialCached
	if adopted {
		source = credentialAdopted
	}
	return token, source, ok
}

// adopt rereads the consent credential under the publish fence and binds it
// to sel's grant. judged is the version read before the fence. A credential
// that current provenance no longer admits is retired and errStale returned.
// It reports false when a concurrent adoption or retirement won.
func (c *Chainer) adopt(ctx context.Context, logger *slog.Logger, req, pending Request, sel selection, judged repo.GetEMACredentialForUseRow) (bool, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin consent credential adoption: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	retireStale := func(row repo.GetEMACredentialForUseRow) (bool, error) {
		retired, err := q.RetireEMACredential(ctx, repo.RetireEMACredentialParams{ID: row.ID, OrganizationID: conv.ToPGText(req.OrganizationID), ProjectID: uuid.NullUUID{UUID: req.ProjectID, Valid: true}, ExpectedUpdatedAt: row.UpdatedAt})
		if err != nil {
			return false, fmt.Errorf("retire stale consent credential: %w", err)
		}
		if retired == 0 {
			return false, nil
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit stale consent credential retirement: %w", err)
		}
		return false, errStale
	}
	if err := c.lockPublishFence(ctx, tx, logger, req, sel); err != nil {
		if errors.Is(err, errStale) {
			return retireStale(judged)
		}
		return false, err
	}
	row, err := readCredential(ctx, q, pending, sel, c.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	case !row.Usable:
		return retireStale(row)
	}
	adopted, err := q.AdoptSpeakeasyConsentEMACredential(ctx, repo.AdoptSpeakeasyConsentEMACredentialParams{
		UserSessionID: sel.userSessionID, ID: row.ID, OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, SignInID: sel.signInID,
	})
	if err != nil {
		return false, fmt.Errorf("adopt consent credential: %w", err)
	}
	if adopted == 0 {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit consent credential adoption: %w", err)
	}
	return true, nil
}
