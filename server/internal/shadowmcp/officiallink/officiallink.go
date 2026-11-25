// Package officiallink writes an administrator's choice of a Shadow MCP
// server's official version. The dashboard endpoint and the Platform MCP tool
// share this one write path, so validation, locking and the audit record
// cannot drift apart.
//
// A choice overrides the automatic link to the project server that proxies
// to the Shadow MCP server's URL: it names another server, or keeps the
// Shadow MCP server in direct use. Removing the choice hands the URL back to
// the automatic link.
package officiallink

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mcpServerVisibilityDisabled is the visibility of a server that serves
// nobody.
const mcpServerVisibilityDisabled = "disabled"

// The kinds of choice an administrator can make for one upstream URL.
const (
	// ChoiceAutomatic means no choice: the automatic link applies.
	ChoiceAutomatic = "automatic"

	// ChoiceServer means an administrator made a server official.
	ChoiceServer = "server"

	// ChoiceKeepShadow means an administrator keeps the Shadow MCP server in
	// direct use: no server is official, not even automatically.
	ChoiceKeepShadow = "keep_shadow"
)

var (
	// ErrInvalidServerURL means the Shadow MCP server URL cannot be keyed.
	ErrInvalidServerURL = errors.New("invalid shadow mcp server url")

	// ErrMCPServerNotFound means the chosen MCP server is not live in the
	// project.
	ErrMCPServerNotFound = errors.New("mcp server not found")

	// ErrMCPServerDisabled means the chosen MCP server is disabled.
	ErrMCPServerDisabled = errors.New("the chosen mcp server is disabled")

	// ErrKeepShadowWithServer means a change both named a server and kept
	// the Shadow MCP server in direct use.
	ErrKeepShadowWithServer = errors.New("keep_shadow cannot be combined with mcp_server_id")
)

// Choice is the administrator's choice for one upstream URL, read the way
// the inventory reads it: a choice whose server was deleted or disabled
// reads as no choice, so the automatic link applies again.
type Choice struct {
	// Kind is ChoiceAutomatic, ChoiceServer or ChoiceKeepShadow.
	Kind string

	// MCPServerID is the chosen server for ChoiceServer, else uuid.Nil.
	MCPServerID uuid.UUID
}

// Visible is the choice as the inventory reports it. Without automatic
// links, keeping a server in direct use has no effect, so it reads as no
// choice.
func (c Choice) Visible(automaticLinks bool) Choice {
	if c.Kind == ChoiceKeepShadow && !automaticLinks {
		return automaticChoice()
	}
	return c
}

// SetInput is one change to a Shadow MCP server's official version. With
// MCPServerID that server is made official; with KeepShadow no server is;
// with neither, the choice is removed and the automatic link applies again.
type SetInput struct {
	OrganizationID string
	ProjectID      uuid.UUID

	// ServerURL is the Shadow MCP server's URL in any form the inventory
	// accepts; it is stored under shadowmcp.OfficialServerKey.
	ServerURL string

	// MCPServerID is the server to make official.
	MCPServerID uuid.NullUUID

	// KeepShadow keeps the Shadow MCP server in direct use. It cannot be
	// combined with MCPServerID.
	KeepShadow bool

	// ActorUserID is recorded as the row's author.
	ActorUserID string

	// Actor and ActorDisplayName are recorded in the audit log.
	Actor            urn.Principal
	ActorDisplayName *string

	// ValidateLocked sees the stored key and the current choice after the
	// row is locked and before any write. An error it returns aborts the
	// change unchanged.
	ValidateLocked func(key string, current Choice) error
}

// SetResult reports what a change did.
type SetResult struct {
	// ServerKey is the stored form of the Shadow MCP server's URL.
	ServerKey string

	// Before is the choice the change replaced.
	Before Choice

	// After is the choice once the change applies.
	After Choice

	// Changed is false when the request matched the stored choice, so
	// nothing was written or audited.
	Changed bool
}

// Writer applies choices inside a caller-owned transaction.
type Writer struct {
	audit *audit.Logger
}

func NewWriter(auditLogger *audit.Logger) *Writer {
	return &Writer{audit: auditLogger}
}

// Key returns the stored form of a Shadow MCP server's URL.
func Key(serverURL string) (string, error) {
	key, ok := shadowmcp.OfficialServerKey(serverURL)
	if !ok {
		return "", ErrInvalidServerURL
	}
	return key, nil
}

