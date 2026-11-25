package growthsignals_test

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/growthsignals"
)

// capturedEvent is one call the fake PostHog client recorded.
type capturedEvent struct {
	Name       string
	DistinctID string
	Properties map[string]any
}

// capturePostHog records what would have been sent to PostHog. PostHogClient is
// our own interface rather than a vendor type, so a capture client is the right
// double here: it lets a test assert on the exact payload.
type capturePostHog struct {
	mu       sync.Mutex
	events   []capturedEvent
	failWith error
}

func (c *capturePostHog) CaptureEvent(_ context.Context, eventName string, distinctID string, eventProperties map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, capturedEvent{
		Name:       eventName,
		DistinctID: distinctID,
		Properties: eventProperties,
	})

	return c.failWith
}

func (c *capturePostHog) Captured() []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.events)
}

// fakeEnricher answers lookups from fixed values, and can fail any of the three
// independently so a test can prove one failed lookup does not cost the others.
type fakeEnricher struct {
	organization    growthsignals.OrganizationDetails
	organizationErr error

	project    growthsignals.ProjectDetails
	projectErr error

	userEmails   map[string]string
	userEmailErr error

	mu             sync.Mutex
	userEmailCalls []string
}

func (f *fakeEnricher) Organization(_ context.Context, _ string) (growthsignals.OrganizationDetails, error) {
	if f.organizationErr != nil {
		return growthsignals.OrganizationDetails{}, f.organizationErr
	}

	return f.organization, nil
}

func (f *fakeEnricher) Project(_ context.Context, _ uuid.UUID) (growthsignals.ProjectDetails, error) {
	if f.projectErr != nil {
		return growthsignals.ProjectDetails{}, f.projectErr
	}

	return f.project, nil
}

func (f *fakeEnricher) UserEmail(_ context.Context, userID string) (string, error) {
	f.mu.Lock()
	f.userEmailCalls = append(f.userEmailCalls, userID)
	f.mu.Unlock()

	if f.userEmailErr != nil {
		return "", f.userEmailErr
	}

	return f.userEmails[userID], nil
}

func (f *fakeEnricher) UserEmailCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.userEmailCalls)
}
