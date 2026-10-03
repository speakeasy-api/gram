package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	usagegen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func (s *Service) GetInferenceKeys(ctx context.Context, payload *gen.GetInferenceKeysPayload) ([]*gen.AdminInferenceKey, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	keyTypes := make([]string, len(openrouter.AllKeyTypes))
	for index, keyType := range openrouter.AllKeyTypes {
		keyTypes[index] = string(keyType)
	}
	keys, err := usagerepo.New(s.db).ListMaterializedOpenRouterInferenceKeys(ctx, usagerepo.ListMaterializedOpenRouterInferenceKeysParams{
		OrganizationID: organizationID,
		KeyTypes:       keyTypes,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list platform-managed inference keys").LogError(ctx, s.logger)
	}

	if len(keys) == 0 {
		return []*gen.AdminInferenceKey{}, nil
	}

	result := make([]*gen.AdminInferenceKey, len(keys))
	group, groupCtx := errgroup.WithContext(ctx)
	for index, key := range keys {
		keyType := openrouter.KeyType(key.KeyType)
		if err := keyType.Validate(); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate stored inference key type").LogError(ctx, s.logger)
		}
		group.Go(func() error {
			creditsUsed, _, err := s.openRouterUsage.GetCreditsUsed(groupCtx, organizationID, keyType)
			if err != nil {
				return fmt.Errorf("read %s inference key usage: %w", keyType, err)
			}
			result[index] = &gen.AdminInferenceKey{
				KeyType:                 key.KeyType,
				CreditsUsed:             creditsUsed,
				MonthlyCredits:          key.MonthlyCredits,
				Disabled:                key.Disabled,
				DisableCauses:           key.DisableCauses,
				DisableCausesClassified: key.DisableCausesClassified,
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		if errors.Is(err, ErrOpenRouterUnavailable) {
			return nil, oops.E(oops.CodeUnavailable, err, "OpenRouter usage is temporarily unavailable").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "read OpenRouter inference key usage").LogError(ctx, s.logger)
	}

	return result, nil
}

func (s *Service) SetInferenceKeyMonthlyLimit(ctx context.Context, payload *gen.SetInferenceKeyMonthlyLimitPayload) (*gen.AdminInferenceKeyLimit, error) {
	keyType := openrouter.KeyType(payload.KeyType)
	if payload.KeyType == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "key_type is required").LogWarn(ctx, s.logger)
	}
	if err := keyType.Validate(); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid inference key type").LogWarn(ctx, s.logger)
	}
	if payload.MonthlyCredits < constants.MinimumPaygSpendCapUSD || payload.MonthlyCredits > constants.MaximumPaygSpendCapUSD {
		return nil, oops.E(oops.CodeInvalid, nil, "monthly_credits must be between %d and %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD).LogWarn(ctx, s.logger)
	}

	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	key, err := usagerepo.New(s.db).GetMaterializedOpenRouterInferenceKey(ctx, usagerepo.GetMaterializedOpenRouterInferenceKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "inference key is not available")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load inference key before setting monthly limit").LogError(ctx, s.logger)
	case key.Disabled:
		return nil, oops.E(oops.CodeConflict, nil, "the inference key is disabled")
	}

	actor, actorDisplayName, _ := adminActor(ctx)
	monthlyCredits, err := s.openRouterSpendCap.SetAdminOpenRouterSpendCap(
		ctx,
		uuid.NewString(),
		organizationID,
		keyType,
		payload.MonthlyCredits,
		actor,
		actorDisplayName,
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set inference key monthly limit").LogError(ctx, s.logger)
	}

	return &gen.AdminInferenceKeyLimit{KeyType: string(keyType), MonthlyCredits: int64(monthlyCredits)}, nil
}

