// Package officiallink writes an administrator's link from a Shadow MCP
// server to its official version. The dashboard endpoint and the Platform MCP
// tool share this one write path, so validation, locking and the audit record
// cannot drift apart.
package officiallink

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowmcprepo "github.com/speakeasy-api/gram/server/internal/shadowmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mcpServerVisibilityDisabled is the visibility of a server that serves
// nobody.
const mcpServerVisibilityDisabled = "disabled"

var (
	// ErrInvalidServerURL means the Shadow MCP server URL cannot be keyed.
	ErrInvalidServerURL = errors.New("invalid shadow mcp server url")

	// ErrMCPServerNotFound means the chosen MCP server is not live in the
	// project.
	ErrMCPServerNotFound = errors.New("mcp server not found")

	// ErrMCPServerDisabled means the chosen MCP server is disabled.
	ErrMCPServerDisabled = errors.New("the chosen mcp server is disabled")

	// ErrLinkedServerRequired means a link was switched off or on without a
	// server to link.
	ErrLinkedServerRequired = errors.New("switching a link off needs the linked mcp server")
)

// Link is the effective link for one upstream URL, read the way the
// inventory reads it: a link whose server was deleted or disabled reads as no
// link.
type Link struct {
	// MCPServerID is the linked server, or uuid.Nil when there is no link.
	MCPServerID uuid.UUID

	// AllowDirectAccess is true while the link is switched off. It is false
	// when there is no link.
	AllowDirectAccess bool
}

// Linked reports whether the upstream URL has an effective link.
func (l Link) Linked() bool { return l.MCPServerID != uuid.Nil }

// SetInput is one change to a Shadow MCP server's link. Without a server to
// link and with AllowDirectAccess false, the link is removed. Without a
// server and with AllowDirectAccess true, the change is refused with
// ErrLinkedServerRequired.
type SetInput struct {
	OrganizationID string
	ProjectID      uuid.UUID

	// ServerURL is the Shadow MCP server's URL in any form the inventory
	// accepts; it is stored under shadowmcp.OfficialServerKey.
	ServerURL string

	// MCPServerID is the official server to link. Unset removes the link
	// unless KeepLinkedServer is set.
	MCPServerID uuid.NullUUID

	// KeepLinkedServer links the server the link already names, read under
	// the lock. It switches an existing link off or on without naming its
	// server; with no current link it is refused with
	// ErrLinkedServerRequired.
	KeepLinkedServer bool

	// AllowDirectAccess keeps the link but stops blocking direct use.
	AllowDirectAccess bool

	// ActorUserID is recorded as the row's author.
	ActorUserID string

	// Actor and ActorDisplayName are recorded in the audit log.
	Actor            urn.Principal
	ActorDisplayName *string

	// ValidateLocked sees the stored key and the effective link after the
	// row is locked and before any write. An error it returns aborts the
	// change unchanged.
	ValidateLocked func(key string, link Link) error
}

// SetResult reports what a change did.
type SetResult struct {
	// ServerKey is the stored form of the Shadow MCP server's URL.
	ServerKey string

	// Before is the effective link the change replaced.
	Before Link

	// After is the link once the change applies.
	After Link

	// Changed is false when the request matched the stored link, so nothing
	// was written or audited.
	Changed bool
}

// Writer applies link changes inside a caller-owned transaction.
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

