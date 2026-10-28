// Package svixrelay delivers webhook events from the gram.webhooks.v1.Event
// topic to Svix.
//
// This is the consumer half of the outbox refactor. The producer writes an
// event transactionally and the relay publishes it; everything Svix-specific —
// whether an organization is eligible, what a permanent HTTP failure looks
// like, how many times to retry — lives here.
//
// Retry and dead-lettering are the subscription's job, not this handler's.
// Returning an error nacks the message, which hands it to the subscription's
// retry policy and, after enough attempts, its dead letter topic. The handler
// therefore only has to decide, for each event, whether another attempt could
// possibly succeed.
package svixrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"
	"go.opentelemetry.io/otel/metric"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterDelivered = "webhooks.svix.delivered"
	meterDropped   = "webhooks.svix.dropped"
)

// Drop reasons, recorded as a metric dimension. Deliberately low cardinality —
// organization id belongs on the log and the span, never on a metric label.
const (
	dropReasonNotEligible    = "not_eligible"
	dropReasonInvalidEvent   = "invalid_event"
	dropReasonInvalidPayload = "invalid_payload"
	dropReasonRejected       = "rejected"
	dropReasonDuplicate      = "duplicate"
)

type Handler struct {
	logger    *slog.Logger
	svix      *svix.Svix
	allowSet  *allowSet
	delivered metric.Int64Counter
	dropped   metric.Int64Counter
}

// NewHandler builds the subscriber.
func NewHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	svixClient *svix.Svix,
) *Handler {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/webhooks/svixrelay")

	delivered, err := meter.Int64Counter(
		meterDelivered,
		metric.WithDescription("Number of webhook events handed to Svix"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterDelivered), attr.SlogError(err))
	}

	dropped, err := meter.Int64Counter(
		meterDropped,
		metric.WithDescription("Number of webhook events acknowledged without delivery"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterDropped), attr.SlogError(err))
	}

	return &Handler{
		logger:    logger.With(attr.SlogComponent("svix-relay")),
		svix:      svixClient,
		allowSet:  newAllowSet(db),
		delivered: delivered,
		dropped:   dropped,
	}
}

