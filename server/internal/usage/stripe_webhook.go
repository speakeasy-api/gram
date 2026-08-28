package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	maxStripeWebhookBodyBytes       = 1 << 20
	stripeLifecycleReconcileTimeout = 10 * time.Second
)

var acceptedStripeWebhookEvents = map[string]struct{}{
	"checkout.session.completed":    {},
	"customer.subscription.deleted": {},
	"invoice.created":               {},
	"invoice.payment_failed":        {},
}

type stripeWebhookResult struct {
	newlyEnabledFeatures []productfeatures.Feature
	reconcileKeyTypes    []openrouter.KeyType
	invoicePaymentFailed bool
	subscriptionLost     bool
}

type stripeWebhookHandler func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error)

func (s *Service) handleStripeWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, r.Body.Close)

	r.Body = http.MaxBytesReader(w, r.Body, maxStripeWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return oops.E(oops.CodeRequestTooLarge, err, "Stripe webhook body is too large").LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeBadRequest, err, "invalid Stripe webhook body").LogWarn(ctx, s.logger)
	}

	event, err := s.stripeClient.VerifyWebhook(body, r.Header.Get("Stripe-Signature"))
	if err != nil {
		if errors.Is(err, stripeclient.ErrWebhookNotConfigured) {
			return oops.E(oops.CodeUnavailable, err, "Stripe webhooks are not configured").LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeBadRequest, err, "invalid Stripe webhook signature").LogWarn(ctx, s.logger)
	}
	if event == nil || event.ID == "" || event.Type == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe webhook event").LogWarn(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogEvent(event.Type), attr.SlogStripeWebhookEventID(event.ID))
	if _, ok := acceptedStripeWebhookEvents[event.Type]; !ok {
		logger.InfoContext(ctx, "skipping unsupported Stripe webhook event")
		return nil
	}
	if event.Type == "checkout.session.completed" && (event.ObjectID == "" || event.CustomerID == "" || event.SubscriptionID == "") {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe Checkout completion identifiers").LogWarn(ctx, logger)
	}
	if event.Type == "customer.subscription.deleted" && event.ObjectID == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe subscription deletion identifier").LogWarn(ctx, logger)
	}
	if event.CustomerID == "" {
		logger.WarnContext(ctx, "skipping Stripe webhook event without a customer")
		return nil
	}
	receipt, err := repo.New(s.db).GetStripeWebhookReceipt(ctx, event.ID)
	if err == nil {
		return s.repairCommittedStripeLifecycleEvent(ctx, logger, receipt)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "failed to check Stripe webhook receipt").LogError(ctx, logger)
	}
	if event.Type == "invoice.created" {
		_, err := repo.New(s.db).GetBillingMetadataOrganizationByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			logger.WarnContext(ctx, "skipping Stripe webhook event for an unknown customer")
			return nil
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "failed to resolve Stripe webhook customer").LogError(ctx, logger)
		}
	}

	var checkoutState *stripeclient.CheckoutSessionState
	var invoiceState *stripeclient.InvoiceState
	checkoutEligible := true
	if event.Type == "checkout.session.completed" {
		checkoutState, checkoutEligible, err = s.currentCheckoutSession(ctx, event)
		if err != nil {
			return err
		}
	}
	if event.Type == "invoice.created" {
		invoiceState, err = s.currentStripeInvoice(ctx, event)
		if err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin Stripe webhook transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	var subscriptionLockID string
	switch {
	case event.Type == "checkout.session.completed" && checkoutEligible:
		subscriptionLockID = event.SubscriptionID
	case event.Type == "customer.subscription.deleted":
		subscriptionLockID = event.ObjectID
	}
	if subscriptionLockID != "" {
		if err := queries.AcquireStripeSubscriptionActivationLock(ctx, subscriptionLockID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to lock Stripe subscription state").LogError(ctx, logger)
		}
	}

	// Resolve routing identity without taking a row lock. The lifecycle handler
	// validates the customer and subscription again from GetPaygActivationState
	// after all canonical advisory locks have been acquired.
	organizationID, err := queries.GetBillingMetadataOrganizationByStripeCustomerID(ctx, pgtype.Text{String: event.CustomerID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping Stripe webhook event for an unknown customer")
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to resolve Stripe webhook customer").LogError(ctx, logger)
	}

	if event.Type == "checkout.session.completed" && checkoutEligible {
		trialQueries := trialsrepo.New(tx)
		_, err := trialQueries.LockTrialLifecycleForRearm(ctx, organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "failed to lock enterprise trial lifecycle").LogError(ctx, logger)
		default:
			if _, err := trialQueries.MarkTrialConverted(ctx, organizationID); err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to mark enterprise trial converted").LogError(ctx, logger)
			}
		}
	}

	// Lifecycle transactions use one deployment-safe order even when replacement
	// checkout and prior-subscription deletion hold different subscription locks:
	// trial row when applicable, every OpenRouter key advisory lock, then billing
	// and key rows. Trial demotion follows the same trial/key prefix.
	if (event.Type == "checkout.session.completed" && checkoutEligible) || event.Type == "customer.subscription.deleted" {
		if err := acquireOpenRouterBillingLocks(ctx, queries, organizationID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to lock OpenRouter billing state").LogError(ctx, logger)
		}
	}

	if event.Type == "checkout.session.completed" && checkoutEligible {
		// Serialize Stripe activation with legacy billing-metadata writes only after
		// OpenRouter locks. This keeps all lifecycle paths advisory-before-row ordered.
		if err := queries.LockBillingMetadataOrganization(ctx, organizationID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to lock billing metadata organization").LogError(ctx, logger)
		}
	}

	inserted, err := queries.TryInsertStripeWebhookReceipt(ctx, repo.TryInsertStripeWebhookReceiptParams{
		StripeEventID:  event.ID,
		OrganizationID: organizationID,
		EventType:      event.Type,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record Stripe webhook receipt").LogError(ctx, logger)
	}
	if !inserted {
		receipt, err := queries.GetStripeWebhookReceipt(ctx, event.ID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load committed Stripe webhook receipt after insert conflict").LogError(ctx, logger)
		}
		if err := tx.Rollback(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to end duplicate Stripe webhook transaction").LogError(ctx, logger)
		}
		return s.repairCommittedStripeLifecycleEvent(ctx, logger, receipt)
	}
	if !checkoutEligible {
		if err := tx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to commit Stripe webhook receipt").LogError(ctx, logger)
		}
		return nil
	}

	result, err := s.stripeHandler(ctx, logger, tx, organizationID, event, checkoutState, invoiceState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to handle Stripe webhook event").LogError(ctx, logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to commit Stripe webhook transaction").LogError(ctx, logger)
	}
	if s.productFeatures != nil {
		for _, enabledFeature := range result.newlyEnabledFeatures {
			s.productFeatures.UpdateFeatureCache(ctx, organizationID, enabledFeature, true)
		}
	}
	if result.invoicePaymentFailed && s.stripeMetrics != nil {
		s.stripeMetrics.RecordInvoicePaymentFailed(ctx)
	}
	if result.subscriptionLost && s.stripeMetrics != nil {
		s.stripeMetrics.RecordSubscriptionLost(ctx)
	}
	if reconciler, ok := s.openRouter.(openrouter.DisableStateReconciler); ok {
		for _, keyType := range result.reconcileKeyTypes {
			reconcileCtx, cancel := context.WithTimeout(ctx, stripeLifecycleReconcileTimeout)
			err := reconciler.ReconcileAPIKeyDisabled(reconcileCtx, organizationID, keyType)
			cancel()
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to reconcile committed OpenRouter lifecycle state").LogError(ctx, logger)
			}
		}
	}

	return nil
}

