package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/agenthooksprotocol/go-sdk/content"
	"github.com/agenthooksprotocol/go-sdk/event"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
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

// The SDK snapshots static configuration. Keep prevalidated variants, but
// choose on each authenticated discovery request so posture changes
// do not require restarting the server. Event dispatch keeps its existing path.
func (s *Service) ahpConfigurationHandler(next http.Handler) (http.Handler, error) {
	var variants [2]http.Handler
	for i := range variants {
		h, err := ahpserver.NewHandler(ahpserver.Handlers{Configuration: ahpConfiguration(i == 1)}, //nolint:exhaustruct // discovery-only handler; event requests use next
			ahpserver.Options{MaxRequestBytes: 1 << 20, MaxResponseBytes: 64 << 10, Diagnostic: nil})
		if err != nil {
			return nil, fmt.Errorf("create AHP configuration handler: %w", err)
		}
		variants[i] = h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func(body io.ReadCloser) { _ = body.Close() }(r.Body)
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			status := http.StatusBadRequest
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var envelope struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Method != "hooks/configuration" {
			next.ServeHTTP(w, r)
			return
		}
		auth, ok := contextvalues.GetAuthContext(r.Context())
		if !ok || auth.ProjectID == nil || auth.ActiveOrganizationID == "" {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		// Unknown settings must not become a successful advertisement of the wrong failure policy.
		if s.db == nil {
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		repo := riskrepo.New(s.db)
		failOpen, err := repo.IsOrganizationHooksFailOpenEnabled(ctx, auth.ActiveOrganizationID)
		if err != nil {
			s.logger.WarnContext(ctx, "read AHP configuration fail-open setting",
				attr.SlogError(err),
				attr.SlogOrganizationID(auth.ActiveOrganizationID),
			)
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		variant := 0
		if failOpen {
			variant = 1
		}
		variants[variant].ServeHTTP(w, r)
	}), nil
}
