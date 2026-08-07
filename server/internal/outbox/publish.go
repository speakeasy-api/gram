// Package outbox provides the producer side of the transactional outbox. It
// writes a message, and the topic that message is destined for, using the
// caller's transaction, so the message is enqueued if and only if the caller's
// work commits.
//
// Typical usage from a service handler:
//
//	_, err := outbox.PublishWebhookEvent(ctx, tx, orgID, events.AuditLogCreated, payload)
//	if err != nil { ... }
//	// then commit the caller's transaction
//
// Nothing is delivered inline. A background relay claims committed rows and
// hands each one to the Pub/Sub topic its message type declares; what any
// particular consumer does with it — Svix included — is that consumer's
// subscription, not this package's concern.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/topics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/repo"
)

// DBTX is the minimal database interface required by Publish. Callers can pass
// a transaction or a pool; the batch variants require repo.DBTX because they
// COPY (see PublishBatch).
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// noopCopyFromDBTX adapts a DBTX to repo.DBTX for the single-row path, which
// never copies. Reaching CopyFrom through it is a programming error, not a
// runtime condition, so it fails permanently rather than inviting a retry.
type noopCopyFromDBTX struct {
	DBTX
}

func (n noopCopyFromDBTX) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, oops.Permanent(errors.New("not implemented"))
}

// maxMessageBytes bounds what may be enqueued. Pub/Sub rejects messages above
// 10 MiB, and the relay drains strictly in id order, so a single oversized row
// would sit at the head of the queue failing forever and block every message
// behind it. Rejecting at write time keeps that failure attached to the code
// that caused it. The margin covers the attribute map, which counts toward the
// same limit.
const maxMessageBytes = 9 * 1024 * 1024

// Message is a single enqueued publish.
type Message struct {
	// Proto is the message to publish. Its type determines the destination
	// topic and must declare a (gcp.pubsub.v1.topic) option.
	Proto proto.Message

	// PublicID pins the row's public_id. Leave it zero to have one generated.
	// Set it when the same id also has to appear inside Proto — as the webhook
	// envelope's event_id does — since the body is marshaled before the row
	// exists and that is the only way for the two to agree.
	PublicID uuid.UUID

	// Attributes are Pub/Sub message attributes. Unlike the body, these are
	// visible to subscription filter expressions, so a routing discriminator
	// belongs here. The content-type and schema markers are derived at publish
	// time and cannot be set from here, and neither can the trace context.
	Attributes map[string]string
}

// PublishResult identifies the enqueued row.
type PublishResult struct {
	ID       int64
	PublicID uuid.UUID
}

// PublishBatchResult is returned by PublishBatch.
type PublishBatchResult struct {
	Count int64
}

// Publish enqueues a protobuf message for publication to the Pub/Sub topic that
// message's type declares. The row is written with the caller's transaction, so
// the message is enqueued if and only if the caller's work commits.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func Publish(ctx context.Context, dbtx DBTX, orgID string, msg Message) (PublishResult, error) {
	entry, err := buildEntry(ctx, orgID, msg, otel.GetTextMapPropagator())
	if err != nil {
		return PublishResult{}, err
	}

	row, err := repo.New(noopCopyFromDBTX{DBTX: dbtx}).InsertPublishOutboxEntry(ctx, repo.InsertPublishOutboxEntryParams{
		PublicID:       entry.PublicID,
		OrganizationID: entry.OrganizationID,
		Topic:          entry.Topic,
		Message:        entry.Message,
		Attributes:     entry.Attributes,
	})
	if err != nil {
		return PublishResult{}, fmt.Errorf("insert publish outbox entry: %w", err)
	}

	return PublishResult{ID: row.ID, PublicID: row.PublicID}, nil
}