func (s *Service) repairCommittedStripeLifecycleEvent(ctx context.Context, logger *slog.Logger, receipt repo.GetStripeWebhookReceiptRow) error {
	logger.InfoContext(ctx, "repairing duplicate Stripe lifecycle event from committed state")
	var err error
	switch receipt.EventType {
	case "checkout.session.completed":
		err = s.repairReplayedPaygCheckout(ctx, logger, receipt.OrganizationID)
	case "customer.subscription.deleted":
		err = RepairPaygOpenRouterChatKey(ctx, logger, s.db, s.openRouter, receipt.OrganizationID, openrouter.KeyDesiredStateDisabled)
	default:
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to repair replayed Stripe lifecycle event").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) repairReplayedPaygCheckout(ctx context.Context, logger *slog.Logger, organizationID string) error {
	if err := RepairPaygOpenRouterChatKey(ctx, logger, s.db, s.openRouter, organizationID, openrouter.KeyDesiredStateEnabled); err != nil {
		return err
	}

	reconciler, ok := s.openRouter.(openrouter.DisableStateReconciler)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot reconcile replayed PAYG checkout lifecycle state")
	}
	for index, keyType := range openrouter.AllKeyTypes {
		if index == 0 {
			if keyType != openrouter.KeyTypeChat {
				return errors.New("OpenRouter key reconciliation order must start with chat")
			}
			continue
		}
		reconcileCtx, cancel := context.WithTimeout(ctx, stripeLifecycleReconcileTimeout)
		err := reconciler.ReconcileAPIKeyDisabled(reconcileCtx, organizationID, keyType)
		cancel()
		if err != nil {
			return fmt.Errorf("reconcile replayed PAYG checkout OpenRouter %s key: %w", keyType, err)
		}
	}
	return nil
}

