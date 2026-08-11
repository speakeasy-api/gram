package trialemails

import "context"

// Notifier publishes lifecycle changes that affect enterprise trial email workflows.
type Notifier interface {
	TrialStarted(ctx context.Context, organizationID string) error
	AdminAdded(ctx context.Context, organizationID, userID string) error
	TrialInactive(ctx context.Context, organizationID string) error
}

// NoopNotifier drops lifecycle notifications.
type NoopNotifier struct{}

func (NoopNotifier) TrialStarted(context.Context, string) error {
	return nil
}

func (NoopNotifier) AdminAdded(context.Context, string, string) error {
	return nil
}

func (NoopNotifier) TrialInactive(context.Context, string) error {
	return nil
}
