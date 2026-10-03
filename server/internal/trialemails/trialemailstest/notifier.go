// Package trialemailstest provides trial email test doubles.
package trialemailstest

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/trialemails"
)

var _ trialemails.Notifier = NoopNotifier{}

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