func (s *Service) GetInferenceSpendHistory(ctx context.Context, payload *gen.GetInferenceSpendHistoryPayload) ([]*gen.AdminInferenceSpendMonth, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	months, err := usagerepo.New(s.db).ListOpenRouterInferenceSpendByMonth(ctx, usagerepo.ListOpenRouterInferenceSpendByMonthParams{
		OrganizationID:   organizationID,
		BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
		CompletedBefore:  conv.ToPGTimestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list inference spend history").LogError(ctx, s.logger)
	}

	result := make([]*gen.AdminInferenceSpendMonth, len(months))
	for index, month := range months {
		result[index] = &gen.AdminInferenceSpendMonth{
			PeriodStart: month.PeriodStart,
			PeriodEnd:   month.PeriodEnd,
			SpendUsd:    month.SpendUsd,
		}
	}
	return result, nil
}

func (s *Service) GetPaygBillingSummary(ctx context.Context, payload *gen.GetPaygBillingSummaryPayload) (*gen.AdminPaygBillingSummary, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	summary, err := s.billing.GetPaygBillingSummaryForOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get PAYG billing summary: %w", err)
	}
	return &gen.AdminPaygBillingSummary{
		PeriodStart: summary.PeriodStart, PeriodEnd: summary.PeriodEnd, TumTokens: summary.TumTokens,
		TumUnitPriceUsd: summary.TumUnitPriceUsd, TumCostUsd: summary.TumCostUsd,
		OtherInferenceSpendUsd: summary.OtherInferenceSpendUsd, RecordedThrough: summary.RecordedThrough,
		EstimatedTotalUsd: summary.EstimatedTotalUsd,
	}, nil
}

func (s *Service) GetMeterUsage(ctx context.Context, payload *gen.GetMeterUsagePayload) (*gen.AdminMeterUsageResponse, error) {
	organization, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        payload.OrganizationID,
		AllowSlug: true,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve meter usage organization").LogError(ctx, s.logger)
	}

	report, err := s.billing.GetMeterUsageForOrganization(ctx, organization.ID, &usagegen.GetMeterUsagePayload{
		SessionToken: nil,
		Family:       payload.Family,
		From:         payload.From,
		To:           payload.To,
		Breakdown:    conv.PtrEmpty("total"),
	})
	if err != nil {
		return nil, fmt.Errorf("get organization meter usage: %w", err)
	}

	billingCycles := make([]*gen.MeterUsageWindow, len(report.BillingCycles))
	for index, cycle := range report.BillingCycles {
		billingCycles[index] = &gen.MeterUsageWindow{From: cycle.From, To: cycle.To}
	}
	buckets := make([]*gen.AdminMeterUsageBucket, len(report.Buckets))
	for index, bucket := range report.Buckets {
		buckets[index] = &gen.AdminMeterUsageBucket{From: bucket.From, To: bucket.To, Total: bucket.Total}
	}
	return &gen.AdminMeterUsageResponse{
		Family: report.Family,
		Window: &gen.MeterUsageWindow{
			From: report.Window.From,
			To:   report.Window.To,
		},
		BillingCycles:     billingCycles,
		Unit:              report.Unit,
		Total:             report.Total,
		Buckets:           buckets,
		QueriedAt:         report.QueriedAt,
		MeasurementMethod: report.MeasurementMethod,
	}, nil
}

func (s *Service) GetSpendBreakdown(ctx context.Context, payload *gen.GetSpendBreakdownPayload) (*gen.AdminSpendBreakdownResponse, error) {
	organization, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        payload.OrganizationID,
		AllowSlug: true,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve spend breakdown organization").LogError(ctx, s.logger)
	}

	report, err := s.billing.GetSpendBreakdownForOrganization(ctx, organization.ID, &usagegen.GetSpendBreakdownPayload{
		SessionToken: nil,
		From:         payload.From,
		To:           payload.To,
	})
	if err != nil {
		return nil, fmt.Errorf("get organization spend breakdown: %w", err)
	}

	billingCycles := make([]*gen.MeterUsageWindow, len(report.BillingCycles))
	for index, cycle := range report.BillingCycles {
		billingCycles[index] = &gen.MeterUsageWindow{From: cycle.From, To: cycle.To}
	}
	products := make([]*gen.SpendProduct, len(report.Products))
	for productIndex, product := range report.Products {
		buckets := make([]*gen.SpendBucket, len(product.Buckets))
		for bucketIndex, bucket := range product.Buckets {
			buckets[bucketIndex] = &gen.SpendBucket{
				From: bucket.From, To: bucket.To, Quantity: bucket.Quantity, CostUsd: bucket.CostUsd,
			}
		}
		products[productIndex] = &gen.SpendProduct{
			ID: product.ID, Label: product.Label, Unit: product.Unit, Quantity: product.Quantity,
			RateQuantity: product.RateQuantity, RateUsd: product.RateUsd, CostUsd: product.CostUsd, Buckets: buckets,
		}
	}
	return &gen.AdminSpendBreakdownResponse{
		Window: &gen.MeterUsageWindow{
			From: report.Window.From,
			To:   report.Window.To,
		},
		BillingCycles: billingCycles,
		Currency:      report.Currency,
		PricingBasis:  report.PricingBasis,
		QueriedAt:     report.QueriedAt,
		TotalCostUsd:  report.TotalCostUsd,
		Products:      products,
	}, nil
}

