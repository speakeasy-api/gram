//nolint:exhaustruct // SDK optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

// SetRegistryIconsInput is a full replacement of one entry's curated icons.
type SetRegistryIconsInput struct {
	EntryID           string                   `json:"entry_id" jsonschema:"Exact registry entry UUID"`
	ExpectedUpdatedAt string                   `json:"expected_updated_at" jsonschema:"Exact updated_at revision returned by get_registry_entry"`
	Icons             mcpregistry.CatalogIcons `json:"icons" jsonschema:"Full replacement of default and dark curated SVG icons; omitted slots are removed; an empty object removes both"`
}

type registryIconsReceipt struct {
	EntryID   string                   `json:"entry_id"`
	UpdatedAt string                   `json:"updated_at"`
	Before    mcpregistry.CatalogIcons `json:"before"`
	After     mcpregistry.CatalogIcons `json:"after"`
}

type registryIconsWriter struct {
	runner   *writeRunner
	registry *mcpregistry.Service
	writes   WriteConfig
}

func (s *registryIconsWriter) apply(ctx context.Context, input SetRegistryIconsInput) (registryIconsReceipt, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationSetRegistryIcons)
	if err != nil {
		return registryIconsReceipt{}, err
	}
	if !validRegistryUUID(input.EntryID) {
		return registryIconsReceipt{}, errors.New("provide an exact entry UUID")
	}
	if _, err := mcpregistry.ParseToken(input.ExpectedUpdatedAt); err != nil {
		return registryIconsReceipt{}, fmt.Errorf("parse registry revision: %w", err)
	}
	id := uuid.MustParse(input.EntryID)
	target := writeTarget{ResourceKind: "registry_entry", ResourceID: input.EntryID}
	return runWrite(ctx, s.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (registryIconsReceipt, error) {
		// The check locks the entry and verifies the revision, so the read below
		// sees the state this change replaces.
		_, err := s.registry.CheckCatalogIconsTx(ctx, tx, id, input.ExpectedUpdatedAt, input.Icons)
		if errors.Is(err, mcpregistry.ErrConflict) {
			return registryIconsReceipt{}, ErrStaleState
		}
		if err != nil {
			return registryIconsReceipt{}, fmt.Errorf("check registry icons: %w", err)
		}
		old, err := s.registry.Get(ctx, id)
		if err != nil {
			return registryIconsReceipt{}, fmt.Errorf("get registry entry: %w", err)
		}
		updated, err := s.registry.SetCatalogIconsTx(ctx, tx, id, input.ExpectedUpdatedAt, input.Icons)
		if err != nil {
			return registryIconsReceipt{}, registryCreateError(err)
		}
		return registryIconsReceipt{EntryID: updated.ID.String(), UpdatedAt: mcpregistry.Token(updated), Before: boundedCatalogIcons(ctx, s.registry, old.Data), After: input.Icons}, nil
	})
}

func (s *registryIconsWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationSetRegistryIcons), Title: "Set Registry Icons", Description: "Replace the full curated default/dark SVG icon set at envelope _meta[com.speakeasy.ai/icons] for an exact registry entry and revision. Missing slots are removed; {} clears both. Upload public platform SVG assets with create_image_upload_url first. Does not edit arbitrary metadata or upstream server.icons."}, s.apply)
}

var _ operationWriter = (*registryIconsWriter)(nil)
