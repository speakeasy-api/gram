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

// ahpConfiguration advertises only implemented callbacks. Each event occurs
// once: intercept delivery already records the event, so adding an observer
// for the same event would duplicate ingestion.
func ahpConfiguration(failOpen bool) *ahp.Configuration {
	boundaries := []event.Type{
		event.ToolBefore, event.ToolAfter, event.ToolPermissionRequest,
		event.UserMessageInbound, event.UserMessageOutbound,
		event.ModelRequestBefore, event.ModelResponseAfter,
		event.ContextCompactBefore, event.ContextCompactAfter,
	}
	observations := []event.Type{
		event.ToolPermissionResolved, event.ToolBatchAfter, event.ModelError,
		event.TurnEnd, event.SessionStart, event.SessionEnd,
	}
	// Text is policy evidence; binary parts remain metadata. Discovery does not
	// introduce an upload destination or authorize credential forwarding.
	selection := content.NewSelection(ahp.ContentSelectionDefaultMetadata,
		content.WithSelectionText(ahp.ContentSelectionTextBody))
	failurePolicy := subscription.FailClosed
	if failOpen {
		failurePolicy = subscription.FailOpen
	}
	return ahp.NewConfiguration(
		subscription.NewIntercept(boundaries, 5*time.Second, failurePolicy, selection),
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