func (h *Handler) Handle(ctx context.Context, ev *webhooksv1.Event, _ gcp.MessageMetadata) error {
	orgID := ev.GetOrganizationId()
	eventID := ev.GetEventId()

	// Neither field can be filled in by a later attempt, so this is a drop
	// rather than a nack. Without an organization there is no Svix application
	// to resolve; without an event id there is no idempotency key, and sending
	// anyway would make every redelivery a duplicate webhook rather than a 409.
	// The producer sets both, so arriving here means a message reached the topic
	// by some other route.
	if orgID == "" || eventID == "" {
		h.drop(ctx, dropReasonInvalidEvent)
		h.logger.ErrorContext(ctx, "dropping webhook event with missing identifiers",
			attr.SlogWebhookDropReason(dropReasonInvalidEvent),
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
		)
		return nil
	}

	// A gate failure is not a "no". Returning the error nacks the message so it
	// is retried, because dropping an event during a database outage would lose
	// it permanently.
	appID, err := h.allowSet.svixAppFor(ctx, orgID)
	if err != nil {
		return fmt.Errorf("resolve svix app for organization: %w", err)
	}
	if appID == "" {
		// Counted but deliberately not logged. Most events belong to
		// organizations that have never enabled webhooks, so this branch is the
		// steady state rather than an anomaly, and a line per event would be a
		// firehose that buries the drops worth reading. The metric carries the
		// same reason dimension as those log lines, so the rate stays visible.
		h.drop(ctx, dropReasonNotEligible)
		return nil
	}

	payload, err := decodePayload(ev.GetPayload())
	if err != nil {
		// A malformed payload will not become well-formed on redelivery.
		h.drop(ctx, dropReasonInvalidPayload)
		h.logger.ErrorContext(ctx, "dropping webhook event with unreadable payload",
			attr.SlogWebhookDropReason(dropReasonInvalidPayload),
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogError(err),
		)
		return nil
	}

	out, err := h.svix.Message.Create(ctx, appID, models.MessageIn{
		EventId:                &eventID,
		EventType:              ev.GetEventType(),
		Payload:                payload,
		Application:            nil,
		Channels:               nil,
		DeliverAt:              nil,
		PayloadRetentionHours:  nil,
		PayloadRetentionPeriod: nil,
		Tags:                   nil,
		TransformationsParams:  nil,
	}, &svix.MessageCreateOptions{
		IdempotencyKey: &eventID,
		WithContent:    nil,
	})
	if err == nil {
		var messageID string
		if out != nil {
			messageID = out.Id
		}
		if h.delivered != nil {
			h.delivered.Add(ctx, 1)
		}
		h.logger.InfoContext(ctx, "webhook event delivered",
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogSvixAppID(appID),
			attr.SlogSvixMessageID(messageID),
		)
		return nil
	}

	status := svixStatus(err)
	switch {
	// Svix rejects a repeated eventId for an application with 409. Because
	// event_id is stable across redeliveries, that response means this exact
	// event is already in Svix — a success we are seeing twice, not a failure.
	// The old Temporal relay classified it as a permanent 4xx and dead-lettered
	// it; doing that here would turn every at-least-once redelivery outside the
	// idempotency window into a lost event.
	case status == 409:
		h.drop(ctx, dropReasonDuplicate)
		h.logger.InfoContext(ctx, "webhook event already delivered",
			attr.SlogWebhookDropReason(dropReasonDuplicate),
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogSvixAppID(appID),
		)
		return nil

	// Svix declining to answer now rather than refusing this message, so both
	// belong with the 5xx retries below. Neither will have been retried by the
	// SDK: its loop covers transport failures and 5xx only, so a 408 arrives
	// here having been attempted exactly once.
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
		return fmt.Errorf("svix message create: %w", err)

	// The relay's own credentials being refused, not this message. A missing,
	// stale or under-scoped API key answers every Create this way, so acking
	// would quietly consume and destroy the entire topic for as long as the key
	// is wrong — and the response stops the moment it is fixed, which is the
	// opposite of the permanence the ack below relies on. Nacking hands the
	// event to the subscription's backoff, and anything that outlives the
	// delivery budget lands in the dead letter topic instead of nowhere.
	//
	// Logged per event rather than sampled: an event that reaches this branch is
	// undeliverable until an operator acts, and no other signal names the cause.
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		h.logger.ErrorContext(ctx, "svix refused the relay's credentials",
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogSvixAppID(appID),
			attr.SlogHTTPResponseStatusCode(status),
			attr.SlogError(err),
		)
		return fmt.Errorf("svix message create: %w", err)

	// Any other 4xx is a rejection of this specific message. Nacking would burn
	// the whole delivery budget re-sending something Svix has already refused.
	// Note what acking costs: this path has no dead letter, so the event is
	// discarded outright. That is only defensible for a response that will be
	// identical next time, which is why the transient codes and the credential
	// failures are split out above rather than left to an exclusion list.
	case status >= 400 && status < 500:
		h.drop(ctx, dropReasonRejected)
		h.logger.ErrorContext(ctx, "svix rejected webhook event",
			attr.SlogWebhookDropReason(dropReasonRejected),
			attr.SlogOrganizationID(orgID),
			attr.SlogOutboxPublicID(eventID),
			attr.SlogSvixAppID(appID),
			attr.SlogHTTPResponseStatusCode(status),
			attr.SlogError(err),
		)
		return nil

	// Rate limits, 5xx and transport failures are all worth another attempt.
	default:
		return fmt.Errorf("svix message create: %w", err)
	}
}

// decodePayload decodes the opaque event payload into the shape the Svix SDK
// takes.
//
// UseNumber, not a plain Unmarshal. Decoding into map[string]any turns every
// JSON number into a float64, whose 53-bit mantissa silently rounds any integer
// past 9007199254740992 and rewrites large magnitudes in exponent notation —
// 12345678901234567890 is delivered as 12345678901234567000, with no error
// anywhere. This payload is the customer's, carried through verbatim by
// contract, so it has to survive the decode and re-encode unchanged.
// json.Number holds the original literal, and the SDK marshals with
// encoding/json, which writes it back as a number rather than a quoted string.
func decodePayload(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode webhook payload: %w", err)
	}

	// Decode stops after the first value where Unmarshal rejects anything
	// trailing it. Keeping that strictness means swapping the decoder does not
	// quietly widen what counts as a valid payload.
	if dec.More() {
		return nil, errors.New("decode webhook payload: unexpected data after top-level value")
	}

	return payload, nil
}

func (h *Handler) drop(ctx context.Context, reason string) {
	if h.dropped != nil {
		h.dropped.Add(ctx, 1, metric.WithAttributes(attr.WebhookDropReason(reason)))
	}
}

// svixStatus returns the HTTP status behind a Svix SDK error, or 0 when the
// failure was not an HTTP response at all (a transport error, say).
func svixStatus(err error) int {
	var svixErr *svix.Error
	if !errors.As(err, &svixErr) {
		return 0
	}

	return svixErr.Status()
}
