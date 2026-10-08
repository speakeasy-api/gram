// Package gramotel is the one way the server's own code writes log records into
// the OTel pipeline, so they go through the same transform, column enrichers
// and writers as every customer producer.
//
// It has two layers. The publishing core, Publish and PublishLogs, holds the
// only copy of the ingest contract: validate every record, publish them, wait
// for Pub/Sub, count the outcome. The ingest edge forwards whole OTLP exports
// through it. On top of the core, NewLoggerProvider builds an OpenTelemetry
// SDK logger provider, so instrumented code uses the standard logs API: each
// Emit stamps tenancy, record id, observed time and scope, publishes its one
// record and waits for the ack before returning. WithResult lets a caller read
// the publish error that Emit itself cannot return.
//
// Records written here never carry the speakeasy.agent namespace: those keys
// are written only by the column enrichers in the transform.
package gramotel

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// ProvenanceSource is the provenance source stamped on every record Speakeasy
// accepts into the pipeline, from customers through the ingest edge and from
// the server's own code alike.
const ProvenanceSource = "speakeasy"

const (
	// MaxLogRelayExportBytes is the largest OTLP log export the relays send to
	// a customer destination in one request.
	MaxLogRelayExportBytes = 4 * constants.MiB

	// LogRelayEnvelopeHeadroom reserves space inside a relay export for OTLP
	// wrappers and for every enrichment added between ingestion and delivery.
	LogRelayEnvelopeHeadroom = 256 * constants.KiB

	// MaxLogRecordBytes is the largest single log record the pipeline accepts,
	// sized so that one record plus its enrichments still fits a relay export.
	MaxLogRecordBytes = MaxLogRelayExportBytes - LogRelayEnvelopeHeadroom
)

const (
	// TraceIDSize is the size of an OTLP trace id in bytes.
	TraceIDSize = 16

	// SpanIDSize is the size of an OTLP span id in bytes.
	SpanIDSize = 8
)

// Signal names which kind of record a publish carries, for the metrics.
type Signal string

const (
	SignalLog    Signal = "log"
	SignalSpan   Signal = "span"
	SignalMetric Signal = "metric"
)

// ErrInvalid marks a record the pipeline refuses: it fails the ingest
// contract, carries a reserved attribute, or has no tenancy. Retrying the
// same record cannot succeed, so callers treat it as a client fault rather
// than a transient failure.
var ErrInvalid = errors.New("record refused by the ingest contract")
