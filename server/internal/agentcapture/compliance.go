package agentcapture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	airepo "github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// pollClaudeCompliance imports Claude Chat (claude.ai web/desktop)
// transcripts through the production compliance import pipeline. Unlike the
// Admin Analytics pollers, whose rows land in ClickHouse telemetry_logs, the
// compliance import writes chats and messages to Postgres (chats /
// chat_messages) and persists per-chat pagination cursors keyed by a real
// ai_integration_configs row — ai_integration_config_chats has a foreign key
// on it — so the uuid.Nil stateless-config trick the analytics leg uses
// cannot work here; the capture ensures a local config row instead.
//
// The run stays stateless at the feed level: LastCursor is always empty, so
// every run walks the import's bounded backfill window (the watermark minus
// its 24h margin) instead of resuming a stored feed position. Chat upserts
// and message inserts are idempotent and per-chat message cursors persist,
// so re-running over the same window is cheap.
func (s *Service) pollClaudeCompliance(ctx context.Context, project projectsrepo.Project, opts Options, since time.Time) error {
	configID, err := s.ensureComplianceConfig(ctx, project, opts)
	if err != nil {
		return err
	}

	// External message imports never upload assets, so no blob store is
	// needed.
	writer, shutdown := chat.NewChatMessageWriter(s.logger, s.db, nil)
	defer o11y.NoLogDefer(func() error { return shutdown(ctx) })

	// The heartbeat exists for Temporal activity liveness; a one-shot CLI
	// run has nothing to keep alive.
	heartbeat := func(context.Context, string, int) {}
	importer := aiintegrations.NewComplianceImportService(s.logger, s.db, s.guardianPolicy, writer, heartbeat)

	cfg := aiintegrations.Config{
		ID:                     configID,
		SyncID:                 uuid.Nil,
		OrganizationID:         project.OrganizationID,
		Provider:               aiintegrations.ProviderAnthropicCompliance,
		ProjectID:              project.ID,
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

	// The returned feed cursor is deliberately dropped: persisting it would
	// make the next run walk forward from this one's position instead of
	// re-covering the requested lookback window.
	if _, err := importer.SyncAnthropicCompliance(ctx, cfg); err != nil {
		return fmt.Errorf("sync anthropic compliance: %w", err)
	}
	return nil
}

// ensureComplianceConfig returns the id of the organization's
// anthropic_compliance integration config, creating a local one when none
// exists. An existing config — e.g. one configured through the dashboard —
// is reused as-is; the capture's api-key and external-org-id flags shape
// only the in-memory Config for this run, never the stored row.
func (s *Service) ensureComplianceConfig(ctx context.Context, project projectsrepo.Project, opts Options) (uuid.UUID, error) {
	q := airepo.New(s.db)
	id, err := q.GetConfigIDByOrgAndProvider(ctx, airepo.GetConfigIDByOrgAndProviderParams{
		OrganizationID: project.OrganizationID,
		Provider:       aiintegrations.ProviderAnthropicCompliance,
	})
	switch {
	case err == nil:
		return id, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, fmt.Errorf("load anthropic compliance config: %w", err)
	}

	// The key is encrypted properly (not a placeholder) so the local
	// dashboard's integrations views can still decrypt the row.
	encrypted, err := s.enc.Encrypt([]byte(opts.APIKey))
	if err != nil {
		return uuid.Nil, fmt.Errorf("encrypt capture api key: %w", err)
	}
	// Enabled is false and no sync schedule rows are created, so the local
	// Temporal worker never starts polling with the stored key behind the
	// operator's back; the import runs against the in-memory Config, which
	// consults neither.
	inserted, err := q.InsertConfig(ctx, airepo.InsertConfigParams{
		OrganizationID:         project.OrganizationID,
		Provider:               aiintegrations.ProviderAnthropicCompliance,
		ProjectID:              project.ID,
		ExternalOrganizationID: conv.ToPGText(opts.ExternalOrgID),
		ApiKeyEncrypted:        encrypted,
		Enabled:                false,
		BillingMode:            pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert anthropic compliance config: %w", err)
	}
	return inserted.ID, nil
}
