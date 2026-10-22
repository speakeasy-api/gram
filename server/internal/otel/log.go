package otel

import "github.com/speakeasy-api/gram/server/internal/constants"

const (
	maxLogRelayExportBytes = 4 * constants.MiB

	// logRelayEnvelopeHeadroom reserves space for OTLP export wrappers and all
	// enrichments added between ingestion and destination delivery.
	logRelayEnvelopeHeadroom = 256 * constants.KiB

	maxOTLPLogRecordBytes = maxLogRelayExportBytes - logRelayEnvelopeHeadroom
)
