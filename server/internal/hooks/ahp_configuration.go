package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/agenthooksprotocol/go-sdk/content"
	"github.com/agenthooksprotocol/go-sdk/event"
	"github.com/agenthooksprotocol/go-sdk/subscription"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ahpDefaultInterceptTimeout is a transport default, not an organization setting.
const ahpDefaultInterceptTimeout = 10 * time.Second

// ahpConfiguration advertises the default harness subscriptions. Each event occurs
// once: intercept delivery already records the event, so adding an observer
// for the same event would duplicate ingestion.
func ahpConfiguration(failOpen bool, uploadURL string) *ahp.Configuration {
	boundaries := []event.Type{event.UserMessageInbound, event.ToolBefore}
	observations := []event.Type{
		event.SessionStart, event.SessionEnd, event.TurnEnd,
		event.ModelResponseAfter, event.ToolAfter,
	}
	// Upload authentication uses the HTTP transport's configured headers. Never
	// advertise credential values or invent a separate client auth binding.
	upload := content.NewUpload(uploadURL, json.Number(strconv.Itoa(ahpMaxUploadBytes)), ahpDefaultInterceptTimeout)
	selection := content.NewSelection(ahp.ContentSelectionDefaultBody)
	failurePolicy := subscription.FailClosed
	if failOpen {
		failurePolicy = subscription.FailOpen
	}
	return ahp.NewConfiguration(
		subscription.NewIntercept(boundaries, ahpDefaultInterceptTimeout, failurePolicy, selection, subscription.WithInterceptUpload(upload)),
		subscription.NewObserve(selection, observations, subscription.WithObserveUpload(upload)),
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
	// Resolve only deployment-configured origins for the authenticated organization.
	// Request Host and forwarding headers must never redirect upload credentials.
	base := s.serverURL
	if s.orgHosts != nil {
		base = s.orgHosts.ServerURL(s.orgDefaultHost(ctx, auth.ActiveOrganizationID))
	}
	if base == nil || base.Host == "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errors.New("AHP upload origin unavailable")
	}
	endpoint := *base
	endpoint.Path = ahpUploadPath
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.ForceQuery = false
	endpoint.Fragment = ""
	endpoint.RawFragment = ""
	return ahpConfiguration(failOpen, endpoint.String()), nil
}
