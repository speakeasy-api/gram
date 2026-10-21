package activities

import (
	"context"
	"fmt"
	"time"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/googlechat"
)

// RenewGoogleChatSubscriptions owns provider credentials inside an activity,
// keeping them out of Temporal inputs, results, and history.
type RenewGoogleChatSubscriptions struct {
	// app loads active trigger configuration and credentials.
	app *bgtriggers.App
	// renewer performs and verifies the Google API update.
	renewer *googlechat.SubscriptionRenewer
}

func NewRenewGoogleChatSubscriptions(app *bgtriggers.App, policy *guardian.Policy) *RenewGoogleChatSubscriptions {
	var httpClient *guardian.HTTPClient
	if policy != nil {
		httpClient = policy.PooledClient()
	}
	return &RenewGoogleChatSubscriptions{app: app, renewer: googlechat.NewSubscriptionRenewer(httpClient)}
}

func (r *RenewGoogleChatSubscriptions) Do(ctx context.Context, now time.Time) error {
	if r.app == nil {
		return fmt.Errorf("google chat trigger app is not configured")
	}
	if err := r.app.RenewGoogleChatSubscriptions(ctx, now, r.renewer.Renew); err != nil {
		return fmt.Errorf("renew Google Chat subscriptions: %w", err)
	}
	return nil
}