func acquireOpenRouterBillingLocks(ctx context.Context, queries *repo.Queries, organizationID string) error {
	for _, keyType := range openrouter.AllKeyTypes {
		if err := queries.AcquireOpenRouterBillingLock(ctx, repo.AcquireOpenRouterBillingLockParams{
			KeyType:        string(keyType),
			OrganizationID: organizationID,
		}); err != nil {
			return fmt.Errorf("lock %s inference billing state: %w", keyType, err)
		}
	}
	return nil
}

func (s *Service) currentStripeInvoice(ctx context.Context, event *stripeclient.WebhookEvent) (*stripeclient.InvoiceState, error) {
	if event.ObjectID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid Stripe invoice identifier").LogWarn(ctx, s.logger)
	}
	state, err := s.stripeClient.GetInvoice(ctx, event.ObjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "failed to retrieve current Stripe invoice").LogWarn(ctx, s.logger)
	}
	if state == nil || state.ID != event.ObjectID || state.CustomerID == "" || state.CustomerID != event.CustomerID {
		return nil, oops.E(oops.CodeBadRequest, nil, "Stripe invoice identifiers do not match current state").LogWarn(ctx, s.logger)
	}
	if state.BillingReason == "subscription_create" || state.BillingReason == "subscription_cycle" {
		if state.SubscriptionID == "" || state.ServicePeriodStart.IsZero() {
			return nil, oops.E(oops.CodeBadRequest, nil, "Stripe subscription invoice identifiers do not match current state").LogWarn(ctx, s.logger)
		}
		if state.BillingReason == "subscription_cycle" && !state.ServicePeriodEnd.After(state.ServicePeriodStart) {
			return nil, oops.E(oops.CodeBadRequest, nil, "Stripe subscription invoice identifiers do not match current state").LogWarn(ctx, s.logger)
		}
	}
	return state, nil
}

func (s *Service) currentCheckoutSession(ctx context.Context, event *stripeclient.WebhookEvent) (*stripeclient.CheckoutSessionState, bool, error) {
	if event.ObjectID == "" || event.SubscriptionID == "" {
		return nil, false, oops.E(oops.CodeBadRequest, nil, "invalid Stripe Checkout completion identifiers").LogWarn(ctx, s.logger)
	}

	state, err := s.stripeClient.GetCheckoutSession(ctx, event.ObjectID)
	if err != nil {
		return nil, false, oops.E(oops.CodeUnavailable, err, "failed to retrieve current Stripe Checkout session").LogWarn(ctx, s.logger)
	}
	if state == nil || state.ID != event.ObjectID || state.CustomerID == "" || state.CustomerID != event.CustomerID || state.SubscriptionID == "" || state.SubscriptionID != event.SubscriptionID || state.SubscriptionCustomerID != event.CustomerID || state.BillingCycleAnchor.IsZero() {
		return nil, false, oops.E(oops.CodeBadRequest, nil, "Stripe Checkout completion identifiers do not match current state").LogWarn(ctx, s.logger)
	}

	switch state.Status {
	case "expired":
		return state, false, nil
	case "complete":
	default:
		return nil, false, oops.E(oops.CodeUnavailable, nil, "Stripe Checkout session is not complete").LogWarn(ctx, s.logger)
	}

	switch state.SubscriptionStatus {
	case "active", "trialing", "past_due":
		return state, true, nil
	case "canceled", "incomplete_expired", "unpaid":
		return state, false, nil
	default:
		return nil, false, oops.E(oops.CodeUnavailable, nil, "Stripe subscription is not ready for activation").LogWarn(ctx, s.logger)
	}
}