var stripeCustomerIDPattern = regexp.MustCompile(`^cus_[A-Za-z0-9_]+$`)

var stripeSubscriptionIDPattern = regexp.MustCompile(`^sub_[A-Za-z0-9_]+$`)

func (s *Service) GetStripeCustomer(ctx context.Context, payload *gen.GetStripeCustomerPayload) (*gen.AdminStripeCustomer, error) {
	if err := validateStripeCustomerID(payload.StripeCustomerID); err != nil {
		return nil, err
	}
	if _, err := s.stripeCustomerAssignmentOrganization(ctx, payload.OrganizationID); err != nil {
		return nil, err
	}

	customer, err := s.billing.GetStripeCustomer(ctx, payload.StripeCustomerID)
	if err != nil {
		return nil, fmt.Errorf("get Stripe customer: %w", err)
	}

	return &gen.AdminStripeCustomer{
		ID:          customer.ID,
		Name:        conv.PtrEmpty(customer.Name),
		Email:       conv.PtrEmpty(customer.Email),
		Description: conv.PtrEmpty(customer.Description),
		Livemode:    customer.LiveMode,
	}, nil
}

func (s *Service) SetStripeCustomer(ctx context.Context, payload *gen.SetStripeCustomerPayload) (*gen.AdminOrganization, error) {
	if err := validateStripeCustomerID(payload.StripeCustomerID); err != nil {
		return nil, err
	}

	eligibleOrganization, err := s.stripeCustomerAssignmentOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if _, err := s.billing.GetStripeCustomer(ctx, payload.StripeCustomerID); err != nil {
		return nil, fmt.Errorf("get Stripe customer before assignment: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin Stripe customer transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	_, err = queries.AdminSetStripeCustomer(ctx, repo.AdminSetStripeCustomerParams{
		OrganizationID:   eligibleOrganization.ID,
		StripeCustomerID: payload.StripeCustomerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, nil, "organization already has Stripe billing identity")
	}
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) &&
		pgErr.Code == pgerrcode.UniqueViolation &&
		pgErr.ConstraintName == "billing_metadata_stripe_customer_id_key":
		return nil, oops.E(oops.CodeConflict, nil, "Stripe customer is already assigned")
	case errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation:
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "set Stripe customer").LogError(ctx, s.logger)
	}

	organization, err := queries.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        eligibleOrganization.ID,
		AllowSlug: false,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization after setting Stripe customer").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit Stripe customer transaction").LogError(ctx, s.logger)
	}

	return s.adminOrganizationFromGetRow(organization), nil
}

func (s *Service) GetStripeSubscriptionCandidate(ctx context.Context, payload *gen.GetStripeSubscriptionCandidatePayload) (*gen.AdminStripeSubscriptionCandidate, error) {
	_, state, err := s.verifiedStripeSubscription(ctx, payload.OrganizationID, payload.StripeSubscriptionID)
	if err != nil {
		return nil, err
	}
	return &gen.AdminStripeSubscriptionCandidate{
		ID:         state.ID,
		CustomerID: state.CustomerID,
		Status:     state.Status,
	}, nil
}

