package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterIssuerMetadataRefresh = "gram.remote_session_issuer.metadata_refresh"

// IssuerMetadataRefreshOutcome labels one on-use issuer metadata refresh; the values partition every exit.
type IssuerMetadataRefreshOutcome string

const (
	// IssuerMetadataRefreshOutcomeRefreshed: the upstream document was fetched, vetted, and applied.
	IssuerMetadataRefreshOutcomeRefreshed IssuerMetadataRefreshOutcome = "refreshed"

	// IssuerMetadataRefreshOutcomeRefreshedPartial: applied, but one well-known candidate was unreadable.
	IssuerMetadataRefreshOutcomeRefreshedPartial IssuerMetadataRefreshOutcome = "refreshed_partial"

	// IssuerMetadataRefreshOutcomeReprojected: the stored document was re-projected onto the typed columns without a fetch.
	IssuerMetadataRefreshOutcomeReprojected IssuerMetadataRefreshOutcome = "reprojected"

	// IssuerMetadataRefreshOutcomeReprojectInvalid: the stored document no longer passes vetting and was left for the fetch.
	IssuerMetadataRefreshOutcomeReprojectInvalid IssuerMetadataRefreshOutcome = "reproject_invalid"

	// IssuerMetadataRefreshOutcomeTransientFailure: the upstream could not be read; retried on use after the retry window.
	IssuerMetadataRefreshOutcomeTransientFailure IssuerMetadataRefreshOutcome = "transient_failure"

	// IssuerMetadataRefreshOutcomeDefinitiveFailure: the upstream answered and the answer is unusable; retried on use after the daily cutoff.
	IssuerMetadataRefreshOutcomeDefinitiveFailure IssuerMetadataRefreshOutcome = "definitive_failure"

	// IssuerMetadataRefreshOutcomeConflict: the row moved, was repointed, or was deleted mid-refresh.
	IssuerMetadataRefreshOutcomeConflict IssuerMetadataRefreshOutcome = "conflict"

	// IssuerMetadataRefreshOutcomeSkippedBusy: every refresh slot on this replica was taken; the next use tries again.
	IssuerMetadataRefreshOutcomeSkippedBusy IssuerMetadataRefreshOutcome = "skipped_busy"

	// IssuerMetadataRefreshOutcomeSkippedInFlight: this replica is already refreshing the issuer.
	IssuerMetadataRefreshOutcomeSkippedInFlight IssuerMetadataRefreshOutcome = "skipped_in_flight"

	// IssuerMetadataRefreshOutcomeInternalError: Gram could not load or persist the row.
	IssuerMetadataRefreshOutcomeInternalError IssuerMetadataRefreshOutcome = "internal_error"
)

// IssuerMetadataRefresh holds the on-use issuer metadata refresh instrument.
type IssuerMetadataRefresh struct {
	attempts metric.Int64Counter
}

func NewIssuerMetadataRefresh(logger *slog.Logger, meterProvider metric.MeterProvider) *IssuerMetadataRefresh {
	meter := meterProvider.Meter(meterScope)

	attempts, err := meter.Int64Counter(
		meterIssuerMetadataRefresh,
		metric.WithDescription("RFC 8414 metadata refreshes triggered by Remote Session issuer use, by outcome and issuer."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterIssuerMetadataRefresh), attr.SlogError(err))
	}

	return &IssuerMetadataRefresh{attempts: attempts}
}

// Record counts one refresh attempt by its outcome and the issuer.
func (m *IssuerMetadataRefresh) Record(ctx context.Context, issuerURL string, outcome IssuerMetadataRefreshOutcome) {
	if m == nil || m.attempts == nil {
		return
	}
	m.attempts.Add(ctx, 1, metric.WithAttributes(
		attr.OAuthIssuer(issuerURL),
		attr.Outcome(outcome),
	))
}