// Read returns the current choice for one stored key.
func Read(ctx context.Context, db shadowmcprepo.DBTX, projectID uuid.UUID, key string) (Choice, error) {
	row, err := shadowmcprepo.New(db).GetShadowMCPOfficialServerChoice(ctx, shadowmcprepo.GetShadowMCPOfficialServerChoiceParams{ProjectID: projectID, ServerUrl: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return automaticChoice(), nil
	}
	if err != nil {
		return Choice{}, fmt.Errorf("read shadow mcp official server choice: %w", err)
	}
	if row.ChosenMcpServerID.Valid {
		return Choice{Kind: ChoiceServer, MCPServerID: row.ChosenMcpServerID.UUID}, nil
	}
	if !row.LinkMcpServerID.Valid {
		return Choice{Kind: ChoiceKeepShadow, MCPServerID: uuid.Nil}, nil
	}
	// The chosen server was deleted or disabled.
	return automaticChoice(), nil
}

// SetInTransaction locks the stored choice, validates the chosen server,
// replaces, restores or removes the choice, and records the audit event in
// tx. The caller owns commit, and commits nothing when Changed is false.
func (w *Writer) SetInTransaction(ctx context.Context, tx pgx.Tx, input SetInput) (SetResult, error) {
	if w == nil || w.audit == nil || tx == nil {
		return SetResult{}, errors.New("shadow mcp official server writer is not configured")
	}
	if input.KeepShadow && input.MCPServerID.Valid {
		return SetResult{}, ErrKeepShadowWithServer
	}
	key, err := Key(input.ServerURL)
	if err != nil {
		return SetResult{}, err
	}
	q := shadowmcprepo.New(tx)
	// The row lock below only holds an existing choice. Concurrent first
	// choices would both read none and audit no previous state, so
	// serialize on the URL before reading.
	if err := q.LockShadowMCPOfficialServerURL(ctx, shadowmcprepo.LockShadowMCPOfficialServerURLParams{ProjectID: input.ProjectID, ServerUrl: key}); err != nil {
		return SetResult{}, fmt.Errorf("lock shadow mcp official server url: %w", err)
	}
	var existing *storedChoice
	row, err := q.LockShadowMCPOfficialServerChoice(ctx, shadowmcprepo.LockShadowMCPOfficialServerChoiceParams{ProjectID: input.ProjectID, ServerUrl: key})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return SetResult{}, fmt.Errorf("lock shadow mcp official server choice: %w", err)
	default:
		existing = &storedChoice{ID: row.ID, ServerURL: row.ServerUrl, MCPServerID: row.McpServerID}
	}
	current, err := Read(ctx, tx, input.ProjectID, key)
	if err != nil {
		return SetResult{}, err
	}
	if input.ValidateLocked != nil {
		if err := input.ValidateLocked(key, current); err != nil {
			return SetResult{}, err
		}
	}

	if input.MCPServerID.Valid {
		server, err := q.GetLiveMCPServerForShadowMCPChoice(ctx, shadowmcprepo.GetLiveMCPServerForShadowMCPChoiceParams{ID: input.MCPServerID.UUID, ProjectID: input.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return SetResult{}, ErrMCPServerNotFound
		}
		if err != nil {
			return SetResult{}, fmt.Errorf("get mcp server: %w", err)
		}
		// A disabled server serves nobody, so it cannot stand in for the
		// server people use today.
		if server.Visibility == mcpServerVisibilityDisabled {
			return SetResult{}, ErrMCPServerDisabled
		}
	}

	after := automaticChoice()
	switch {
	case input.MCPServerID.Valid:
		after = Choice{Kind: ChoiceServer, MCPServerID: input.MCPServerID.UUID}
	case input.KeepShadow:
		after = Choice{Kind: ChoiceKeepShadow, MCPServerID: uuid.Nil}
	}
	unchanged := SetResult{ServerKey: key, Before: current, After: current, Changed: false}
	useAutomatic := after.Kind == ChoiceAutomatic

	event := audit.ShadowMCPOfficialServerEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            input.Actor,
		ActorDisplayName: input.ActorDisplayName,
		ActorSlug:        nil,
		LinkURN:          urn.ShadowMCPOfficialServer{ID: uuid.Nil},
		ServerURL:        key,
	}
	userID := conv.ToPGTextEmpty(input.ActorUserID)

	switch {
	case useAutomatic && existing == nil:
		// No choice to remove: nothing changed, so nothing is recorded.
		return unchanged, nil
	case useAutomatic:
		if _, err := q.DeleteShadowMCPOfficialServerChoice(ctx, shadowmcprepo.DeleteShadowMCPOfficialServerChoiceParams{UserID: userID, ProjectID: input.ProjectID, ServerUrl: key}); err != nil {
			return SetResult{}, fmt.Errorf("remove shadow mcp official server choice: %w", err)
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(existing.ID)
		err = w.audit.LogShadowMCPOfficialServerDelete(ctx, tx, audit.LogShadowMCPOfficialServerDeleteEvent{
			ShadowMCPOfficialServerEvent:          event,
			ShadowMCPOfficialServerSnapshotBefore: existing.snapshot(),
		})
	case existing == nil:
		saved, addErr := addChoice(ctx, q, input.ProjectID, key, input.MCPServerID, userID)
		if addErr != nil {
			return SetResult{}, addErr
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(saved.ID)
		err = w.audit.LogShadowMCPOfficialServerCreate(ctx, tx, audit.LogShadowMCPOfficialServerCreateEvent{
			ShadowMCPOfficialServerEvent:         event,
			ShadowMCPOfficialServerSnapshotAfter: saved.snapshot(),
		})
	case existing.MCPServerID == input.MCPServerID:
		// The stored choice already says this.
		return unchanged, nil
	default:
		saved, upsertErr := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
			ProjectID:   input.ProjectID,
			ServerUrl:   key,
			McpServerID: input.MCPServerID,
			UserID:      userID,
		})
		if upsertErr != nil {
			return SetResult{}, fmt.Errorf("update shadow mcp official server choice: %w", upsertErr)
		}
		event.LinkURN = urn.NewShadowMCPOfficialServer(saved.ID)
		updated := storedChoice{ID: saved.ID, ServerURL: saved.ServerUrl, MCPServerID: saved.McpServerID}
		err = w.audit.LogShadowMCPOfficialServerUpdate(ctx, tx, audit.LogShadowMCPOfficialServerUpdateEvent{
			ShadowMCPOfficialServerEvent:          event,
			ShadowMCPOfficialServerSnapshotBefore: existing.snapshot(),
			ShadowMCPOfficialServerSnapshotAfter:  updated.snapshot(),
		})
	}
	if err != nil {
		return SetResult{}, fmt.Errorf("log shadow mcp official server change: %w", err)
	}
	return SetResult{ServerKey: key, Before: current, After: after, Changed: true}, nil
}

