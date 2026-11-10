package otel

import "github.com/speakeasy-api/gram/server/internal/constants"

const (
	maxMetricRelayExportBytes = 512 * constants.KiB

	// Keep destination exports within Datadog's 512 KiB compressed intake
	// limit even though the relay sends uncompressed protobuf. Reserve space
	// for resource context, future bounded enrichments, and OTLP wrappers.
	metricRelayEnvelopeHeadroom = 64 * constants.KiB
	maxOTLPMetricBytes          = maxMetricRelayExportBytes - metricRelayEnvelopeHeadroom
)
