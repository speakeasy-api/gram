package agentcapture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// pollClaude runs the Anthropic Admin Analytics pollers (usage tokens and
// cost) in-process against an ephemeral config, exactly as the Temporal
// worker runs them, so rows land in telemetry_logs through the production
// ingest path. The config's SyncID is uuid.Nil: watermark advances update no
// ai_integration_syncs row, which makes the run stateless — the window is
// bounded purely by the checkpoint seeded from since.
//
// One schedule failing (e.g. the key lacks analytics access) does not stop
// the other; failures are joined and returned after both ran.
func (s *Service) pollClaude(ctx context.Context, projectID uuid.UUID, organizationID string, opts Options, since, until time.Time) error {
	cfg := aiintegrations.Config{
		ID:     uuid.Nil,
		SyncID: uuid.Nil,
		// The Anthropic Admin Analytics endpoints infer the organization from
		// the org-scoped admin key, so the external org ID is optional here;
		// when supplied it is stamped on rows as gram.external_org_id, and
		// org-scoped feeds added later (the Compliance API) require it.
		OrganizationID:         organizationID,
		Provider:               aiintegrations.ProviderAnthropicCompliance,
		ProjectID:              projectID,
		ExternalOrganizationID: conv.PtrEmpty(opts.ExternalOrgID),
		BillingMode:            "",
		APIKey:                 opts.APIKey,
		Enabled:                true,
		PollWatermarkAt:        since,
		PollCheckpoint:         timewindowpoller.CompletedCheckpoint(since),
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}

	// The heartbeat exists for Temporal activity liveness; a one-shot CLI
	// run has nothing to keep alive.
	heartbeat := func(context.Context, string, int) {}

	pollers := []struct {
		name   string
		poller *aiintegrations.AnthropicAnalyticsPoller
	}{
		{"anthropic_analytics_usage", aiintegrations.NewAnthropicUsageAnalyticsPoller(s.store, s.guardianPolicy, s.telemetryLogger, heartbeat)},
		{"anthropic_analytics_cost", aiintegrations.NewAnthropicCostAnalyticsPoller(s.store, s.guardianPolicy, s.telemetryLogger, heartbeat)},
	}

	var errs []error
	for _, p := range pollers {
		before := s.written.count()
		if err := p.poller.Sync(ctx, cfg, until); err != nil {
			s.logger.ErrorContext(ctx, "provider poll failed", attr.SlogError(err))
			errs = append(errs, fmt.Errorf("sync %s: %w", p.name, err))
			continue
		}
		s.logger.InfoContext(ctx, "provider poll complete",
			attr.SlogTelemetryCHRowCount(s.written.count()-before),
		)
	}
	return errors.Join(errs...)
}
