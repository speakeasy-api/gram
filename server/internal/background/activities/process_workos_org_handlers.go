package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// workosOrganizationEvent is the subset of the WorkOS organization event payload
// needed by the org sync handlers.
type workosOrganizationEvent struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Name       string `json:"name"`
	ExternalID string `json:"external_id"`
}

// HandleWorkOSOrganizationEvent dispatches an organization.* WorkOS event to the
// appropriate handler. The event must already be authenticated and decoded.
// Caller is responsible for transaction lifecycle.
func HandleWorkOSOrganizationEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	switch event.Event {
	case string(workos.EventKindOrganizationCreated),
		string(workos.EventKindOrganizationUpdated):
		return handleOrganizationCreatedOrUpdated(ctx, dbtx, event)
	case string(workos.EventKindOrganizationDeleted):
		return handleOrganizationDeleted(ctx, logger, dbtx, event)
	}

	return fmt.Errorf("unhandled workos organization event: %s", event.Event)
}

// handleOrganizationCreatedOrUpdated will create or update an organization internally.
// It first looks up an existing Gram org mapped to the WorkOS organization ID. If it
// cannot be found, it falls back to the WorkOS external_id (which Speakeasy is
// expected to set to the Gram org UUID). It then upserts the metadata for that org.
func handleOrganizationCreatedOrUpdated(ctx context.Context, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization event: %w", err)
	}

	repo := orgrepo.New(dbtx)

	organizationID, err := repo.GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Not yet mapped to an internal org. external_id must be set by Speakeasy
		// to the Gram org UUID. If empty we fail loudly — this is a wiring bug, not
		// a graceful edge case.
		if payload.ExternalID == "" {
			return fmt.Errorf("workos organization %q has no external_id", payload.ID)
		}
		organizationID = payload.ExternalID
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.ID, err)
	}

	if _, err := repo.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       organizationID,
		Name:     payload.Name,
		Slug:     conv.ToSlug(payload.Name),
		WorkosID: conv.ToPGText(payload.ID),
	}); err != nil {
		return fmt.Errorf("upsert organization %q from workos event: %w", payload.ID, err)
	}

	return nil
}

func handleOrganizationDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization delete event: %w", err)
	}

	rows, err := orgrepo.New(dbtx).DisableOrganizationByWorkosID(ctx, conv.ToPGText(payload.ID))
	if err != nil {
		return fmt.Errorf("disable organization %q: %w", payload.ID, err)
	}
	if rows == 0 {
		logger.DebugContext(ctx, "skipping organization delete for unknown org", attr.SlogWorkOSOrganizationID(payload.ID))
	}

	return nil
}
