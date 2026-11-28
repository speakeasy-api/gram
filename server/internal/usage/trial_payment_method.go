package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// trialSetupCheckoutLifetime bounds a card Checkout session. Stripe accepts
// expirations between 30 minutes and 24 hours.
const trialSetupCheckoutLifetime = time.Hour

// startTrialPath is the dashboard page that collects the card a trial starts with.
const startTrialPath = "start-trial"

// CreateTrialPaymentMethodCheckout starts an embedded setup Checkout that saves
// the card the organization's free trial starts with. The trial itself starts
// only when the verified Stripe webhook reports the saved card.
func (s *Service) CreateTrialPaymentMethodCheckout(ctx context.Context, payload *gen.CreateTrialPaymentMethodCheckoutPayload) (*gen.StripeEmbeddedCheckout, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Org(authz.OrgIDFrom(authCtx.ActiveOrganizationID)).Admin()); err != nil {
		return nil, err
	}

	setupClient, ok := s.stripeClient.(stripeclient.TrialPaymentMethodClient)
	if s.stripeClient == nil || !ok || s.siteURL == nil || !stripeclient.IsConfigured(s.stripeClient.PublishableKey()) {
		return nil, oops.E(oops.CodeUnavailable, nil, "free trial sign-up is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	organizationID := authCtx.ActiveOrganizationID
	organization, err := s.orgRepo.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load organization").LogError(ctx, s.logger)
	}
	awaiting, err := trials.AwaitingPaymentMethod(ctx, s.db, organization)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check free trial eligibility").LogError(ctx, s.logger)
	}
	if !awaiting {
		return nil, oops.E(oops.CodeConflict, nil, "this organization cannot start a free trial").LogWarn(ctx, s.logger)
	}

	billingMetadata, err := repo.New(s.db).GetBillingMetadata(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").LogError(ctx, s.logger)
	}
	identity, err := s.stripeOrganizationIdentity(ctx, organizationID, billingMetadata)
	if err != nil {
		return nil, err
	}
	customerID, err := s.resolveStripeCustomer(ctx, organizationID, authCtx.OrganizationSlug, billingMetadata, identity)
	if err != nil {
		return nil, err
	}
	stored, err := repo.New(s.db).StoreStripeCustomer(ctx, repo.StoreStripeCustomerParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: customerID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to store Stripe customer").LogError(ctx, s.logger)
	}
	if !stored.StripeCustomerID.Valid || stored.StripeCustomerID.String != customerID {
		return nil, oops.E(oops.CodeConflict, nil, "billing customer changed while Checkout was being prepared").LogWarn(ctx, s.logger)
	}

	theme := ""
	if payload != nil {
		theme = payload.Theme
	}
	returnURL := strings.TrimRight(s.platformHostBaseURL(ctx, s.siteURL), "/") + "/" + startTrialPath + "?session_id=" + stripeCheckoutSessionIDTemplate
	checkout, err := setupClient.CreateTrialSetupCheckoutSession(ctx, stripeclient.CreateTrialSetupCheckoutSessionInput{
		CustomerID:       customerID,
		OrganizationID:   organizationID,
		OrganizationSlug: authCtx.OrganizationSlug,
		UserID:           authCtx.UserID,
		ReturnURL:        returnURL,
		Dark:             theme == "dark",
		ExpiresAt:        s.checkoutNow().Add(trialSetupCheckoutLifetime),
		IdempotencyKey:   fmt.Sprintf("trial-setup:%s:%s", organizationID, uuid.NewString()),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "failed to create Stripe Checkout").LogError(ctx, s.logger)
	}
	if checkout == nil || checkout.ClientSecret == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "Stripe Checkout returned no client secret").LogError(ctx, s.logger)
	}

	return &gen.StripeEmbeddedCheckout{
		ClientSecret:   checkout.ClientSecret,
		PublishableKey: s.stripeClient.PublishableKey(),
	}, nil
}

