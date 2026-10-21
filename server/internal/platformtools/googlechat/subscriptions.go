package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	// workspaceEventsAPIURL is fixed so subscription identifiers cannot redirect credentials.
	workspaceEventsAPIURL = "https://workspaceevents.googleapis.com/v1/"

	// subscriptionLifetime stays within Google's resource-data subscription limit.
	subscriptionLifetime = 4 * time.Hour

	// subscriptionRenewalWindow leaves two hourly sweep opportunities before expiry.
	subscriptionRenewalWindow = 2 * time.Hour
)

var subscriptionPattern = regexp.MustCompile(`^subscriptions/[A-Za-z0-9_-]+$`)

// SubscriptionRenewer maintains already-created, administrator-approved Chat subscriptions.
// It cannot create subscriptions or change their target, event types, or delivery destination.
type SubscriptionRenewer struct {
	// client sends authenticated requests only to the fixed Google API endpoints.
	client *client
}

func NewSubscriptionRenewer(httpClient *guardian.HTTPClient) *SubscriptionRenewer {
	return &SubscriptionRenewer{client: newClient(httpClient)}
}

// workspaceSubscription is the metadata needed to validate and renew a subscription.
type workspaceSubscription struct {
	// Name identifies the exact configured subscription.
	Name string `json:"name"`

	// TargetResource identifies the space monitored by the subscription.
	TargetResource string `json:"targetResource"`

	// State must be ACTIVE; suspended subscriptions need operator attention.
	State string `json:"state"`

	// ExpireTime is the provider's observed expiration, not a locally assumed lease.
	ExpireTime time.Time `json:"expireTime"`

	// Etag prevents a renewal from racing a provider-side configuration change.
	Etag string `json:"etag"`

	// EventTypes must include message creation for text commands to arrive.
	EventTypes []string `json:"eventTypes"`

	// PayloadOptions must include message content for prefix and participant filtering.
	PayloadOptions struct {
		IncludeResourceData bool `json:"includeResourceData"`
	} `json:"payloadOptions"`
}

// subscriptionRenewalRequest changes only the expiration of the configured resource.
type subscriptionRenewalRequest struct {
	// TTL requests another four hours of event delivery.
	TTL string `json:"ttl"`

	// Etag matches the metadata validated immediately before renewal.
	Etag string `json:"etag,omitempty"`
}

// Renew validates the configured space, renews subscriptions nearing expiration,
// and reads back provider state. The boolean reports whether renewal was necessary.
func (r *SubscriptionRenewer) Renew(ctx context.Context, env toolconfig.ToolCallEnv, name, space string, now time.Time) (bool, error) {
	if !subscriptionPattern.MatchString(name) || !spacePattern.MatchString(space) {
		return false, fmt.Errorf("invalid Google Chat subscription or space resource")
	}
	subscription, err := r.read(ctx, env, name, space)
	if err != nil {
		return false, err
	}
	if subscription.ExpireTime.After(now.Add(subscriptionRenewalWindow)) {
		return false, nil
	}
	body := subscriptionRenewalRequest{TTL: fmt.Sprintf("%ds", int64(subscriptionLifetime.Seconds())), Etag: subscription.Etag}
	raw, err := r.client.callAPI(ctx, env, historyScope, workspaceEventsAPIURL, http.MethodPatch, name, url.Values{"updateMask": {"ttl"}}, body)
	if err != nil {
		return false, fmt.Errorf("renew Google Chat subscription: %w", err)
	}
	var operation struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &operation); err != nil {
		return false, fmt.Errorf("decode subscription renewal operation: %w", err)
	}
	if operation.Error != nil {
		return false, fmt.Errorf("google chat subscription renewal operation failed with code %d", operation.Error.Code)
	}
	// PATCH returns an operation, not proof of an extended lease. An in-progress
	// operation that hasn't updated the resource yet is retried by Temporal.
	renewed, err := r.read(ctx, env, name, space)
	if err != nil {
		return false, err
	}
	if !renewed.ExpireTime.After(now.Add(subscriptionRenewalWindow)) {
		return false, fmt.Errorf("google chat subscription renewal has not extended its expiration")
	}
	return true, nil
}

func (r *SubscriptionRenewer) read(ctx context.Context, env toolconfig.ToolCallEnv, name, space string) (*workspaceSubscription, error) {
	raw, err := r.client.callAPI(ctx, env, historyScope, workspaceEventsAPIURL, http.MethodGet, name, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("read Google Chat subscription: %w", err)
	}
	var subscription workspaceSubscription
	if err := json.Unmarshal(raw, &subscription); err != nil {
		return nil, fmt.Errorf("decode Google Chat subscription: %w", err)
	}
	if subscription.Name != name || subscription.TargetResource != "//chat.googleapis.com/"+space ||
		!slices.Contains(subscription.EventTypes, "google.workspace.chat.message.v1.created") || !subscription.PayloadOptions.IncludeResourceData {
		return nil, fmt.Errorf("google chat subscription does not match the configured space and message delivery")
	}
	if subscription.State != "ACTIVE" || subscription.ExpireTime.IsZero() {
		return nil, fmt.Errorf("google chat subscription is not active or has no expiration; check app authorization and membership")
	}
	return &subscription, nil
}