func (s *Service) SetStripeSubscription(ctx context.Context, payload *gen.SetStripeSubscriptionPayload) (*gen.AdminOrganization, error) {
	organization, state, err := s.verifiedStripeSubscription(ctx, payload.OrganizationID, payload.StripeSubscriptionID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin Stripe subscription transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.AdminAcquireStripeSubscriptionLock(ctx, state.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock Stripe subscription").LogError(ctx, s.logger)
	}
	// The advisory lock only serializes database assignments. Re-check the
	// organization on this transaction and the live Stripe subscription so
	// a deletion in the meantime is not recorded.
	organization, err = queries.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        organization.ID,
		AllowSlug: false,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization after locking Stripe subscription").LogError(ctx, s.logger)
	}
	if err := requirePaygSubscriptionAssignment(organization); err != nil {
		return nil, err
	}
	state, err = s.liveStripeSubscriptionForOrganization(ctx, organization, state.ID)
	if err != nil {
		return nil, err
	}
	_, err = queries.AdminSetStripeSubscription(ctx, repo.AdminSetStripeSubscriptionParams{
		StripeSubscriptionID:     state.ID,
		StripeBillingCycleAnchor: conv.ToPGTimestamptz(state.BillingCycleAnchor.UTC()),
		BillingCycleAnchorDay:    conv.SafeInt32(state.BillingCycleAnchor.UTC().Day()),
		OrganizationID:           organization.ID,
		StripeCustomerID:         organization.StripeCustomerID.String,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, nil, "organization cannot accept this Stripe subscription")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set Stripe subscription").LogError(ctx, s.logger)
	}

	updated, err := queries.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        organization.ID,
		AllowSlug: false,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization after setting Stripe subscription").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit Stripe subscription transaction").LogError(ctx, s.logger)
	}
	return s.adminOrganizationFromGetRow(updated), nil
}

func (s *Service) verifiedStripeSubscription(ctx context.Context, organizationID, subscriptionID string) (repo.AdminGetOrganizationRow, *stripeclient.SubscriptionState, error) {
	if err := validateStripeSubscriptionID(subscriptionID); err != nil {
		return repo.AdminGetOrganizationRow{}, nil, err
	}
	organization, err := s.paygSubscriptionAssignmentOrganization(ctx, organizationID)
	if err != nil {
		return repo.AdminGetOrganizationRow{}, nil, err
	}
	state, err := s.liveStripeSubscriptionForOrganization(ctx, organization, subscriptionID)
	if err != nil {
		return repo.AdminGetOrganizationRow{}, nil, err
	}
	return organization, state, nil
}

func (s *Service) liveStripeSubscriptionForOrganization(ctx context.Context, organization repo.AdminGetOrganizationRow, subscriptionID string) (*stripeclient.SubscriptionState, error) {
	state, err := s.billing.GetStripeSubscriptionByID(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("get Stripe subscription: %w", err)
	}
	if state.CustomerID != organization.StripeCustomerID.String || state.ID != subscriptionID {
		return nil, oops.E(oops.CodeConflict, nil, "Stripe subscription does not belong to the organization's Stripe customer")
	}
	if state.BillingCycleAnchor.IsZero() {
		return nil, oops.E(oops.CodeConflict, nil, "Stripe subscription is missing a billing cycle anchor")
	}
	return state, nil
}

func (s *Service) paygSubscriptionAssignmentOrganization(ctx context.Context, organizationID string) (repo.AdminGetOrganizationRow, error) {
	organization, err := s.canonicalBillingOrganization(ctx, organizationID)
	if err != nil {
		return repo.AdminGetOrganizationRow{}, err
	}
	if err := requirePaygSubscriptionAssignment(organization); err != nil {
		return repo.AdminGetOrganizationRow{}, err
	}
	return organization, nil
}

func requirePaygSubscriptionAssignment(organization repo.AdminGetOrganizationRow) error {
	if organization.AccountType != string(billing.TierPayg) {
		return oops.E(oops.CodeConflict, nil, "subscription ID can only be set for a PAYG organization")
	}
	if !organization.StripeCustomerID.Valid || organization.StripeCustomerID.String == "" {
		return oops.E(oops.CodeConflict, nil, "set a Stripe customer ID before setting a subscription ID")
	}
	if organization.StripeSubscriptionID.Valid && organization.StripeSubscriptionID.String != "" {
		return oops.E(oops.CodeConflict, nil, "organization already has a Stripe subscription")
	}
	return nil
}

