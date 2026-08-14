package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

const (
	openRouterInvoiceFreezeDelay      = 48 * time.Hour
	openRouterInvoiceObservationDelay = 72 * time.Hour
	stripeAllocationRetryWindow       = 24 * time.Hour
	stripeAllocationClaimLease        = 5 * time.Minute

	stripeAllocationSourceOpenRouter = "openrouter_daily_spend"
	stripeAllocationSourceTUM        = "tum_cycle"
)

// SettleStripeInvoiceAllocationsArgs fixes the observation time for one daily
// settlement pass.
type SettleStripeInvoiceAllocationsArgs struct {
	Now time.Time
	// RestrictOpenRouterToReadyOrganizations is explicit so an empty list from
	// Temporal means "freeze no chat spend", never "freeze every org".
	RestrictOpenRouterToReadyOrganizations bool
	OpenRouterReadyOrganizationIDs         []string
}

// SettleStripeInvoiceAllocations freezes OpenRouter charges, attaches TUM
// carries, and delivers at most one claimed allocation per organization.
type SettleStripeInvoiceAllocations struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	stripeClient stripeInvoiceAllocationClient
}

type stripeInvoiceAllocationClient interface {
	GetInvoice(context.Context, string) (*stripeclient.InvoiceState, error)
	CreateInvoiceItem(context.Context, stripeclient.CreateInvoiceItemInput) (*stripeclient.InvoiceItem, error)
	CreateCreditNote(context.Context, stripeclient.CreateCreditNoteInput) (*stripeclient.CreditNote, error)
	FindInvoiceItem(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.InvoiceItem, error)
	FindCreditNote(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.CreditNote, error)
}

func NewSettleStripeInvoiceAllocations(
	logger *slog.Logger,
	db *pgxpool.Pool,
	stripeClient stripeInvoiceAllocationClient,
) *SettleStripeInvoiceAllocations {
	return &SettleStripeInvoiceAllocations{
		logger:       logger.With(attr.SlogComponent("settle_stripe_invoice_allocations")),
		db:           db,
		stripeClient: stripeClient,
	}
}

func (s *SettleStripeInvoiceAllocations) Do(ctx context.Context, args SettleStripeInvoiceAllocationsArgs) error {
	now := args.Now.UTC()
	if now.IsZero() {
		return errors.New("settle Stripe invoice allocations: current time is required")
	}

	queries := repo.New(s.db)
	organizations, err := queries.ListStripeInvoiceBillingOrganizations(ctx, timestamptz(now))
	if err != nil {
		return fmt.Errorf("list Stripe invoice billing organizations: %w", err)
	}
	ready := make(map[string]struct{}, len(args.OpenRouterReadyOrganizationIDs))
	for _, organizationID := range args.OpenRouterReadyOrganizationIDs {
		ready[organizationID] = struct{}{}
	}

	var failures []error
	for _, organizationID := range organizations {
		_, openRouterReady := ready[organizationID]
		if err := s.settleOrganization(ctx, queries, organizationID, now,
			!args.RestrictOpenRouterToReadyOrganizations || openRouterReady); err != nil {
			s.logger.ErrorContext(ctx, "settle Stripe invoice allocations",
				attr.SlogOrganizationID(organizationID),
				attr.SlogError(err),
			)
			failures = append(failures, fmt.Errorf("settle organization %s: %w", organizationID, err))
		}
	}

	return errors.Join(failures...)
}

