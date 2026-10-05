package trialemails

import "context"

// Notifier publishes lifecycle changes that affect enterprise trial email workflows.
type Notifier interface {
	TrialStarted(ctx context.Context, organizationID string) error
	AdminAdded(ctx context.Context, organizationID, userID string) error
	TrialInactive(ctx context.Context, organizationID string) error
}