// Read returns the effective link for one stored key.
func Read(ctx context.Context, db shadowmcprepo.DBTX, projectID uuid.UUID, key string) (Link, error) {
	row, err := shadowmcprepo.New(db).GetShadowMCPOfficialServerChoice(ctx, shadowmcprepo.GetShadowMCPOfficialServerChoiceParams{ProjectID: projectID, ServerUrl: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return noLink(), nil
	}
	if err != nil {
		return Link{}, fmt.Errorf("read shadow mcp official server link: %w", err)
	}
	if !row.ChosenMcpServerID.Valid {
		return noLink(), nil
	}
	return Link{MCPServerID: row.ChosenMcpServerID.UUID, AllowDirectAccess: row.AllowDirectAccess}, nil
}

// SetInTransaction locks the stored link, validates the chosen server,
// replaces or removes the link, and records the audit event in tx. The caller
// owns commit, and commits nothing when Changed is false.
func (w *Writer) SetInTransaction(ctx context.Context, tx pgx.Tx, input SetInput) (SetResult, error) {
	if w == nil || w.audit == nil || tx == nil {
		return SetResult{}, errors.New("shadow mcp official server writer is not configured")
	}
	key, err := Key(input.ServerURL)
	if err != nil {
		return SetResult{}, err
	}
	q := shadowmcprepo.New(tx)

	var before *audit.ShadowMCPOfficialServerSnapshot
	choiceID := uuid.Nil
	existing, err := q.LockShadowMCPOfficialServerChoice(ctx, shadowmcprepo.LockShadowMCPOfficialServerChoiceParams{ProjectID: input.ProjectID, ServerUrl: key})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return SetResult{}, fmt.Errorf("lock shadow mcp official server choice: %w", err)
	default:
		before = snapshot(existing)
		choiceID = existing.ID
	}
	effective, err := Read(ctx, tx, input.ProjectID, key)
	if err != nil {
		return SetResult{}, err
	}
	if input.ValidateLocked != nil {
		if err := input.ValidateLocked(key, effective); err != nil {
			return SetResult{}, err
		}
	}

	chosen := input.MCPServerID
	if input.KeepLinkedServer {
		if !effective.Linked() {
			return SetResult{}, ErrLinkedServerRequired
		}
		chosen = uuid.NullUUID{UUID: effective.MCPServerID, Valid: true}
	}
	if chosen.Valid {
		server, err := q.GetLiveMCPServerForShadowMCPChoice(ctx, shadowmcprepo.GetLiveMCPServerForShadowMCPChoiceParams{ID: chosen.UUID, ProjectID: input.ProjectID})
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
	// Switching a link off keeps the linked server; without one there is
	// nothing to switch off.
	if !chosen.Valid && input.AllowDirectAccess {
		return SetResult{}, ErrLinkedServerRequired
	}

	after := noLink()
	if chosen.Valid {
		after = Link{MCPServerID: chosen.UUID, AllowDirectAccess: input.AllowDirectAccess}
	}
	unchanged := SetResult{ServerKey: key, Before: effective, After: effective, Changed: false}

	var afterSnapshot *audit.ShadowMCPOfficialServerSnapshot
	userID := conv.ToPGTextEmpty(input.ActorUserID)
	if !chosen.Valid {
		if before == nil {
			// No link to remove: nothing changed, so nothing is recorded.
			return unchanged, nil
		}
		if _, err := q.DeleteShadowMCPOfficialServerChoice(ctx, shadowmcprepo.DeleteShadowMCPOfficialServerChoiceParams{UserID: userID, ProjectID: input.ProjectID, ServerUrl: key}); err != nil {
			return SetResult{}, fmt.Errorf("clear shadow mcp official server choice: %w", err)
		}
	}
	if chosen.Valid {
		if before != nil && before.MCPServerID == chosen.UUID.String() && before.AllowDirectAccess == input.AllowDirectAccess {
			return unchanged, nil
		}
		saved, err := q.UpsertShadowMCPOfficialServerChoice(ctx, shadowmcprepo.UpsertShadowMCPOfficialServerChoiceParams{
			ProjectID:         input.ProjectID,
			ServerUrl:         key,
			McpServerID:       chosen,
			AllowDirectAccess: input.AllowDirectAccess,
			UserID:            userID,
		})
		if err != nil {
			return SetResult{}, fmt.Errorf("save shadow mcp official server choice: %w", err)
		}
		afterSnapshot = snapshot(saved)
		choiceID = saved.ID
	}

	if err := w.audit.LogShadowMCPOfficialServerSet(ctx, tx, audit.LogShadowMCPOfficialServerSetEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            input.Actor,
		ActorDisplayName: input.ActorDisplayName,
		ActorSlug:        nil,
		ChoiceURN:        urn.NewShadowMCPOfficialServer(choiceID),
		ServerURL:        key,
		SnapshotBefore:   before,
		SnapshotAfter:    afterSnapshot,
	}); err != nil {
		return SetResult{}, fmt.Errorf("log shadow mcp official server choice: %w", err)
	}
	return SetResult{ServerKey: key, Before: effective, After: after, Changed: true}, nil
}

func noLink() Link {
	return Link{MCPServerID: uuid.Nil, AllowDirectAccess: false}
}

func snapshot(row shadowmcprepo.ShadowMcpOfficialServer) *audit.ShadowMCPOfficialServerSnapshot {
	result := &audit.ShadowMCPOfficialServerSnapshot{
		ServerURL:         row.ServerUrl,
		MCPServerID:       "",
		AllowDirectAccess: row.AllowDirectAccess,
	}
	if row.McpServerID.Valid {
		result.MCPServerID = row.McpServerID.UUID.String()
	}
	return result
}