func (s *Service) serviceStripeWebhookHandler(ctx context.Context, logger *slog.Logger, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState, invoice *stripeclient.InvoiceState) (stripeWebhookResult, error) {
	switch event.Type {
	case "checkout.session.completed":
		return s.activatePaygCheckout(ctx, tx, organizationID, event, checkout)
	case "invoice.created":
		recorded, err := s.recordStripeInvoice(ctx, tx, organizationID, event, invoice)
		if err != nil {
			return stripeWebhookResult{}, err
		}
		if recorded {
			logger.InfoContext(ctx, "recorded Stripe invoice creation")
		} else {
			logger.InfoContext(ctx, "skipped non-billable Stripe invoice creation")
		}
	case "invoice.payment_failed":
		logger.InfoContext(ctx, "received Stripe invoice payment failure")
		return stripeWebhookResult{newlyEnabledFeatures: nil, reconcileKeyTypes: nil, invoicePaymentFailed: true, subscriptionLost: false}, nil
	case "customer.subscription.deleted":
		return s.deactivatePaygSubscription(ctx, tx, organizationID, event)
	}
	return stripeWebhookResult{newlyEnabledFeatures: nil, reconcileKeyTypes: nil, invoicePaymentFailed: false, subscriptionLost: false}, nil
}

func getClassifiedOpenRouterKeyTx(ctx context.Context, tx pgx.Tx, organizationID string, keyType openrouter.KeyType) (*openrouterrepo.OpenrouterApiKey, error) {
	key, err := openrouterrepo.New(tx).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read OpenRouter %s key lifecycle: %w", keyType, err)
	case key.DisableCauses == nil:
		return nil, fmt.Errorf("OpenRouter %s key lifecycle causes are unclassified", keyType)
	default:
		return &key, nil
	}
}

func addOpenRouterDisableCauseTx(ctx context.Context, tx pgx.Tx, organizationID string, keyType openrouter.KeyType, cause openrouter.DisableCause) error {
	key, err := getClassifiedOpenRouterKeyTx(ctx, tx, organizationID, keyType)
	if err != nil || key == nil {
		return err
	}
	_, err = openrouterrepo.New(tx).AddOpenRouterAPIKeyDisableCause(ctx, openrouterrepo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
		KeyHash:        key.KeyHash,
		DisableCause:   string(cause),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("OpenRouter %s key changed while adding %s lifecycle cause", keyType, cause)
	}
	if err != nil {
		return fmt.Errorf("persist OpenRouter %s key %s lifecycle cause: %w", keyType, cause, err)
	}
	return nil
}

func removeOpenRouterDisableCauseTx(ctx context.Context, tx pgx.Tx, organizationID string, keyType openrouter.KeyType, cause openrouter.DisableCause) error {
	key, err := getClassifiedOpenRouterKeyTx(ctx, tx, organizationID, keyType)
	if err != nil || key == nil {
		return err
	}
	_, err = openrouterrepo.New(tx).RemoveOpenRouterAPIKeyDisableCause(ctx, openrouterrepo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID:       organizationID,
		KeyType:              string(keyType),
		KeyHash:              key.KeyHash,
		DisableCause:         string(cause),
		MonthlyCredits:       key.MonthlyCredits,
		UpdateMonthlyCredits: false,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("OpenRouter %s key changed while removing %s lifecycle cause", keyType, cause)
	}
	if err != nil {
		return fmt.Errorf("persist OpenRouter %s key %s lifecycle cause removal: %w", keyType, cause, err)
	}
	return nil
}

