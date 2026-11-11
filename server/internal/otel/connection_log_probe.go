package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

const connectionLogProbeTimeout = 5 * time.Second

// ConnectionLogProbe sends one connection log record straight to a destination.
// It uses the relay's transform, request builder, outbound policy and client, with
// no retries and a short timeout, and publishes nothing, so no other sink sees it.
type ConnectionLogProbe struct {
	relay       *signalRelay
	instruments *enrich.Instruments
	// enrichers is tenancy only: the directory enricher would add a person.
	enrichers []enrich.LogEnricher
}

func NewConnectionLogProbe(logger *slog.Logger, meterProvider metric.MeterProvider, policy *guardian.Policy) *ConnectionLogProbe {
	relay := newSignalRelay(nil, nil, policy, dataexports.DataSourceMCPConnectionLogs, "/v1/logs", "connection log test")
	relay.maxRetries = 0
	relay.timeout = connectionLogProbeTimeout
	return &ConnectionLogProbe{
		relay:       relay,
		instruments: enrich.NewInstruments(logger, meterProvider),
		enrichers:   []enrich.LogEnricher{enrich.NewLogTenancy()},
	}
}

// Send returns the destination's HTTP status, or an error wrapping
// dataexports.ErrTestUnreachable when no response came back.
func (p *ConnectionLogProbe) Send(ctx context.Context, destination dataexports.TestDestination, inbound *otelv1.InboundLogRecord) (int, error) {
	record, err := transformInboundLog(ctx, p.instruments, inbound, p.enrichers)
	if err != nil {
		return 0, fmt.Errorf("transform connection log test record: %w", err)
	}
	if !isConnectionLogRecord(record) {
		return 0, errors.New("connection log test record has the wrong provenance source")
	}
	target, err := p.relay.newDestination(relayRouteKey{organizationID: destination.OrganizationID, projectID: destination.ProjectID}, destination.EndpointURL, destination.Headers, destination.IncludeSensitiveData)
	if err != nil {
		return 0, fmt.Errorf("build connection log test destination: %w", err)
	}
	defer closeIdleRelayDestination(target)

	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record}, target.includeSensitiveData)
	if err != nil {
		return 0, fmt.Errorf("build connection log test export: %w", err)
	}
	statusCode, err := target.exportWithLimit(ctx, request, maxLogRelayExportBytes)
	if statusCode != 0 {
		return statusCode, nil
	}
	if exportErr, ok := errors.AsType[*relayExportError](err); ok && exportErr.reason == relayReasonNetworkError {
		return 0, fmt.Errorf("%w: %w", dataexports.ErrTestUnreachable, err)
	}
	return 0, err
}
