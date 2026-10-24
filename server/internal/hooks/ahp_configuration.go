package hooks

import (
	"context"
	"errors"
	"fmt"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/agenthooksprotocol/go-sdk/content"
	"github.com/agenthooksprotocol/go-sdk/event"
	"github.com/agenthooksprotocol/go-sdk/subscription"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ahpConfiguration advertises the default harness subscriptions. Each event occurs
// once: intercept delivery already records the event, so adding an observer
// for the same event would duplicate ingestion.
func ahpConfiguration(failOpen bool) *ahp.Configuration {
	boundaries := []event.Type{event.UserMessageInbound, event.ToolBefore}
	observations := []event.Type{
		event.SessionStart, event.SessionEnd, event.TurnStart, event.TurnEnd,
		event.ModelResponseAfter, event.ToolAfter,
	}
	// TurnStart observation is acknowledged by the SDK without ingestion.
	// Discovery does not introduce uploads or authorize credential forwarding.
	selection := content.NewSelection(ahp.ContentSelectionDefaultBody)
	failurePolicy := subscription.FailClosed
	if failOpen {
		failurePolicy = subscription.FailOpen
	}
	return ahp.NewConfiguration(
		subscription.NewIntercept(boundaries, 30*time.Second, failurePolicy, selection),
		subscription.NewObserve(selection, observations),
	)
}

// Resolve the authenticated organization's current posture on each discovery request.
func (s *Service) ahpResolveConfiguration(ctx context.Context) (*ahp.Configuration, error) {
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth.ProjectID == nil || auth.ActiveOrganizationID == "" {
		return nil, errors.New("AHP configuration requires authenticated organization")
	}
	// Unknown settings must not become a successful advertisement of the wrong failure policy.
	if s.db == nil {
		return nil, errors.New("AHP configuration database unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	repo := riskrepo.New(s.db)
	failOpen, err := repo.IsOrganizationHooksFailOpenEnabled(ctx, auth.ActiveOrganizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read AHP configuration fail-open setting",
			attr.SlogError(err),
			attr.SlogOrganizationID(auth.ActiveOrganizationID),
		)
		return nil, fmt.Errorf("read AHP configuration fail-open setting: %w", err)
	}
	return ahpConfiguration(failOpen), nil
}