func recoverPaygOpenRouterChatKeyTx(ctx context.Context, tx pgx.Tx, organizationID string) (bool, error) {
	key, err := getClassifiedOpenRouterKeyTx(ctx, tx, organizationID, openrouter.KeyTypeChat)
	if err != nil || key == nil {
		return false, err
	}
	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok || limit <= 0 {
		return false, errors.New("PAYG OpenRouter chat key credit policy is unavailable")
	}
	disableCauses := slices.DeleteFunc(slices.Clone(key.DisableCauses), func(cause string) bool {
		return cause == string(openrouter.DisableCauseBillingInactive)
	})
	stateChanged := len(disableCauses) != len(key.DisableCauses) || key.MonthlyCredits != int64(limit) || key.Disabled != (len(disableCauses) > 0)
	rows, err := repo.New(tx).RecoverPaygOpenRouterChatKey(ctx, repo.RecoverPaygOpenRouterChatKeyParams{
		MonthlyCredits: int64(limit),
		OrganizationID: organizationID,
		KeyHash:        key.KeyHash,
	})
	if err != nil {
		return false, fmt.Errorf("persist OpenRouter chat key PAYG recovery: %w", err)
	}
	if rows != 1 {
		return false, fmt.Errorf("OpenRouter chat key changed while recovering PAYG billing: updated %d rows", rows)
	}
	return stateChanged, nil
}

func (s *Service) deactivatePaygSubscription(ctx context.Context, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent) (stripeWebhookResult, error) {
	q := repo.New(tx)
	state, err := q.GetPaygActivationState(ctx, organizationID)
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("lock PAYG deactivation state: %w", err)
	}

	if !state.StripeCustomerID.Valid || state.StripeCustomerID.String != event.CustomerID ||
		state.GramAccountType != "payg" ||
		!state.StripeSubscriptionID.Valid || state.StripeSubscriptionID.String != event.ObjectID {
		return stripeWebhookResult{newlyEnabledFeatures: nil, reconcileKeyTypes: nil, invoicePaymentFailed: false, subscriptionLost: false}, nil
	}

	billingRows, err := q.DeactivatePaygBillingMetadata(ctx, repo.DeactivatePaygBillingMetadataParams{
		OrganizationID:       organizationID,
		StripeCustomerID:     pgtype.Text{String: event.CustomerID, Valid: true},
		StripeSubscriptionID: pgtype.Text{String: event.ObjectID, Valid: true},
	})
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("clear PAYG Stripe subscription: %w", err)
	}
	if billingRows != 1 {
		return stripeWebhookResult{}, fmt.Errorf("clear PAYG Stripe subscription: expected one row, updated %d", billingRows)
	}

	organizationRows, err := q.DeactivatePaygOrganization(ctx, organizationID)
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("deactivate PAYG organization: %w", err)
	}
	if organizationRows != 1 {
		return stripeWebhookResult{}, fmt.Errorf("deactivate PAYG organization: expected one row, updated %d", organizationRows)
	}
	if err := addOpenRouterDisableCauseTx(ctx, tx, organizationID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("record inactive PAYG billing for OpenRouter chat key: %w", err)
	}

	if s.auditLogger == nil {
		return stripeWebhookResult{}, errors.New("audit logger is unavailable")
	}
	if err := s.auditLogger.LogOrganizationPaygDeactivated(ctx, tx, audit.LogOrganizationPaygDeactivatedEvent{
		OrganizationID:   organizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName: nil,
		ActorSlug:        nil,
		OrganizationName: state.OrganizationName,
		OrganizationSlug: state.OrganizationSlug,
		OrganizationSnapshotBefore: &audit.OrganizationPaygActivationSnapshot{
			AccountType: state.GramAccountType,
			Whitelisted: state.Whitelisted,
		},
		OrganizationSnapshotAfter: &audit.OrganizationPaygActivationSnapshot{
			AccountType: "free",
			Whitelisted: false,
		},
	}); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("log PAYG organization deactivation: %w", err)
	}

	return stripeWebhookResult{
		newlyEnabledFeatures: nil,
		reconcileKeyTypes:    []openrouter.KeyType{openrouter.KeyTypeChat},
		invoicePaymentFailed: false,
		subscriptionLost:     true,
	}, nil
}

