package access

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	keepShadow := conv.PtrValOr(payload.KeepShadow, false)
	if keepShadow && chosen.Valid {
		return oops.E(oops.CodeBadRequest, nil, "keep_shadow cannot be combined with mcp_server_id").LogError(ctx, s.logger)
	}
	// With neither a server nor keep_shadow, the administrator hands the URL
	// back to the automatic link.
	useAutomatic := !chosen.Valid && !keepShadow

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := shadowmcprepo.New(dbtx)
	// The row lock below only holds an existing link. Concurrent first links
	// would both read no link and audit no previous state, so serialize on
	// the URL before reading.
	if err := q.LockShadowMCPOfficialServerURL(ctx, shadowmcprepo.LockShadowMCPOfficialServerURLParams{ProjectID: projectID, ServerUrl: key}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock shadow mcp official server url").LogError(ctx, s.logger)
	}
	var existing *officialLinkRow
	row, err := q.LockShadowMCPOfficialServerChoice(ctx, shadowmcprepo.LockShadowMCPOfficialServerChoiceParams{ProjectID: projectID, ServerUrl: key})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock shadow mcp official server choice").LogError(ctx, s.logger)
	default:
		existing = &officialLinkRow{ID: row.ID, ServerURL: row.ServerUrl, MCPServerID: row.McpServerID}
	}

	event := audit.ShadowMCPOfficialServerEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            temporaryGrantActor(ctx, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		LinkURN:          urn.ShadowMCPOfficialServer{ID: uuid.Nil},
		ServerURL:        key,
	}
	userID := conv.ToPGTextEmpty(ac.UserID)

	switch {
	case useAutomatic && existing == nil:
		// No override to remove: nothing changed, so nothing is recorded.
		return nil
	case useAutomatic:
		if _, err := q.DeleteShadowMCPOfficialServerChoice(ctx, shadowmcprepo.DeleteShadowMCPOfficialServerChoiceParams{UserID: userID, ProjectID: projectID, ServerUrl: key}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "remove shadow mcp official server link").LogError(ctx, s.logger)
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(existing.ID)
		err = s.audit.LogShadowMCPOfficialServerDelete(ctx, dbtx, audit.LogShadowMCPOfficialServerDeleteEvent{
			ShadowMCPOfficialServerEvent:          event,
			ShadowMCPOfficialServerSnapshotBefore: existing.snapshot(),
		})
	case existing == nil:
		saved, addErr := s.addShadowMCPOfficialServerLink(ctx, q, projectID, key, chosen, userID)
		if addErr != nil {
			return addErr
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(saved.ID)
		err = s.audit.LogShadowMCPOfficialServerCreate(ctx, dbtx, audit.LogShadowMCPOfficialServerCreateEvent{
			ShadowMCPOfficialServerEvent:         event,
			ShadowMCPOfficialServerSnapshotAfter: saved.snapshot(),
		})
	case existing.MCPServerID == chosen:
		// The override already says this.
		return nil
	default:
		saved, upsertErr := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
			ProjectID:   projectID,
			ServerUrl:   key,
			McpServerID: chosen,
			UserID:      userID,
		})
		if upsertErr != nil {
			return oops.E(oops.CodeUnexpected, upsertErr, "update shadow mcp official server link").LogError(ctx, s.logger)
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(saved.ID)
		after := officialLinkRow{ID: saved.ID, ServerURL: saved.ServerUrl, MCPServerID: saved.McpServerID}
		err = s.audit.LogShadowMCPOfficialServerUpdate(ctx, dbtx, audit.LogShadowMCPOfficialServerUpdateEvent{
			ShadowMCPOfficialServerEvent:          event,
			ShadowMCPOfficialServerSnapshotBefore: existing.snapshot(),
			ShadowMCPOfficialServerSnapshotAfter:  after.snapshot(),
		})
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "log shadow mcp official server link").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit shadow mcp official server link").LogError(ctx, s.logger)
	}
	return nil
}

// officialLinkRow is the part of a stored link the handler reads.
type officialLinkRow struct {
	ID          uuid.UUID
	ServerURL   string
	MCPServerID uuid.NullUUID
}

func (r officialLinkRow) snapshot() *audit.ShadowMCPOfficialServerSnapshot {
	snapshot := &audit.ShadowMCPOfficialServerSnapshot{
		ServerURL:   r.ServerURL,
		MCPServerID: "",
		KeepShadow:  !r.MCPServerID.Valid,
	}
	if r.MCPServerID.Valid {
		snapshot.MCPServerID = r.MCPServerID.UUID.String()
	}
	return snapshot
}

// addShadowMCPOfficialServerLink adds an override for a URL with none:
// it restores the most recently removed row for the URL, or inserts one.
func (s *Service) addShadowMCPOfficialServerLink(ctx context.Context, q *shadowmcprepo.Queries, projectID uuid.UUID, key string, chosen uuid.NullUUID, userID pgtype.Text) (officialLinkRow, error) {
	restored, err := q.RestoreShadowMCPOfficialServerChoice(ctx, shadowmcprepo.RestoreShadowMCPOfficialServerChoiceParams{
		McpServerID: chosen,
		UserID:      userID,
		ProjectID:   projectID,
		ServerUrl:   key,
	})
	if err == nil {
		return officialLinkRow{ID: restored.ID, ServerURL: restored.ServerUrl, MCPServerID: restored.McpServerID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return officialLinkRow{}, oops.E(oops.CodeUnexpected, err, "restore shadow mcp official server link").LogError(ctx, s.logger)
	}
	inserted, err := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
		ProjectID:   projectID,
		ServerUrl:   key,
		McpServerID: chosen,
		UserID:      userID,
	})
	if err != nil {
		return officialLinkRow{}, oops.E(oops.CodeUnexpected, err, "add shadow mcp official server link").LogError(ctx, s.logger)
	}
	return officialLinkRow{ID: inserted.ID, ServerURL: inserted.ServerUrl, MCPServerID: inserted.McpServerID}, nil
}