func (s *Service) GetStripeSubscription(ctx context.Context, payload *gen.GetStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	subscription, err := s.billing.GetStripeSubscriptionForOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get Stripe subscription: %w", err)
	}
	return adminStripeSubscription(subscription), nil
}

func (s *Service) CancelStripeSubscription(ctx context.Context, payload *gen.CancelStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, payload.OrganizationID, true)
}

func (s *Service) ResumeStripeSubscription(ctx context.Context, payload *gen.ResumeStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, payload.OrganizationID, false)
}

func (s *Service) setStripeSubscriptionCancelAtPeriodEnd(ctx context.Context, requestedOrganizationID string, cancelAtPeriodEnd bool) (*gen.AdminStripeSubscription, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, requestedOrganizationID)
	if err != nil {
		return nil, err
	}
	actor, actorDisplayName, _ := adminActor(ctx)
	subscription, err := s.billing.SetStripeSubscriptionCancelAtPeriodEndForOrganization(ctx, organizationID, usage.BillingActor{
		Principal: actor, DisplayName: actorDisplayName,
	}, cancelAtPeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("update Stripe subscription: %w", err)
	}
	return adminStripeSubscription(subscription), nil
}

func validateStripeCustomerID(customerID string) error {
	if len(customerID) > 255 || !stripeCustomerIDPattern.MatchString(customerID) {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe customer ID")
	}
	return nil
}

func validateStripeSubscriptionID(subscriptionID string) error {
	if len(subscriptionID) > 255 || !stripeSubscriptionIDPattern.MatchString(subscriptionID) {
		return oops.E(oops.CodeBadRequest, nil, "invalid Stripe subscription ID")
	}
	return nil
}

func (s *Service) stripeCustomerAssignmentOrganization(ctx context.Context, organizationID string) (repo.AdminGetOrganizationRow, error) {
	organization, err := s.canonicalBillingOrganization(ctx, organizationID)
	if err != nil {
		return repo.AdminGetOrganizationRow{}, err
	}
	if organization.StripeCustomerID.Valid || organization.StripeSubscriptionID.Valid {
		return repo.AdminGetOrganizationRow{}, oops.E(oops.CodeConflict, nil, "organization already has Stripe billing identity")
	}
	return organization, nil
}

func (s *Service) canonicalBillingOrganization(ctx context.Context, organizationID string) (repo.AdminGetOrganizationRow, error) {
	return s.canonicalAdminOrganization(ctx, organizationID)
}

func (s *Service) canonicalBillingOrganizationID(ctx context.Context, organizationID string) (string, error) {
	organization, err := s.canonicalBillingOrganization(ctx, organizationID)
	if err != nil {
		return "", err
	}
	return organization.ID, nil
}

func (s *Service) canonicalAdminOrganizationID(ctx context.Context, organizationID string) (string, error) {
	organization, err := s.canonicalAdminOrganization(ctx, organizationID)
	if err != nil {
		return "", err
	}
	return organization.ID, nil
}

func (s *Service) canonicalAdminOrganization(ctx context.Context, organizationID string) (repo.AdminGetOrganizationRow, error) {
	organization, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.AdminGetOrganizationRow{}, oops.C(oops.CodeNotFound)
	case err != nil:
		return repo.AdminGetOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "resolve admin organization").LogError(ctx, s.logger)
	default:
		return organization, nil
	}
}

func adminStripeSubscription(value *usage.StripeSubscription) *gen.AdminStripeSubscription {
	return &gen.AdminStripeSubscription{
		Status: value.Status, CurrentPeriodStart: value.CurrentPeriodStart, CurrentPeriodEnd: value.CurrentPeriodEnd,
		TrialStart: value.TrialStart, TrialEnd: value.TrialEnd, CancelAtPeriodEnd: value.CancelAtPeriodEnd,
		CancelAt: value.CancelAt, CanceledAt: value.CanceledAt, PaymentFailed: value.PaymentFailed,
	}
}
