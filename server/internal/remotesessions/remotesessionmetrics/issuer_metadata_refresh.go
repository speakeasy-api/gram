package remotesessionmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterIssuerMetadataRefresh = "gram.remote_session_issuer.metadata_refresh"

// IssuerMetadataRefreshOutcome labels one scheduled issuer metadata refresh attempt; the values partition every exit.
type IssuerMetadataRefreshOutcome string

const (
	// IssuerMetadataRefreshOutcomeRefreshed: the upstream document was fetched, vetted, and applied.
	IssuerMetadataRefreshOutcomeRefreshed IssuerMetadataRefreshOutcome = "refreshed"

	// IssuerMetadataRefreshOutcomeRefreshedPartial: applied, but one well-known candidate was unreadable.
	IssuerMetadataRefreshOutcomeRefreshedPartial IssuerMetadataRefreshOutcome = "refreshed_partial"

	// IssuerMetadataRefreshOutcomeReprojected: the stored document was re-projected onto the typed columns without a fetch.
	IssuerMetadataRefreshOutcomeReprojected IssuerMetadataRefreshOutcome = "reprojected"

	// IssuerMetadataRefreshOutcomeReprojectInvalid: the stored document no longer passes vetting and was left for the fetch pass.
	IssuerMetadataRefreshOutcomeReprojectInvalid IssuerMetadataRefreshOutcome = "reproject_invalid"

	// IssuerMetadataRefreshOutcomeTransientFailure: the upstream could not be read; retried on the next hourly tick.
	IssuerMetadataRefreshOutcomeTransientFailure IssuerMetadataRefreshOutcome = "transient_failure"

	// IssuerMetadataRefreshOutcomeDefinitiveFailure: the upstream answered and the answer is unusable; retried next day.
	IssuerMetadataRefreshOutcomeDefinitiveFailure IssuerMetadataRefreshOutcome = "definitive_failure"

	// IssuerMetadataRefreshOutcomeConflict: the row moved, was repointed, or was deleted mid-refresh.
	IssuerMetadataRefreshOutcomeConflict IssuerMetadataRefreshOutcome = "conflict"

	// IssuerMetadataRefreshOutcomeSkippedTunnelDown: the issuer rides a tunnel with no live route.
	IssuerMetadataRefreshOutcomeSkippedTunnelDown IssuerMetadataRefreshOutcome = "skipped_tunnel_down"

	// IssuerMetadataRefreshOutcomeInternalError: Gram could not load or persist the row.
	IssuerMetadataRefreshOutcomeInternalError IssuerMetadataRefreshOutcome = "internal_error"
)

// IssuerMetadataRefresh holds the scheduled issuer metadata refresh instrument.
type IssuerMetadataRefresh struct {
	attempts metric.Int64Counter
}

func NewIssuerMetadataRefresh(logger *slog.Logger, meterProvider metric.MeterProvider) *IssuerMetadataRefresh {
	meter := meterProvider.Meter(meterScope)

	attempts, err := meter.Int64Counter(
		meterIssuerMetadataRefresh,
		metric.WithDescription("Scheduled RFC 8414 metadata refresh attempts for Remote Session issuers, by outcome and issuer host."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterIssuerMetadataRefresh), attr.SlogError(err))
	}

	return &IssuerMetadataRefresh{attempts: attempts}
}

// Record counts one refresh attempt by its outcome and the issuer's host.
func (m *IssuerMetadataRefresh) Record(ctx context.Context, issuerHost string, outcome IssuerMetadataRefreshOutcome) {
	if m == nil || m.attempts == nil {
		return
	}
	m.attempts.Add(ctx, 1, metric.WithAttributes(
		attr.ServerAddress(issuerHost),
		attr.Outcome(outcome),
	))
}
