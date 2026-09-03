package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	ErrTypeTUMStripeReporting = "tum_stripe_reporting_failed"

	tumBillingFreezeDelay       = 48 * time.Hour
	tumObservationDelay         = 72 * time.Hour
	stripeIdentifierRetryWindow = 24 * time.Hour
	// Stripe summaries are eventually consistent. Positive evidence can
	// confirm delivery immediately, but absence must remain stable for 24
	// hours after its first observation before a new identifier is emitted.
	stripeMissingEvidenceWindow = 24 * time.Hour
)

// ReportTUMUsageToStripeFailureDetails survives Temporal error conversion so
// the workflow can count the organizations that actually failed.
type ReportTUMUsageToStripeFailureDetails struct {
	FailedOrganizationCount int
}

// ReportTUMUsageToStripeInput carries one deterministic hourly reporting pass.
type ReportTUMUsageToStripeInput struct {
	OrganizationIDs []string
	Now             time.Time
}

// ReportTUMUsageToStripe turns durable TUM snapshots into durable Stripe
// delivery intents. It never recomputes usage from ClickHouse.
type ReportTUMUsageToStripe struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	stripeClient tumStripeClient
	enabled      bool
}

type tumStripeClient interface {
	Catalog() stripeclient.Catalog
	CreateMeterEvent(context.Context, stripeclient.CreateMeterEventInput) error
	GetMeterEventSummary(context.Context, stripeclient.GetMeterEventSummaryInput) (float64, error)
}

func NewReportTUMUsageToStripe(logger *slog.Logger, db *pgxpool.Pool, stripeClient tumStripeClient, enabled bool) *ReportTUMUsageToStripe {
	return &ReportTUMUsageToStripe{
		logger:       logger,
		db:           db,
		stripeClient: stripeClient,
		enabled:      enabled,
	}
}

