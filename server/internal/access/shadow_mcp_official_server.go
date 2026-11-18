package access

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetShadowMCPOfficialServer links one Shadow MCP server to its official
// version, switches the link off or on, or removes it. Without
// mcp_server_id and with allow_direct_access false, the link is removed,
// which leaves the Shadow MCP server unmanaged so another server can be
// linked. Without mcp_server_id and with allow_direct_access true, the
// request is rejected: switching a link off needs the linked server.
func (s *Service) SetShadowMCPOfficialServer(ctx context.Context, payload *gen.SetShadowMCPOfficialServerPayload) error {
	ac, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return err
	}
	key, ok := shadowmcp.OfficialServerKey(inventoryURL.CanonicalURL)
	if !ok {
		return oops.E(oops.CodeBadRequest, nil, "invalid shadow mcp server url").LogError(ctx, s.logger)
	}

	chosen := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if rawID := strings.TrimSpace(conv.PtrValOr(payload.McpServerID, "")); rawID != "" {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, s.logger)
		}
		server, err := shadowmcprepo.New(s.db).GetLiveMCPServerForShadowMCPChoice(ctx, shadowmcprepo.GetLiveMCPServerForShadowMCPChoiceParams{ID: id, ProjectID: projectID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, nil, "mcp server not found").LogError(ctx, s.logger)
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, s.logger)
		}
		// A disabled server serves nobody, so it cannot stand in for the
		// server people use today.
		if server.Visibility == "disabled" {
			return oops.E(oops.CodeBadRequest, nil, "the chosen mcp server is disabled").LogError(ctx, s.logger)
		}
		chosen = uuid.NullUUID{UUID: server.ID, Valid: true}
	}
	// Switching a link off keeps the linked server; without one there is
	// nothing to switch off.
	if !chosen.Valid && payload.AllowDirectAccess {
		return oops.E(oops.CodeBadRequest, nil, "switching a link off needs the linked mcp server").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := shadowmcprepo.New(dbtx)
	var before *audit.ShadowMCPOfficialServerSnapshot
	choiceID := uuid.Nil
	existing, err := q.LockShadowMCPOfficialServerChoice(ctx, shadowmcprepo.LockShadowMCPOfficialServerChoiceParams{ProjectID: projectID, ServerUrl: key})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock shadow mcp official server choice").LogError(ctx, s.logger)
	default:
		before = officialServerSnapshot(existing)
		choiceID = existing.ID
	}

	var after *audit.ShadowMCPOfficialServerSnapshot
	userID := conv.ToPGTextEmpty(ac.UserID)
	removeLink := !chosen.Valid
	if removeLink {
		if before == nil {
			// No link to remove: nothing changed, so nothing is recorded.
			return nil
		}
		if _, err := q.DeleteShadowMCPOfficialServerChoice(ctx, shadowmcprepo.DeleteShadowMCPOfficialServerChoiceParams{UserID: userID, ProjectID: projectID, ServerUrl: key}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "clear shadow mcp official server choice").LogError(ctx, s.logger)
		}
	}
	if !removeLink {
		saved, err := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
			ProjectID:         projectID,
			ServerUrl:         key,
			McpServerID:       chosen,
			AllowDirectAccess: payload.AllowDirectAccess,
			UserID:            userID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "save shadow mcp official server choice").LogError(ctx, s.logger)
		}
		after = officialServerSnapshot(saved)
		choiceID = saved.ID
		if before != nil && *before == *after {
			return nil
		}
	}

	if err := s.audit.LogShadowMCPOfficialServerSet(ctx, dbtx, audit.LogShadowMCPOfficialServerSetEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            temporaryGrantActor(ctx, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		ChoiceURN:        urn.NewShadowMCPOfficialServer(choiceID),
		ServerURL:        key,
		SnapshotBefore:   before,
		SnapshotAfter:    after,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log shadow mcp official server choice").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit shadow mcp official server choice").LogError(ctx, s.logger)
	}
	return nil
}

func officialServerSnapshot(row shadowmcprepo.ShadowMcpOfficialServer) *audit.ShadowMCPOfficialServerSnapshot {
	snapshot := &audit.ShadowMCPOfficialServerSnapshot{
		ServerURL:         row.ServerUrl,
		MCPServerID:       "",
		AllowDirectAccess: row.AllowDirectAccess,
	}
	if row.McpServerID.Valid {
		snapshot.MCPServerID = row.McpServerID.UUID.String()
	}
	return snapshot
}