func (s *Service) recordStripeInvoice(ctx context.Context, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, invoice *stripeclient.InvoiceState) (bool, error) {
	if invoice == nil {
		return false, errors.New("missing current Stripe invoice state")
	}
	switch invoice.BillingReason {
	case "subscription_create", "subscription_cycle":
	default:
		return false, nil
	}

	q := repo.New(tx)
	identity, err := q.GetPaygInvoiceIdentity(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("read PAYG invoice identity: %w", err)
	}
	if !identity.StripeCustomerID.Valid || identity.StripeCustomerID.String != event.CustomerID {
		return false, errors.New("stripe invoice customer does not belong to the organization")
	}
	if identity.GramAccountType != "payg" {
		if identity.StripeCheckoutSessionID.Valid {
			return false, errors.New("PAYG Checkout activation has not committed")
		}
		return false, nil
	}
	if !identity.StripeSubscriptionID.Valid || identity.StripeSubscriptionID.String != invoice.SubscriptionID || !identity.StripeBillingCycleAnchor.Valid {
		return false, errors.New("stripe invoice does not belong to active PAYG billing")
	}
	if invoice.BillingReason == "subscription_create" && !invoice.ServicePeriodEnd.After(invoice.ServicePeriodStart) {
		// Stripe's immediate first subscription invoice has no prior service
		// period. It carries no usage and must not enter pass-through billing.
		return false, nil
	}
	if invoice.ServicePeriodStart.Before(identity.StripeBillingCycleAnchor.Time.UTC()) {
		// Checkout can create a zero-dollar free stub before the first paid
		// midnight. It is intentionally outside pass-through billing.
		return false, nil
	}
	if invoice.Currency != "usd" {
		return false, fmt.Errorf("unsupported Stripe invoice currency %q", invoice.Currency)
	}
	if !invoice.ServicePeriodStart.Equal(invoice.ServicePeriodStart.Truncate(24*time.Hour)) || !invoice.ServicePeriodEnd.Equal(invoice.ServicePeriodEnd.Truncate(24*time.Hour)) {
		return false, errors.New("stripe invoice service period is not UTC-day aligned")
	}

	_, err = q.UpsertStripeInvoice(ctx, repo.UpsertStripeInvoiceParams{
		StripeInvoiceID:      invoice.ID,
		OrganizationID:       pgtype.Text{String: organizationID, Valid: true},
		StripeCustomerID:     invoice.CustomerID,
		StripeSubscriptionID: invoice.SubscriptionID,
		ServicePeriodStart:   pgtype.Timestamptz{Time: invoice.ServicePeriodStart.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		ServicePeriodEnd:     pgtype.Timestamptz{Time: invoice.ServicePeriodEnd.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		InvoiceState:         invoice.Status,
		FinalizedAt:          pgtype.Timestamptz{Time: invoice.FinalizedAt.UTC(), InfinityModifier: pgtype.Finite, Valid: !invoice.FinalizedAt.IsZero()},
	})
	if err != nil {
		return false, fmt.Errorf("record Stripe invoice: %w", err)
	}
	return true, nil
}

func (s *Service) activatePaygCheckout(ctx context.Context, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
	if checkout == nil {
		return stripeWebhookResult{}, errors.New("missing current Stripe Checkout state")
	}

	q := repo.New(tx)
	state, err := q.GetPaygActivationState(ctx, organizationID)
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("lock PAYG activation state: %w", err)
	}
	if !state.StripeCustomerID.Valid || state.StripeCustomerID.String != event.CustomerID {
		return stripeWebhookResult{}, errors.New("stripe customer does not own the organization's billing metadata")
	}
	if state.StripeSubscriptionID.Valid && state.StripeSubscriptionID.String != event.SubscriptionID {
		return stripeWebhookResult{}, errors.New("organization already belongs to another Stripe subscription")
	}

	owners, err := q.ListStripeSubscriptionOwners(ctx, pgtype.Text{String: event.SubscriptionID, Valid: true})
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("check Stripe subscription ownership: %w", err)
	}
	switch {
	case len(owners) > 1:
		return stripeWebhookResult{}, errors.New("stripe subscription belongs to multiple organizations")
	case len(owners) == 1 && owners[0] != organizationID:
		return stripeWebhookResult{}, errors.New("stripe subscription already belongs to another organization")
	}

	newlyEnabled := []productfeatures.Feature(nil)
	convertedDemotedTrial := false
	trial, err := trialsrepo.New(tx).GetTrial(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		newlyEnabled, err = productfeatures.SeedPaygEntitlementsTx(ctx, tx, organizationID)
		if err != nil {
			return stripeWebhookResult{}, fmt.Errorf("seed PAYG entitlements: %w", err)
		}
	} else if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("read trial state for PAYG activation: %w", err)
	} else if trial.DemotedAt.Valid {
		if !trial.ConvertedAt.Valid {
			return stripeWebhookResult{}, errors.New("demoted enterprise trial is not durably converted")
		}
		convertedDemotedTrial = true
		if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, organizationID, true); err != nil {
			return stripeWebhookResult{}, fmt.Errorf("restore demoted trial runtime features: %w", err)
		}
		newlyEnabled = append(newlyEnabled, productfeatures.TrialRuntimeFeatures...)
	}

	reconcileKeyTypes := []openrouter.KeyType(nil)
	if convertedDemotedTrial {
		reconcileKeyTypes = append(reconcileKeyTypes, openrouter.AllKeyTypes...)
		for _, keyType := range openrouter.AllKeyTypes {
			if err := removeOpenRouterDisableCauseTx(ctx, tx, organizationID, keyType, openrouter.DisableCauseTrialDemotion); err != nil {
				return stripeWebhookResult{}, fmt.Errorf("replace demoted trial OpenRouter %s key lifecycle: %w", keyType, err)
			}
		}
	}
	chatStateChanged, err := recoverPaygOpenRouterChatKeyTx(ctx, tx, organizationID)
	if err != nil {
		return stripeWebhookResult{}, fmt.Errorf("recover PAYG OpenRouter chat key billing: %w", err)
	}
	if chatStateChanged && !convertedDemotedTrial {
		reconcileKeyTypes = append(reconcileKeyTypes, openrouter.KeyTypeChat)
	}

	anchorDay := conv.SafeInt32(checkout.BillingCycleAnchor.UTC().Day())
	alreadyActivated := state.StripeSubscriptionID.Valid &&
		state.StripeSubscriptionID.String == event.SubscriptionID &&
		state.StripeBillingCycleAnchor.Valid &&
		state.StripeBillingCycleAnchor.Time.Equal(checkout.BillingCycleAnchor) &&
		state.BillingCycleAnchorDay == anchorDay &&
		state.GramAccountType == "payg" && state.Whitelisted
	if alreadyActivated {
		return stripeWebhookResult{
			newlyEnabledFeatures: newlyEnabled,
			reconcileKeyTypes:    reconcileKeyTypes,
			invoicePaymentFailed: false,
			subscriptionLost:     false,
		}, nil
	}

	if _, err := q.ActivatePaygBillingMetadata(ctx, repo.ActivatePaygBillingMetadataParams{
		StripeSubscriptionID:     pgtype.Text{String: event.SubscriptionID, Valid: true},
		StripeBillingCycleAnchor: pgtype.Timestamptz{Time: checkout.BillingCycleAnchor.UTC(), InfinityModifier: pgtype.Finite, Valid: true},
		BillingCycleAnchorDay:    anchorDay,
		OrganizationID:           organizationID,
		StripeCustomerID:         pgtype.Text{String: event.CustomerID, Valid: true},
	}); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("store PAYG Stripe subscription: %w", err)
	}
	if err := q.ActivatePaygOrganization(ctx, organizationID); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("activate PAYG organization: %w", err)
	}

	if s.auditLogger == nil {
		return stripeWebhookResult{}, errors.New("audit logger is unavailable")
	}
	if err := s.auditLogger.LogOrganizationPaygActivated(ctx, tx, audit.LogOrganizationPaygActivatedEvent{
		OrganizationID:   organizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName: nil,
		ActorSlug:        nil,
		OrganizationName: state.OrganizationName,
		OrganizationSlug: state.OrganizationSlug,
		OrganizationSnapshotBefore: &audit.OrganizationPaygActivationSnapshot{
			AccountType: state.GramAccountType,
			Whitelisted: state.Whitelisted,
		},
		OrganizationSnapshotAfter: &audit.OrganizationPaygActivationSnapshot{
			AccountType: "payg",
			Whitelisted: true,
		},
	}); err != nil {
		return stripeWebhookResult{}, fmt.Errorf("log PAYG organization activation: %w", err)
	}

	return stripeWebhookResult{
		newlyEnabledFeatures: newlyEnabled,
		reconcileKeyTypes:    reconcileKeyTypes,
		invoicePaymentFailed: false,
		subscriptionLost:     false,
	}, nil
}