func (r *ReportTUMUsageToStripe) Do(ctx context.Context, input ReportTUMUsageToStripeInput) error {
	if !r.enabled || r.stripeClient == nil {
		return nil
	}

	catalog := r.stripeClient.Catalog()
	// Local and legacy-only deployments use the Stripe stub. They must keep
	// the shared billing sweep healthy without manufacturing delivery intents.
	if !stripeclient.IsConfigured(catalog.MeterIDTUM) || !stripeclient.IsConfigured(catalog.MeterEventName) {
		return nil
	}

	now := input.Now.UTC()
	if now.IsZero() {
		return errors.New("report TUM usage to Stripe: current time is required")
	}

	queries := usagerepo.New(r.db)
	var errs []error
	for _, organizationID := range input.OrganizationIDs {
		if err := r.reportOrganization(ctx, queries, catalog, organizationID, now); err != nil {
			r.logger.ErrorContext(ctx, "failed to report TUM usage to Stripe",
				attr.SlogOrganizationID(organizationID), attr.SlogError(err))
			errs = append(errs, fmt.Errorf("report organization %s: %w", organizationID, err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return temporal.NewApplicationErrorWithOptions(
		"report TUM usage to Stripe",
		ErrTypeTUMStripeReporting,
		temporal.ApplicationErrorOptions{
			Cause: errors.Join(errs...),
			Details: []any{ReportTUMUsageToStripeFailureDetails{
				FailedOrganizationCount: len(errs),
			}},
		},
	)
}

func (r *ReportTUMUsageToStripe) reportOrganization(
	ctx context.Context,
	queries *usagerepo.Queries,
	catalog stripeclient.Catalog,
	organizationID string,
	now time.Time,
) error {
	organization, err := queries.GetTUMMeteringOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get metering organization: %w", err)
	}
	if organization.GramAccountType != "payg" {
		return nil
	}
	if !organization.StripeCustomerID.Valid || !organization.StripeSubscriptionID.Valid || !organization.StripeBillingCycleAnchor.Valid {
		return errors.New("PAYG organization has incomplete Stripe billing identity")
	}
	organizationIDParam := pgtype.Text{String: organizationID, Valid: true}

	var errs []error
	didReconcile, reconcileErr := r.reconcileExpiredAmbiguity(ctx, queries, organizationID, now)
	if reconcileErr != nil {
		errs = append(errs, reconcileErr)
	}

	cycles, err := queries.ListTUMBillingCyclesForReporting(ctx, usagerepo.ListTUMBillingCyclesForReportingParams{
		OrganizationID: organizationIDParam,
		FirstPaidCycleStart: pgtype.Timestamptz{
			Time:             organization.StripeBillingCycleAnchor.Time.UTC(),
			Valid:            true,
			InfinityModifier: pgtype.Finite,
		},
	})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("list billing cycles: %w", err))...)
	}

	anchor := organization.StripeBillingCycleAnchor.Time.UTC()
	for _, cycle := range cycles {
		if !validPaidCycleBounds(cycle, anchor) {
			continue
		}

		freezeAt := cycle.CycleEnd.Time.UTC().Add(tumBillingFreezeDelay)
		observationEndsAt := cycle.CycleEnd.Time.UTC().Add(tumObservationDelay)
		if !now.Before(freezeAt) && now.Before(observationEndsAt) && !cycle.BilledTumTokens.Valid {
			frozen, freezeErr := queries.FreezeTUMBillingCycleBaseline(ctx, usagerepo.FreezeTUMBillingCycleBaselineParams{
				FrozenAt:            pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
				OrganizationID:      organizationIDParam,
				BillingCycleUsageID: cycle.ID,
			})
			if freezeErr != nil && !errors.Is(freezeErr, pgx.ErrNoRows) {
				errs = append(errs, fmt.Errorf("freeze cycle %s: %w", cycle.CycleStart.Time, freezeErr))
				continue
			}
			if freezeErr == nil {
				cycle = frozen
			}
		}

		if !now.Before(observationEndsAt) {
			if !cycle.BilledTumTokens.Valid && cycle.FinalizedAt.Valid {
				recovered, recoverErr := queries.FreezeMissedTUMBillingCycleBaseline(ctx, usagerepo.FreezeMissedTUMBillingCycleBaselineParams{
					FrozenAt:            pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
					OrganizationID:      organizationIDParam,
					BillingCycleUsageID: cycle.ID,
				})
				if recoverErr != nil && !errors.Is(recoverErr, pgx.ErrNoRows) {
					errs = append(errs, fmt.Errorf("recover missed freeze for cycle %s: %w", cycle.CycleStart.Time, recoverErr))
					continue
				}
				if recoverErr == nil {
					cycle = recovered
				}
			}
			if cycle.BilledTumTokens.Valid && cycle.FinalizedAt.Valid {
				if _, allocationErr := queries.CreateTUMCarryAllocation(ctx, usagerepo.CreateTUMCarryAllocationParams{
					OrganizationID:      organizationIDParam,
					BillingCycleUsageID: cycle.ID,
					TumUnitPriceUsd:     usage.TUMUnitPriceUSD,
				}); allocationErr != nil {
					errs = append(errs, fmt.Errorf("create carry allocation for cycle %s: %w", cycle.CycleStart.Time, allocationErr))
				}
			}
			// Observation stops at +72h, but delivery of the immutable +48h
			// baseline does not. A report found missing during reconciliation
			// still needs a new identifier and correction after this cutoff.
			if !cycle.BilledTumTokens.Valid {
				continue
			}
		}

		targetTokens := cycle.TumTokens
		if cycle.BilledTumTokens.Valid {
			targetTokens = cycle.BilledTumTokens.Int64
		}
		eventTimestamp := now.Truncate(time.Second)
		if !now.Before(cycle.CycleEnd.Time) || cycle.BilledTumTokens.Valid {
			eventTimestamp = cycle.CycleEnd.Time.UTC().Add(-time.Second)
		}

		_, intentErr := queries.CreateTUMMeterReportIntent(ctx, usagerepo.CreateTUMMeterReportIntentParams{
			StripeCustomerID:     organization.StripeCustomerID,
			StripeMeterEventName: pgtype.Text{String: catalog.MeterEventName, Valid: true},
			EventTimestamp:       pgtype.Timestamptz{Time: eventTimestamp, Valid: true, InfinityModifier: pgtype.Finite},
			OrganizationID:       organizationIDParam,
			BillingCycleUsageID:  cycle.ID,
			TargetTumTokens:      targetTokens,
		})
		if intentErr != nil && !errors.Is(intentErr, pgx.ErrNoRows) {
			errs = append(errs, fmt.Errorf("create meter intent for cycle %s: %w", cycle.CycleStart.Time, intentErr))
		}
	}

	// At most one Stripe operation is made per organization per activity run.
	// With five organizations and a 30-second HTTP deadline, the activity's
	// three-minute deadline remains a real upper bound even during backfills.
	if !didReconcile {
		if err := r.deliverPendingIntents(ctx, queries, organizationID, now); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func validPaidCycleBounds(cycle usagerepo.BillingCycleUsage, anchor time.Time) bool {
	start := cycle.CycleStart.Time.UTC()
	end := cycle.CycleEnd.Time.UTC()
	if !cycle.CycleStart.Valid || !cycle.CycleEnd.Valid || start.Before(anchor) {
		return false
	}

	expectedStart, expectedEnd := usage.CurrentBillingCycle(start, anchor.Day())
	return start.Equal(expectedStart) && end.Equal(expectedEnd)
}

func (r *ReportTUMUsageToStripe) reconcileExpiredAmbiguity(
	ctx context.Context,
	queries *usagerepo.Queries,
	organizationID string,
	now time.Time,
) (bool, error) {
	organizationIDParam := pgtype.Text{String: organizationID, Valid: true}
	retryAfter := now.Add(-stripeIdentifierRetryWindow)
	cycles, err := queries.ListStaleTUMMeterReportCycles(ctx, usagerepo.ListStaleTUMMeterReportCyclesParams{
		OrganizationID: organizationIDParam,
		RetryAfter:     pgtype.Timestamptz{Time: retryAfter, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return false, fmt.Errorf("list stale meter reports: %w", err)
	}
	if len(cycles) == 0 {
		return false, nil
	}

	var errs []error
	for _, cycle := range cycles {
		observed, summaryErr := r.stripeClient.GetMeterEventSummary(ctx, stripeclient.GetMeterEventSummaryInput{
			CustomerID: cycle.StripeCustomerID.String,
			Start:      cycle.CycleStart.Time.UTC(),
			End:        cycle.CycleEnd.Time.UTC(),
		})
		if summaryErr != nil {
			errs = append(errs, fmt.Errorf("get Stripe summary for cycle %s: %w", cycle.CycleStart.Time, summaryErr))
			continue
		}
		if math.IsNaN(observed) || math.IsInf(observed, 0) || math.Trunc(observed) != observed || observed > math.MaxInt64 || observed < math.MinInt64 {
			errs = append(errs, fmt.Errorf("reconcile cycle %s: Stripe returned non-integral TUM total %v", cycle.CycleStart.Time, observed))
			continue
		}

		totals, totalsErr := queries.GetTUMMeterReportTotals(ctx, usagerepo.GetTUMMeterReportTotalsParams{
			OrganizationID:      organizationIDParam,
			BillingCycleUsageID: cycle.BillingCycleUsageID.UUID,
		})
		if totalsErr != nil {
			errs = append(errs, fmt.Errorf("get report totals for cycle %s: %w", cycle.CycleStart.Time, totalsErr))
			continue
		}

		reconciledAt := pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite}
		switch int64(observed) {
		case totals.IntendedTokens:
			_, err = queries.ConfirmReconciledTUMMeterReports(ctx, usagerepo.ConfirmReconciledTUMMeterReportsParams{
				ReconciledAt:        reconciledAt,
				OrganizationID:      organizationIDParam,
				BillingCycleUsageID: cycle.BillingCycleUsageID,
				RetryAfter:          pgtype.Timestamptz{Time: retryAfter, Valid: true, InfinityModifier: pgtype.Finite},
			})
		case totals.ConfirmedTokens:
			if !cycle.AbsenceObservedAt.Valid {
				_, err = queries.NoteTUMMeterReportReconciliation(ctx, usagerepo.NoteTUMMeterReportReconciliationParams{
					ReconciledAt:        reconciledAt,
					OrganizationID:      organizationIDParam,
					BillingCycleUsageID: cycle.BillingCycleUsageID,
					RetryAfter:          pgtype.Timestamptz{Time: retryAfter, Valid: true, InfinityModifier: pgtype.Finite},
				})
				if err == nil {
					err = fmt.Errorf("stripe total %d first showed absence; awaiting confirmation window", int64(observed))
				}
			} else if cycle.AbsenceObservedAt.Time.After(now.Add(-stripeMissingEvidenceWindow)) {
				err = fmt.Errorf("stripe total %d has not shown absence for the full reconciliation window", int64(observed))
			} else {
				_, err = queries.MarkReconciledTUMMeterReportsMissing(ctx, usagerepo.MarkReconciledTUMMeterReportsMissingParams{
					ReconciledAt:        reconciledAt,
					OrganizationID:      organizationIDParam,
					BillingCycleUsageID: cycle.BillingCycleUsageID,
					RetryAfter:          pgtype.Timestamptz{Time: retryAfter, Valid: true, InfinityModifier: pgtype.Finite},
				})
			}
		default:
			_, err = queries.NoteTUMMeterReportReconciliation(ctx, usagerepo.NoteTUMMeterReportReconciliationParams{
				ReconciledAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: pgtype.Finite,
					Valid:            false,
				},
				OrganizationID:      organizationIDParam,
				BillingCycleUsageID: cycle.BillingCycleUsageID,
				RetryAfter:          pgtype.Timestamptz{Time: retryAfter, Valid: true, InfinityModifier: pgtype.Finite},
			})
			if err == nil {
				err = fmt.Errorf("stripe total %d matches neither confirmed %d nor intended %d", int64(observed), totals.ConfirmedTokens, totals.IntendedTokens)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("reconcile cycle %s: %w", cycle.CycleStart.Time, err))
		}
	}

	return true, errors.Join(errs...)
}

func (r *ReportTUMUsageToStripe) deliverPendingIntents(
	ctx context.Context,
	queries *usagerepo.Queries,
	organizationID string,
	now time.Time,
) error {
	organizationIDParam := pgtype.Text{String: organizationID, Valid: true}
	reports, err := queries.ListTUMMeterReportsForDelivery(ctx, usagerepo.ListTUMMeterReportsForDeliveryParams{
		OrganizationID: organizationIDParam,
		RetryAfter:     pgtype.Timestamptz{Time: now.Add(-stripeIdentifierRetryWindow), Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return fmt.Errorf("list meter reports for delivery: %w", err)
	}

	var errs []error
	for _, report := range reports {
		attempt, attemptErr := queries.BeginTUMMeterReportAttempt(ctx, usagerepo.BeginTUMMeterReportAttemptParams{
			AttemptedAt:    pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
			OrganizationID: organizationIDParam,
			ID:             report.ID,
			RetryAfter:     pgtype.Timestamptz{Time: now.Add(-stripeIdentifierRetryWindow), Valid: true, InfinityModifier: pgtype.Finite},
		})
		if errors.Is(attemptErr, pgx.ErrNoRows) {
			continue
		}
		if attemptErr != nil {
			errs = append(errs, fmt.Errorf("begin meter report %s attempt: %w", report.ID, attemptErr))
			continue
		}
		if !attempt.StripeCustomerID.Valid || !attempt.StripeMeterEventName.Valid || !attempt.StripeIdentifier.Valid || !attempt.EventTimestamp.Valid {
			errs = append(errs, fmt.Errorf("meter report %s has incomplete durable payload", attempt.ID))
			continue
		}

		createErr := r.stripeClient.CreateMeterEvent(ctx, stripeclient.CreateMeterEventInput{
			CustomerID:     attempt.StripeCustomerID.String,
			EventName:      attempt.StripeMeterEventName.String,
			Value:          attempt.DeltaTokens,
			Timestamp:      attempt.EventTimestamp.Time.UTC(),
			IdempotencyKey: attempt.StripeIdentifier.String,
		})
		if createErr != nil {
			if _, markErr := queries.MarkTUMMeterReportAmbiguous(ctx, usagerepo.MarkTUMMeterReportAmbiguousParams{
				AmbiguousAt:    pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
				OrganizationID: organizationIDParam,
				ID:             attempt.ID,
			}); markErr != nil {
				errs = append(errs, fmt.Errorf("mark meter report %s ambiguous: %w", attempt.ID, markErr))
			}
			errs = append(errs, fmt.Errorf("deliver meter report %s: %w", attempt.ID, createErr))
			continue
		}

		rows, confirmErr := queries.ConfirmTUMMeterReport(ctx, usagerepo.ConfirmTUMMeterReportParams{
			ConfirmedAt:    pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
			OrganizationID: organizationIDParam,
			ID:             attempt.ID,
		})
		if confirmErr != nil {
			errs = append(errs, fmt.Errorf("confirm meter report %s: %w", attempt.ID, confirmErr))
		} else if rows != 1 {
			errs = append(errs, fmt.Errorf("confirm meter report %s: updated %d rows", attempt.ID, rows))
		}
	}

	return errors.Join(errs...)
}