// storedChoice is the part of a stored choice the writer reads.
type storedChoice struct {
	ID          uuid.UUID
	ServerURL   string
	MCPServerID uuid.NullUUID
}

func (r storedChoice) snapshot() *audit.ShadowMCPOfficialServerSnapshot {
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

// addChoice adds a choice for a URL with none: it restores the most
// recently removed row for the URL, or inserts one.
func addChoice(ctx context.Context, q *shadowmcprepo.Queries, projectID uuid.UUID, key string, chosen uuid.NullUUID, userID pgtype.Text) (storedChoice, error) {
	restored, err := q.RestoreShadowMCPOfficialServerChoice(ctx, shadowmcprepo.RestoreShadowMCPOfficialServerChoiceParams{
		McpServerID: chosen,
		UserID:      userID,
		ProjectID:   projectID,
		ServerUrl:   key,
	})
	if err == nil {
		return storedChoice{ID: restored.ID, ServerURL: restored.ServerUrl, MCPServerID: restored.McpServerID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storedChoice{}, fmt.Errorf("restore shadow mcp official server choice: %w", err)
	}
	inserted, err := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
		ProjectID:   projectID,
		ServerUrl:   key,
		McpServerID: chosen,
		UserID:      userID,
	})
	if err != nil {
		return storedChoice{}, fmt.Errorf("add shadow mcp official server choice: %w", err)
	}
	return storedChoice{ID: inserted.ID, ServerURL: inserted.ServerUrl, MCPServerID: inserted.McpServerID}, nil
}

func automaticChoice() Choice {
	return Choice{Kind: ChoiceAutomatic, MCPServerID: uuid.Nil}
}