func (s *SettleStripeInvoiceAllocations) settleOrganization(
	ctx context.Context,
	queries *repo.Queries,
	organizationID string,
	now time.Time,
	freezeOpenRouter bool,
) error {
	organizationIDParam := pgtype.Text{String: organizationID, Valid: true}
	invoices, err := queries.ListStripeInvoicesForOpenRouterBilling(ctx, repo.ListStripeInvoicesForOpenRouterBillingParams{
		OrganizationID: organizationIDParam,
		Now:            timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("list invoices: %w", err)
	}

	if freezeOpenRouter {
		for _, invoice := range invoices {
			if err := s.freezeInvoice(ctx, queries, organizationIDParam, invoice, now); err != nil {
				return err
			}
		}
	}

	if _, err := queries.AttachTUMCarryToOriginalInvoice(ctx, organizationIDParam); err != nil {
		return fmt.Errorf("attach TUM carry to original invoice: %w", err)
	}
	if _, err := queries.AssignPositiveCarryToStripeInvoice(ctx, organizationIDParam); err != nil {
		return fmt.Errorf("assign positive carry to draft invoice: %w", err)
	}

	claimed, err := queries.ClaimNextStripeInvoiceAllocation(ctx, repo.ClaimNextStripeInvoiceAllocationParams{
		OrganizationID: organizationIDParam,
		LeaseBefore:    timestamptz(now.Add(-stripeAllocationClaimLease)),
		AttemptedAt:    timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim invoice allocation: %w", err)
	}

	return s.deliverClaim(ctx, queries, claimed, now)
}

func (s *SettleStripeInvoiceAllocations) freezeInvoice(
	ctx context.Context,
	queries *repo.Queries,
	organizationID pgtype.Text,
	invoice repo.ListStripeInvoicesForOpenRouterBillingRow,
	now time.Time,
) error {
	cycleEnd := invoice.ServicePeriodEnd.Time.UTC()
	if invoice.ServicePeriodEnd.Valid && !now.Before(cycleEnd.Add(openRouterInvoiceFreezeDelay)) {
		days, err := queries.ListOpenRouterInvoiceSourceDays(ctx, repo.ListOpenRouterInvoiceSourceDaysParams{
			OrganizationID:  organizationID,
			StripeInvoiceID: invoice.StripeInvoiceID,
			Now:             timestamptz(now),
		})
		if err != nil {
			return fmt.Errorf("list source days for invoice %s: %w", invoice.StripeInvoiceID, err)
		}

		missedFreeze := !now.Before(cycleEnd.Add(openRouterInvoiceObservationDelay))
		for _, day := range days {
			snapshot := day.SpendUsd
			if missedFreeze {
				snapshot = numericFromCents(0)
			}
			cents, err := roundNumericToCents(snapshot)
			if err != nil {
				return fmt.Errorf("round baseline for %s: %w", day.SourceDay.Time.Format(time.DateOnly), err)
			}

			destination := pgtype.Text{String: "", Valid: false}
			if cents != 0 && invoice.InvoiceState == "draft" {
				destination = pgtype.Text{String: invoice.StripeInvoiceID, Valid: true}
			}
			if _, err := queries.CreateOpenRouterInvoiceAllocation(ctx, openRouterAllocationParams(
				organizationID,
				day.SourceDay,
				1,
				snapshot,
				cents,
				invoice.StripeInvoiceID,
				destination,
				now,
			)); err != nil {
				return fmt.Errorf("freeze baseline for %s: %w", day.SourceDay.Time.Format(time.DateOnly), err)
			}
		}
	}

	if !invoice.ServicePeriodEnd.Valid || now.Before(cycleEnd.Add(openRouterInvoiceObservationDelay)) {
		return nil
	}

	baselines, err := queries.ListOpenRouterInvoiceBaselines(ctx, repo.ListOpenRouterInvoiceBaselinesParams{
		OrganizationID:  organizationID,
		StripeInvoiceID: invoice.StripeInvoiceID,
		Now:             timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("list final source snapshots for invoice %s: %w", invoice.StripeInvoiceID, err)
	}
	for _, baseline := range baselines {
		frozenCents, err := roundNumericToCents(baseline.SourceSnapshotUsd)
		if err != nil {
			return fmt.Errorf("round frozen snapshot for %s: %w", baseline.SourceKey, err)
		}
		finalCents, err := roundNumericToCents(baseline.FinalSpendUsd)
		if err != nil {
			return fmt.Errorf("round final snapshot for %s: %w", baseline.SourceKey, err)
		}
		deltaCents := finalCents - frozenCents
		destination := pgtype.Text{String: "", Valid: false}
		if deltaCents < 0 {
			destination = pgtype.Text{String: invoice.StripeInvoiceID, Valid: true}
		}
		if _, err := queries.CreateOpenRouterInvoiceAllocation(ctx, openRouterAllocationParams(
			organizationID,
			baseline.SourceDay,
			2,
			baseline.FinalSpendUsd,
			deltaCents,
			invoice.StripeInvoiceID,
			destination,
			now,
		)); err != nil {
			return fmt.Errorf("freeze carry for %s: %w", baseline.SourceKey, err)
		}
	}

	return nil
}

func openRouterAllocationParams(
	organizationID pgtype.Text,
	day pgtype.Date,
	seq int32,
	snapshot pgtype.Numeric,
	cents int64,
	originalInvoiceID string,
	destinationInvoiceID pgtype.Text,
	now time.Time,
) repo.CreateOpenRouterInvoiceAllocationParams {
	deliveryState := "pending"
	confirmedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if cents == 0 {
		deliveryState = "confirmed"
		confirmedAt = timestamptz(now)
	}
	dayText := day.Time.UTC().Format(time.DateOnly)
	return repo.CreateOpenRouterInvoiceAllocationParams{
		OrganizationID:       organizationID,
		SourceKey:            dayText + ":chat",
		Seq:                  seq,
		SourceDay:            day,
		SourceSnapshotUsd:    snapshot,
		AmountUsd:            numericFromCents(cents),
		OriginalInvoiceID:    pgtype.Text{String: originalInvoiceID, Valid: true},
		DestinationInvoiceID: destinationInvoiceID,
		IdempotencyKey:       fmt.Sprintf("openrouter:%s:%s:%d", organizationID.String, dayText, seq),
		DeliveryState:        deliveryState,
		ConfirmedAt:          confirmedAt,
	}
}

func (s *SettleStripeInvoiceAllocations) deliverClaim(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	now time.Time,
) error {
	cents, err := roundNumericToCents(claim.AmountUsd)
	if err != nil {
		return fmt.Errorf("read claimed allocation amount: %w", err)
	}
	if cents == 0 {
		return errors.New("claimed zero-dollar allocation")
	}

	reconcile := claim.PreviousFirstAttemptedAt.Valid &&
		!now.Before(claim.PreviousFirstAttemptedAt.Time.UTC().Add(stripeAllocationRetryWindow))
	reconciledAbsent := false
	if reconcile {
		found, err := s.findAllocation(ctx, claim, cents)
		if err != nil {
			s.markAmbiguous(ctx, queries, claim, now)
			return err
		}
		if found != "" {
			return s.confirmAllocation(ctx, queries, claim, found, cents < 0, true, now)
		}
		rotatedKey, err := reconcileAndRotateAllocation(ctx, queries, claim, now)
		if err != nil {
			return err
		}
		claim.IdempotencyKey = rotatedKey
		reconciledAbsent = true
	}

	// A credit-note retry cannot recompute credit_amount inside Stripe's
	// idempotency window: invoice payment may have changed since the first
	// request. Wait for metadata reconciliation, then create from current state
	// after the key's retention window.
	if cents < 0 && claim.PreviousFirstAttemptedAt.Valid && !reconcile {
		return nil
	}

	invoice, err := s.stripeClient.GetInvoice(ctx, claim.DestinationInvoiceID.String)
	if err != nil {
		s.markAmbiguous(ctx, queries, claim, now)
		return fmt.Errorf("retrieve destination invoice: %w", err)
	}
	if err := validateClaimInvoice(claim, invoice); err != nil {
		s.markAmbiguous(ctx, queries, claim, now)
		return err
	}
	if err := updateClaimInvoiceState(ctx, queries, claim, invoice); err != nil {
		return err
	}

	if cents > 0 {
		if invoice.Status != "draft" {
			// A destination that closes after an ambiguous write cannot be
			// rebound on one eventually-consistent list response. Hold it until
			// the attempt's 24-hour reconciliation boundary.
			if claim.PreviousFirstAttemptedAt.Valid && !reconciledAbsent {
				return nil
			}
			updated, err := queries.UnassignPositiveStripeInvoiceAllocation(ctx, repo.UnassignPositiveStripeInvoiceAllocationParams{
				OrganizationID: pgtype.Text{String: claim.OrganizationID, Valid: true},
				ID:             claim.ID,
				AttemptedAt:    timestamptz(now),
			})
			if err != nil {
				return fmt.Errorf("unassign closed destination invoice: %w", err)
			}
			if updated != 1 {
				return errors.New("unassign closed destination invoice: claim lost")
			}
			return nil
		}

		periodStart, periodEnd, description, err := allocationPeriodAndDescription(claim)
		if err != nil {
			return err
		}
		item, err := s.stripeClient.CreateInvoiceItem(ctx, stripeclient.CreateInvoiceItemInput{
			CustomerID:     claim.StripeCustomerID,
			SubscriptionID: claim.StripeSubscriptionID,
			InvoiceID:      claim.DestinationInvoiceID.String,
			Description:    description,
			AmountCents:    cents,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			AllocationKey:  claim.IdempotencyKey,
			IdempotencyKey: claim.IdempotencyKey,
		})
		if err != nil {
			s.markAmbiguous(ctx, queries, claim, now)
			return fmt.Errorf("create Stripe invoice item: %w", err)
		}
		return s.confirmAllocation(ctx, queries, claim, item.ID, false, false, now)
	}

	if invoice.Status != "open" && invoice.Status != "paid" {
		return fmt.Errorf("destination invoice status %q does not support credit notes", invoice.Status)
	}
	if invoice.FinalizedAt.IsZero() {
		return errors.New("destination invoice is not finalized")
	}
	return s.createCreditNote(ctx, queries, claim, invoice, -cents, now)
}

func (s *SettleStripeInvoiceAllocations) findAllocation(
	ctx context.Context,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	cents int64,
) (string, error) {
	input := stripeclient.FindInvoiceAllocationInput{
		InvoiceID:     claim.DestinationInvoiceID.String,
		AllocationKey: claim.IdempotencyKey,
		AmountCents:   max(cents, -cents),
	}
	if cents > 0 {
		item, err := s.stripeClient.FindInvoiceItem(ctx, input)
		if err != nil {
			return "", fmt.Errorf("reconcile Stripe invoice item: %w", err)
		}
		if item == nil {
			return "", nil
		}
		if item.ID == "" || item.InvoiceID != input.InvoiceID || item.Currency != "usd" || item.AmountCents != input.AmountCents {
			return "", errors.New("reconcile Stripe invoice item: metadata match had different invoice, currency, or amount")
		}
		return item.ID, nil
	}

	note, err := s.stripeClient.FindCreditNote(ctx, input)
	if err != nil {
		return "", fmt.Errorf("reconcile Stripe credit note: %w", err)
	}
	if note == nil {
		return "", nil
	}
	if note.ID == "" || note.InvoiceID != input.InvoiceID || note.Currency != "usd" || note.AmountCents != input.AmountCents {
		return "", errors.New("reconcile Stripe credit note: metadata match had different invoice, currency, or amount")
	}
	return note.ID, nil
}

func (s *SettleStripeInvoiceAllocations) createCreditNote(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	invoice *stripeclient.InvoiceState,
	amountCents int64,
	now time.Time,
) error {
	remaining := max(invoice.AmountRemaining, 0)
	note, err := s.stripeClient.CreateCreditNote(ctx, stripeclient.CreateCreditNoteInput{
		InvoiceID:         claim.DestinationInvoiceID.String,
		Description:       allocationDescription(claim),
		AmountCents:       amountCents,
		CreditAmountCents: max(amountCents-remaining, 0),
		AllocationKey:     claim.IdempotencyKey,
		IdempotencyKey:    claim.IdempotencyKey,
	})
	if err != nil {
		s.markAmbiguous(ctx, queries, claim, now)
		return fmt.Errorf("create Stripe credit note: %w", err)
	}
	return s.confirmAllocation(ctx, queries, claim, note.ID, true, false, now)
}

func reconcileAndRotateAllocation(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	now time.Time,
) (string, error) {
	key, err := queries.ReconcileAndRotateStripeInvoiceAllocation(ctx, repo.ReconcileAndRotateStripeInvoiceAllocationParams{
		ReconciledAt:   timestamptz(now),
		OrganizationID: pgtype.Text{String: claim.OrganizationID, Valid: true},
		ID:             claim.ID,
		AttemptedAt:    timestamptz(now),
	})
	if err != nil {
		return "", fmt.Errorf("record Stripe allocation reconciliation: %w", err)
	}
	if key == "" {
		return "", errors.New("record Stripe allocation reconciliation: empty rotated key")
	}
	return key, nil
}

func validateClaimInvoice(claim repo.ClaimNextStripeInvoiceAllocationRow, invoice *stripeclient.InvoiceState) error {
	if invoice == nil {
		return errors.New("retrieve destination invoice: empty response")
	}
	if invoice.ID != claim.DestinationInvoiceID.String ||
		invoice.CustomerID != claim.StripeCustomerID ||
		invoice.SubscriptionID != claim.StripeSubscriptionID ||
		!invoice.ServicePeriodStart.Equal(claim.DestinationPeriodStart.Time.UTC()) ||
		!invoice.ServicePeriodEnd.Equal(claim.DestinationPeriodEnd.Time.UTC()) {
		return errors.New("destination invoice identity or service period changed")
	}
	if invoice.Currency != "usd" {
		return fmt.Errorf("destination invoice currency is %q, want usd", invoice.Currency)
	}
	return nil
}

func updateClaimInvoiceState(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	invoice *stripeclient.InvoiceState,
) error {
	finalizedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if !invoice.FinalizedAt.IsZero() {
		finalizedAt = timestamptz(invoice.FinalizedAt)
	}
	updated, err := queries.UpdateStripeInvoiceState(ctx, repo.UpdateStripeInvoiceStateParams{
		InvoiceState:         invoice.Status,
		FinalizedAt:          finalizedAt,
		OrganizationID:       pgtype.Text{String: claim.OrganizationID, Valid: true},
		StripeInvoiceID:      invoice.ID,
		StripeCustomerID:     invoice.CustomerID,
		StripeSubscriptionID: invoice.SubscriptionID,
		ServicePeriodStart:   timestamptz(invoice.ServicePeriodStart),
		ServicePeriodEnd:     timestamptz(invoice.ServicePeriodEnd),
	})
	if err != nil {
		return fmt.Errorf("update destination invoice state: %w", err)
	}
	if updated != 1 {
		return errors.New("update destination invoice state: identity mismatch")
	}
	return nil
}

func (s *SettleStripeInvoiceAllocations) markAmbiguous(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	now time.Time,
) {
	if _, err := queries.MarkStripeInvoiceAllocationAmbiguous(ctx, repo.MarkStripeInvoiceAllocationAmbiguousParams{
		AttemptedAt:    timestamptz(now),
		OrganizationID: pgtype.Text{String: claim.OrganizationID, Valid: true},
		ID:             claim.ID,
	}); err != nil {
		s.logger.ErrorContext(ctx, "mark Stripe invoice allocation ambiguous", attr.SlogError(err))
	}
}

func (s *SettleStripeInvoiceAllocations) confirmAllocation(
	ctx context.Context,
	queries *repo.Queries,
	claim repo.ClaimNextStripeInvoiceAllocationRow,
	stripeID string,
	creditNote bool,
	reconciled bool,
	now time.Time,
) error {
	if stripeID == "" {
		return errors.New("confirm Stripe allocation: empty Stripe object id")
	}
	organizationID := pgtype.Text{String: claim.OrganizationID, Valid: true}
	var updated int64
	var err error
	if creditNote {
		updated, err = queries.ConfirmStripeCreditNoteAllocation(ctx, repo.ConfirmStripeCreditNoteAllocationParams{
			StripeCreditNoteID: pgtype.Text{String: stripeID, Valid: true},
			ConfirmedAt:        timestamptz(now),
			Reconciled:         reconciled,
			OrganizationID:     organizationID,
			ID:                 claim.ID,
			AttemptedAt:        timestamptz(now),
		})
	} else {
		updated, err = queries.ConfirmStripeInvoiceItemAllocation(ctx, repo.ConfirmStripeInvoiceItemAllocationParams{
			StripeInvoiceItemID: pgtype.Text{String: stripeID, Valid: true},
			ConfirmedAt:         timestamptz(now),
			Reconciled:          reconciled,
			OrganizationID:      organizationID,
			ID:                  claim.ID,
			AttemptedAt:         timestamptz(now),
		})
	}
	if err != nil {
		return fmt.Errorf("confirm Stripe allocation: %w", err)
	}
	if updated != 1 {
		return errors.New("confirm Stripe allocation: claim lost")
	}
	return nil
}

func allocationPeriodAndDescription(claim repo.ClaimNextStripeInvoiceAllocationRow) (time.Time, time.Time, string, error) {
	if claim.SourceKind == stripeAllocationSourceOpenRouter && claim.SourceDay.Valid {
		start := time.Date(claim.SourceDay.Time.Year(), claim.SourceDay.Time.Month(), claim.SourceDay.Time.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), allocationDescription(claim), nil
	}
	if claim.SourceKind == stripeAllocationSourceTUM && claim.SourcePeriodStart.Valid && claim.SourcePeriodEnd.Valid {
		start := claim.SourcePeriodStart.Time.UTC()
		end := claim.SourcePeriodEnd.Time.UTC()
		if end.After(start) {
			return start, end, allocationDescription(claim), nil
		}
	}
	return time.Time{}, time.Time{}, "", fmt.Errorf("allocation %s has invalid source bounds", claim.ID)
}

func allocationDescription(claim repo.ClaimNextStripeInvoiceAllocationRow) string {
	if claim.SourceKind == stripeAllocationSourceOpenRouter && claim.SourceDay.Valid {
		return "OpenRouter chat usage for " + claim.SourceDay.Time.UTC().Format(time.DateOnly)
	}
	if claim.SourceKind == stripeAllocationSourceTUM && claim.SourcePeriodStart.Valid && claim.SourcePeriodEnd.Valid {
		return fmt.Sprintf("TUM usage adjustment for %s to %s",
			claim.SourcePeriodStart.Time.UTC().Format(time.DateOnly),
			claim.SourcePeriodEnd.Time.UTC().Format(time.DateOnly))
	}
	return "Usage adjustment"
}

func roundNumericToCents(value pgtype.Numeric) (int64, error) {
	if !value.Valid || value.NaN || value.InfinityModifier != pgtype.Finite || value.Int == nil {
		return 0, errors.New("amount is not a finite numeric")
	}

	abs := new(big.Int).Abs(new(big.Int).Set(value.Int))
	exponent := int64(value.Exp) + 2
	if exponent >= 0 {
		abs.Mul(abs, new(big.Int).Exp(big.NewInt(10), big.NewInt(exponent), nil))
	} else {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(-exponent), nil)
		quotient, remainder := new(big.Int).QuoRem(abs, divisor, new(big.Int))
		if new(big.Int).Lsh(remainder, 1).Cmp(divisor) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		abs = quotient
	}
	if value.Int.Sign() < 0 {
		abs.Neg(abs)
	}
	if !abs.IsInt64() {
		return 0, errors.New("amount exceeds Stripe minor-unit range")
	}
	return abs.Int64(), nil
}

func numericFromCents(cents int64) pgtype.Numeric {
	negative := cents < 0
	abs := new(big.Int).SetInt64(cents)
	if negative {
		abs.Abs(abs)
	}
	text := fmt.Sprintf("%s%s.%02d", map[bool]string{true: "-", false: ""}[negative], new(big.Int).Quo(abs, big.NewInt(100)).String(), new(big.Int).Mod(abs, big.NewInt(100)).Int64())
	var result pgtype.Numeric
	if err := result.Scan(text); err != nil {
		panic(fmt.Sprintf("represent cents as numeric: %v", err))
	}
	return result
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), InfinityModifier: pgtype.Finite, Valid: true}
}
