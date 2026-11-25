package access

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/officiallink"
)

// SetShadowMCPOfficialServer overrides, or stops overriding, the automatic
// link of one Shadow MCP server to the project server that proxies to its
// URL. With mcp_server_id that server is linked; with keep_shadow nothing is
// linked; adding a removed override back restores its row. With neither,
// the override is soft-deleted and the automatic link applies again. Every
// change is recorded in the audit log; a request that changes nothing
// records nothing.
func (s *Service) SetShadowMCPOfficialServer(ctx context.Context, payload *gen.SetShadowMCPOfficialServerPayload) error {
	ac, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return err
	}

	chosen := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if rawID := strings.TrimSpace(conv.PtrValOr(payload.McpServerID, "")); rawID != "" {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, s.logger)
		}
		chosen = uuid.NullUUID{UUID: id, Valid: true}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	result, err := officiallink.NewWriter(s.audit).SetInTransaction(ctx, dbtx, officiallink.SetInput{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        projectID,
		ServerURL:        inventoryURL.CanonicalURL,
		MCPServerID:      chosen,
		KeepShadow:       conv.PtrValOr(payload.KeepShadow, false),
		ActorUserID:      ac.UserID,
		Actor:            temporaryGrantActor(ctx, ac.UserID),
		ActorDisplayName: ac.Email,
		ValidateLocked:   nil,
	})
	switch {
	case errors.Is(err, officiallink.ErrInvalidServerURL):
		return oops.E(oops.CodeBadRequest, err, "invalid shadow mcp server url").LogError(ctx, s.logger)
	case errors.Is(err, officiallink.ErrMCPServerNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, s.logger)
	case errors.Is(err, officiallink.ErrMCPServerDisabled):
		return oops.E(oops.CodeBadRequest, err, "the chosen mcp server is disabled").LogError(ctx, s.logger)
	case errors.Is(err, officiallink.ErrKeepShadowWithServer):
		return oops.E(oops.CodeBadRequest, err, "keep_shadow cannot be combined with mcp_server_id").LogError(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "save shadow mcp official server").LogError(ctx, s.logger)
	}
	if !result.Changed {
		return nil
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit shadow mcp official server change").LogError(ctx, s.logger)
	}
	return nil
}