// PublishBatch enqueues many messages in one COPY. Much more efficient than
// repeated Publish calls for large batches, at the cost of not reading the
// generated ids back. Messages may target different topics.
//
// THIS METHOD MUST BE CALLED WITHIN A TRANSACTION.
func PublishBatch(ctx context.Context, dbtx repo.DBTX, orgID string, msgs []Message) (PublishBatchResult, error) {
	propagator := otel.GetTextMapPropagator()

	entries := make([]repo.BulkInsertPublishOutboxEntriesParams, 0, len(msgs))
	for _, msg := range msgs {
		entry, err := buildEntry(ctx, orgID, msg, propagator)
		if err != nil {
			return PublishBatchResult{}, err
		}

		entries = append(entries, repo.BulkInsertPublishOutboxEntriesParams{
			PublicID:       entry.PublicID,
			OrganizationID: entry.OrganizationID,
			Topic:          entry.Topic,
			Message:        entry.Message,
			Attributes:     entry.Attributes,
		})
	}

	n, err := repo.New(dbtx).BulkInsertPublishOutboxEntries(ctx, entries)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("bulk insert publish outbox entries: %w", err)
	}

	return PublishBatchResult{Count: n}, nil
}

type publishEntry struct {
	PublicID       uuid.UUID
	OrganizationID string
	Topic          string
	Message        []byte
	Attributes     []byte
}

// buildEntry turns a message into the row that will be written.
//
// The propagator is a parameter rather than an otel.GetTextMapPropagator call
// in here, because it is the only input that would otherwise be reachable only
// through a process-wide singleton. Exercising the trace capture would then
// mean swapping that singleton mid-run, which every other test in the package
// shares — a leak if it is not restored, and a coin toss between parallel
// tests either way. Callers pass the global; tests pass whichever propagator
// they mean to assert about.
func buildEntry(ctx context.Context, orgID string, msg Message, propagator propagation.TextMapPropagator) (publishEntry, error) {
	if msg.Proto == nil || !msg.Proto.ProtoReflect().IsValid() {
		return publishEntry{}, oops.Permanent(fmt.Errorf("publish outbox message must not be nil"))
	}

	// Reject an undeclared topic here rather than letting the relay discover
	// it: the write is the last point at which the failure is still attached to
	// the code that caused it, instead of being a dead-lettered row. The
	// registry is generated from the proto descriptors, so a type is publishable
	// exactly when it declares a (gcp.pubsub.v1.topic) option.
	topic := proto.MessageName(msg.Proto)
	if _, ok := topics.Lookup(string(topic)); !ok {
		return publishEntry{}, oops.Permanent(fmt.Errorf("publish outbox topic %s is not a declared pubsub topic", topic))
	}

	body, err := proto.Marshal(msg.Proto)
	if err != nil {
		return publishEntry{}, fmt.Errorf("marshal publish outbox message: %w", err)
	}
	if len(body) > maxMessageBytes {
		return publishEntry{}, oops.Permanent(fmt.Errorf("publish outbox message for topic %s is %d bytes, over the %d byte limit", topic, len(body), maxMessageBytes))
	}

	publicID := msg.PublicID
	if publicID == uuid.Nil {
		publicID, err = uuid.NewV7()
		if err != nil {
			return publishEntry{}, fmt.Errorf("generate publish outbox id: %w", err)
		}
	}

	attrs := make(map[string]string, len(msg.Attributes)+2)
	maps.Copy(attrs, msg.Attributes)
	// Trace context last so a caller-supplied attribute cannot displace the link
	// back to the producing request, which is the one thing only this call site
	// can supply.
	propagator.Inject(ctx, propagation.MapCarrier(attrs))

	attributes, err := json.Marshal(attrs)
	if err != nil {
		return publishEntry{}, fmt.Errorf("marshal publish outbox attributes: %w", err)
	}

	return publishEntry{
		PublicID:       publicID,
		OrganizationID: orgID,
		Topic:          string(topic),
		Message:        body,
		Attributes:     attributes,
	}, nil
}
