package activities

import (
	"context"
	"fmt"
	"time"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

// RenewGoogleChatSubscriptions maintains hosted Google Chat leases outside assistant runs.
type RenewGoogleChatSubscriptions struct{ app *bgtriggers.App }

func NewRenewGoogleChatSubscriptions(app *bgtriggers.App) *RenewGoogleChatSubscriptions {
	return &RenewGoogleChatSubscriptions{app: app}
}
func (r *RenewGoogleChatSubscriptions) Do(ctx context.Context, now time.Time) error {
	if r.app == nil {
		return fmt.Errorf("google chat trigger app is not configured")
	}
	if err := r.app.RenewManagedGoogleChat(ctx, now); err != nil {
		return fmt.Errorf("renew hosted Google subscriptions: %w", err)
	}
	return nil
}