// handleTrialSetupCheckout processes a completed setup Checkout that saved
// the card a free trial starts with. handled is false when the session is not
// a trial setup Checkout, so the caller treats the event as it did before.
func (s *Service) handleTrialSetupCheckout(ctx context.Context, logger *slog.Logger, event *stripeclient.WebhookEvent) (bool, error) {
	setupClient, ok := s.stripeClient.(stripeclient.TrialPaymentMethodClient)
	if !ok {
		return false, nil
	}
	state, err := setupClient.GetTrialSetupCheckoutSession(ctx, event.ObjectID)
	if errors.Is(err, stripeclient.ErrCheckoutSessionNotFound) {
		return false, nil
	}
	if err != nil {
		return true, oops.E(oops.CodeUnavailable, err, "failed to retrieve Stripe trial setup Checkout").LogWarn(ctx, logger)
	}
	if !state.TrialSetup {
		return false, nil
	}
	if state.ID != event.ObjectID || state.CustomerID != event.CustomerID {
		return true, oops.E(oops.CodeBadRequest, nil, "Stripe trial setup Checkout does not match the event").LogWarn(ctx, logger)
	}
	if !state.CardSaved() {
		logger.InfoContext(ctx, "skipping Stripe trial setup Checkout without a saved card")
		return true, nil
	}

	outcome, err := s.startTrialFromSavedCard(ctx, logger, event, state)
	if err != nil {
		return true, err
	}
	organizationID := outcome.organizationID
	switch {
	case organizationID == "":
	case outcome.rejected:
		// A rejected card must not be offered again at PAYG Checkout.
		if err := setupClient.DetachPaymentMethod(ctx, state.PaymentMethodID); err != nil {
			logger.WarnContext(ctx, "failed to detach a rejected trial card", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		}
	case outcome.started:
		// The trial does not depend on these. A failure leaves the card saved
		// and the trial running; PAYG Checkout still offers the saved card.
		if err := setupClient.SetDefaultPaymentMethod(ctx, state.CustomerID, state.PaymentMethodID); err != nil {
			logger.WarnContext(ctx, "failed to set the trial card as the default payment method", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		}
		if s.productFeatures != nil {
			for _, feature := range productfeatures.EnterpriseAccessBundle {
				s.productFeatures.UpdateFeatureCache(ctx, organizationID, feature, true)
			}
		}
		if s.trial != nil {
			if err := s.trial.TrialStarted(ctx, organizationID); err != nil {
				logger.ErrorContext(ctx, "failed to notify trial started", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
			}
		}
	}
	return true, nil
}

// trialCardOutcome reports what a saved trial card did. organizationID is
// empty when the customer is unknown or the event was already processed.
type trialCardOutcome struct {
	organizationID string
	started        bool
	rejected       bool
}

// startTrialFromSavedCard records the card and, when the card is acceptable,
// arms the trial in one transaction behind the webhook receipt.
func (s *Service) startTrialFromSavedCard(ctx context.Context, logger *slog.Logger, event *stripeclient.WebhookEvent, state *stripeclient.TrialSetupCheckoutSessionState) (trialCardOutcome, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to begin Stripe trial setup transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	organizationID, err := queries.GetBillingMetadataOrganizationByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping Stripe trial setup Checkout for an unknown customer")
		return trialCardOutcome{organizationID: "", started: false, rejected: false}, nil
	case err != nil:
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to resolve Stripe webhook customer").LogError(ctx, logger)
	}
	if state.OrganizationID != "" && state.OrganizationID != organizationID {
		return trialCardOutcome{}, oops.E(oops.CodeBadRequest, nil, "Stripe trial setup Checkout organization does not match customer ownership").LogWarn(ctx, logger)
	}
	// Serializes with PAYG Checkout preparation and activation for the organization.
	if err := queries.LockBillingMetadataOrganization(ctx, organizationID); err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to lock billing metadata organization").LogError(ctx, logger)
	}

	inserted, err := queries.TryInsertStripeWebhookReceipt(ctx, repo.TryInsertStripeWebhookReceiptParams{
		StripeEventID:  event.ID,
		OrganizationID: organizationID,
		EventType:      event.Type,
	})
	if err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to record Stripe webhook receipt").LogError(ctx, logger)
	}
	if !inserted {
		return trialCardOutcome{organizationID: "", started: false, rejected: false}, nil
	}

	organization, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to load organization for trial setup").LogError(ctx, logger)
	}
	awaiting, err := trials.AwaitingPaymentMethod(ctx, tx, organization)
	if err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to check free trial eligibility").LogError(ctx, logger)
	}
	outcome := trialCardOutcome{organizationID: organizationID, started: false, rejected: false}
	if !awaiting {
		logger.InfoContext(ctx, "Stripe trial setup Checkout completed for an organization that cannot start a trial", attr.SlogOrganizationID(organizationID))
	} else {
		rejection, err := trials.CardRejection(ctx, tx, organizationID, state.CardFingerprint, state.CardFunding)
		if err != nil {
			return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to check the trial card").LogError(ctx, logger)
		}
		recorded, err := trialsrepo.New(tx).RecordTrialPaymentMethod(ctx, trialsrepo.RecordTrialPaymentMethodParams{
			OrganizationID:          organizationID,
			StripeCustomerID:        state.CustomerID,
			StripePaymentMethodID:   state.PaymentMethodID,
			CardFingerprint:         state.CardFingerprint,
			CardFunding:             state.CardFunding,
			StripeCheckoutSessionID: state.ID,
			RejectionReason:         conv.PtrToPGTextEmpty(&rejection),
		})
		if err != nil {
			return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to record the trial card").LogError(ctx, logger)
		}
		switch {
		case recorded == 0:
			// An accepted card is already on record; the trial it started stands.
		case rejection != "":
			logger.InfoContext(ctx, "trial card rejected", attr.SlogOrganizationID(organizationID), attr.SlogReason(rejection))
			outcome.rejected = true
		default:
			actorUserID := state.UserID
			if actorUserID == "" {
				actorUserID = "system"
			}
			err := auth.ArmEnterpriseTrialTx(ctx, tx, organization, actorUserID, "", productfeatures.SeedEnterpriseTrialBundleTx, s.auditLogger)
			switch {
			case errors.Is(err, trials.ErrNotStartable):
			case err != nil:
				return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to start the free trial").LogError(ctx, logger)
			default:
				outcome.started = true
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return trialCardOutcome{}, oops.E(oops.CodeUnexpected, err, "failed to commit Stripe trial setup").LogError(ctx, logger)
	}
	return outcome, nil
}
