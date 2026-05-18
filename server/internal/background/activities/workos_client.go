package activities

import (
	"context"

	"github.com/workos/workos-go/v6/pkg/events"
)

type WorkOSClient interface {
	ListEvents(ctx context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error)
	UpdateUserExternalID(ctx context.Context, workosUserID, externalID string) error
}
