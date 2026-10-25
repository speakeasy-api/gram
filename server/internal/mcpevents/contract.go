// Package mcpevents implements recipient-scoped MCP event webhooks.
// The wire contract follows experimental-ext-triggers-events at
// d5316be214be2e76e51462069c1d67e00a5a7619, restricted to webhook delivery.
package mcpevents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// ReviewRequested reports new pending demand for an administrator's review.
	ReviewRequested = "review.requested"
	// ReviewDecided reports a decision to the members who requested the review.
	ReviewDecided = "review.decided"
	// DefaultTTL bounds unattended subscriptions while allowing daily renewal.
	DefaultTTL = 24 * time.Hour
	// MinimumTTL prevents excessive subscription refresh requests.
	MinimumTTL = time.Minute
	// RotationWindow lets in-flight deliveries survive a signing-key refresh.
	RotationWindow = 5 * time.Minute
)

// ErrDenied suppresses delivery when a subscription owner loses access.
var ErrDenied = errors.New("event subscription access denied")

// ErrNotFound identifies an unsupported event type.
var ErrNotFound = errors.New("unknown event name")

// ErrLimit reports the active subscription quota.
var ErrLimit = errors.New("event subscription limit reached")

// Owner is a server-authenticated OAuth connection, never caller-supplied JSON.
type Owner struct {
	// OrganizationID is the connection's tenant.
	OrganizationID string `json:"organization_id"`
	// UserID is the current connection subject.
	UserID string `json:"user_id"`
	// ConnectionID is the revocable OAuth connection.
	ConnectionID uuid.UUID `json:"connection_id"`
	// Generation changes when the connection is reauthorized.
	Generation uuid.UUID `json:"generation"`
	// ClientID identifies the registered OAuth client.
	ClientID string `json:"client_id"`
}

// Authorizer checks live connection, membership, project, and event permissions.
type Authorizer interface {
	// Authorize returns ErrDenied for permanent refusal, other errors for unavailable checks.
	Authorize(context.Context, Owner, uuid.UUID, string, uuid.UUID) error

	// WithTx keeps all live database checks on the transaction already held by the caller.
	WithTx(pgx.Tx) Authorizer
}

// Arguments deliberately supports only an exact project in the initial profile.
type Arguments struct {
	// ProjectID is the explicit project returned by list_projects.
	ProjectID uuid.UUID `json:"project_id"`
}

// UnmarshalJSON rejects filters the server cannot enforce instead of silently
// broadening a subscription when a client misspells an argument.
func (a *Arguments) UnmarshalJSON(data []byte) error {
	type decoded Arguments
	var value decoded
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode event arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid event arguments")
	}
	*a = Arguments(value)
	return nil
}

// Destination is the draft webhook delivery configuration.
type Destination struct {
	// Mode must be webhook.
	Mode string `json:"mode"`
	// URL is the verified HTTPS callback.
	URL string `json:"url"`
	// Secret is a client-generated Standard Webhooks key; absent on unsubscribe.
	Secret string `json:"secret,omitempty"`
}

// SubscriptionInput is shared by subscribe and unsubscribe.
type SubscriptionInput struct {
	// Name identifies the event type.
	Name string `json:"name"`
	// Arguments narrows the event stream to a project.
	Arguments Arguments `json:"arguments"`
	// Delivery is the callback configuration.
	Delivery Destination `json:"delivery"`
	// TTLMs is a requested lifetime; nil selects the finite server default.
	TTLMs *int64 `json:"ttlMs,omitempty"`
	// Cursor is unsupported: this profile has no event history replay.
	Cursor *string `json:"cursor"`
}

// SubscriptionResult reports the persisted lifetime and absence of replay.
type SubscriptionResult struct {
	// ID is the deterministic identity used to route webhook deliveries.
	ID string `json:"id"`
	// RefreshBefore is the expiry the server granted.
	RefreshBefore time.Time `json:"refreshBefore"`
	// Cursor is always null because replay is unsupported.
	Cursor *string `json:"cursor"`
	// Truncated reports a supplied replay cursor that cannot be honored.
	Truncated bool `json:"truncated"`
}

// ReviewData contains only references for an authorized follow-up read.
type ReviewData struct {
	// ProjectID scopes the referenced review.
	ProjectID uuid.UUID `json:"project_id"`
	// RequestID is accepted by get_my_mcp_review_request.
	RequestID uuid.UUID `json:"request_id"`
	// DecisionID identifies this decision occurrence when one exists.
	DecisionID string `json:"decision_id,omitempty"`
}

// Event is a single draft webhook application event, not a JSON-RPC envelope.
type Event struct {
	// EventID remains stable across retries and receiver deduplication.
	EventID uuid.UUID `json:"eventId"`
	// Name is the subscribed event name.
	Name string `json:"name"`
	// Timestamp is the domain occurrence time.
	Timestamp time.Time `json:"timestamp"`
	// Data is the privacy-minimized review reference.
	Data ReviewData `json:"data"`
	// Cursor is null because the initial profile does not retain replay history.
	Cursor *string `json:"cursor"`
}

// SubscriptionID binds canonical typed arguments and the authenticated connection.
// Struct encoding gives project arguments a single ordering and spelling.
func SubscriptionID(owner Owner, input SubscriptionInput) string {
	identity := struct {
		Owner     Owner     `json:"owner"`
		Name      string    `json:"name"`
		URL       string    `json:"url"`
		Arguments Arguments `json:"arguments"`
	}{Owner: owner, Name: input.Name, URL: input.Delivery.URL, Arguments: input.Arguments}
	data, _ := json.Marshal(identity)
	sum := sha256.Sum256(data)
	return "sub_" + hex.EncodeToString(sum[:])
}
