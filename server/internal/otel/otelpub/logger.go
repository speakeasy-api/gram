// Package otelpub is how the server's own code writes log records into the OTel pipeline.
package otelpub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/protobuf/proto"

	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
)

// A stuck Pub/Sub must not hold the caller forever.
const publishTimeout = 10 * time.Second

// ErrClosed is returned by Log once the logger is closed.
var ErrClosed = errors.New("otelpub logger is closed")

// Logger writes log records into the OTel pipeline and returns once Pub/Sub has acked them.
type Logger struct {
	publisher gcp.Publisher[*otelv1.InboundLogRecord]
	resource  *resource.Resource
	scope     string
	timeout   time.Duration

	// Log holds the read lock for its whole call, so Close's write lock waits for in-flight calls.
	mu     sync.RWMutex
	closed bool
}

// NewLogger returns a logger that publishes under the given resource and instrumentation scope.
func NewLogger(publisher gcp.Publisher[*otelv1.InboundLogRecord], res *resource.Resource, scope string) *Logger {
	return &Logger{
		publisher: publisher,
		resource:  res,
		scope:     scope,
		timeout:   publishTimeout,
		mu:        sync.RWMutex{},
		closed:    false,
	}
}

// Log publishes the record and returns nil only once Pub/Sub has acked it.
func (l *Logger) Log(ctx context.Context, record log.Record) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return ErrClosed
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.timeout)
	defer cancel()

	inbound, err := l.inbound(ctx, &record)
	if err != nil {
		return err
	}
	if err := otelsvc.ValidateInboundLogRecord(inbound); err != nil {
		return fmt.Errorf("validate log record: %w", err)
	}
	if _, err := l.publisher.Publish(ctx, inbound).Get(ctx); err != nil {
		return fmt.Errorf("publish log record: %w", err)
	}
	return nil
}

// Close waits for in-flight Log calls; later calls return ErrClosed.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
}

func (l *Logger) inbound(ctx context.Context, record *log.Record) (*otelv1.InboundLogRecord, error) {
	t, ok := tenantFrom(ctx)
	if !ok {
		return nil, errors.New("no tenancy in context")
	}

	inbound, err := inboundFromRecord(ctx, record, l.resource, l.scope)
	if err != nil {
		return nil, err
	}
	for _, attrs := range [][]*otelv1.InboundLogRecord_KeyValue{
		inbound.GetAttributes(),
		inbound.GetResource().GetAttributes(),
		inbound.GetScope().GetAttributes(),
	} {
		for _, kv := range attrs {
			if enrich.IsPipelineKey(kv.GetKey()) {
				return nil, fmt.Errorf("attribute %q is in the reserved namespace", kv.GetKey())
			}
		}
	}

	inbound.SetProvenance((&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(otelsvc.ProvenanceSource),
		OrganizationId: &t.organizationID,
		ProjectId:      &t.projectID,
	}).Build())

	// Derived before observed time is set, which differs on every call.
	id := recordIDFrom(ctx)
	if id == "" {
		id, err = contentRecordID(inbound)
		if err != nil {
			return nil, err
		}
	}
	inbound.SetRecordId(id)

	observed := record.ObservedTimestamp()
	if observed.IsZero() {
		observed = time.Now()
	}
	inbound.SetObservedTimeUnixNano(unixNano(observed))
	return inbound, nil
}

func contentRecordID(record *otelv1.InboundLogRecord) (string, error) {
	var options proto.MarshalOptions
	options.Deterministic = true
	encoded, err := options.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("marshal log record for record id: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
