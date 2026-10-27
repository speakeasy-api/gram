package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// requestPublication asks for the project's plugins to be republished and
// reports whether that was queued.
func (m *Migrator) requestPublication(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, actor Actor, outcome *Outcome) error {
	queued, err := m.publication.ProjectWithOutcome(ctx, tx, organizationID, projectID, actor.UserID)
	if err != nil {
		return fmt.Errorf("request plugin publication: %w", err)
	}
	if queued == plugins.ProjectPublicationEnqueued {
		outcome.Actions = append(outcome.Actions, "Republish the project's plugins")
	} else {
		outcome.Warnings = append(outcome.Warnings, "the project's plugins were not queued for republishing; republish them from the dashboard")
	}
	return nil
}

// deleteInTx deletes a toolset left with nothing to migrate, taking the same
// steps as the toolset delete API: its hosted server and that server's
// addresses, plugin entries, gateway memberships and assistant links go with
// it. The caller holds the scope and toolset locks.
//
// The API also reconciles a custom domain whose root address it cleared and
// cleans up results of risk policies bound to the deleted server, after its
// transaction commits. This path does neither, so it refuses a toolset that
// needs either; delete that one from the dashboard instead.
func (m *Migrator) deleteInTx(ctx context.Context, tx pgx.Tx, actor Actor, toolset toolsetsrepo.Toolset, outcome *Outcome) error {
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, actor.UserID)

	// Only an MCP-enabled toolset is in a generated package, so only its
	// deletion can change one. Membership is read before the hosted server
	// goes, since that detaches server-backed plugin entries.
	served, _, err := hostedmcp.LoadServerState(ctx, tx, toolset)
	if err != nil {
		return fmt.Errorf("load hosted MCP server: %w", err)
	}
	carried := false
	if served.McpEnabled {
		carried, err = pluginsrepo.New(tx).HasPluginMembershipForToolset(ctx, pluginsrepo.HasPluginMembershipForToolsetParams{ProjectID: toolset.ProjectID, ToolsetID: toolset.ID})
		if err != nil {
			return fmt.Errorf("check toolset plugin membership: %w", err)
		}
	}

	toolsets := toolsetsrepo.New(tx)
	deleted, err := toolsets.DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: toolset.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotCandidate
	}
	if err != nil {
		return fmt.Errorf("delete toolset: %w", err)
	}

	// hostedmcp.Delete tombstones only a server the toolset still backs under
	// its own id; one already migrated keeps working.
	ownServer := false
	if serverID, err := repo.New(tx).GetToolsetServerID(ctx, repo.GetToolsetServerIDParams{ToolsetID: conv.ToNullUUID(toolset.ID), ProjectID: toolset.ProjectID}); err == nil {
		ownServer = serverID == toolset.ID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read toolset server: %w", err)
	}

	hosted, err := hostedmcp.Delete(ctx, tx, m.tracerProvider, m.audit, hostedmcp.Actor{UserID: actor.UserID, Email: actor.Email, System: ""}, toolset)
	if err != nil {
		return fmt.Errorf("delete hosted server: %w", err)
	}
	if len(hosted.RootDomainIDs) > 0 {
		return fail("its server is the root of a custom domain; delete it from the dashboard instead")
	}
	if len(hosted.DeletedRiskPolicies) > 0 {
		return fail("risk policies are bound to its server; delete it from the dashboard instead")
	}
	if ownServer && toolset.McpSlug.Valid && toolset.McpSlug.String != "" {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Delete the server at /mcp/%s with its addresses and plugin entries", toolset.McpSlug.String))
	}

	// A server whose id is not the toolset's still points at it; deleting the
	// toolset under it would leave that server broken.
	if _, err := repo.New(tx).GetToolsetServerID(ctx, repo.GetToolsetServerIDParams{ToolsetID: conv.ToNullUUID(toolset.ID), ProjectID: toolset.ProjectID}); err == nil {
		return fail("another server is backed by this toolset; delete that server first")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read toolset servers: %w", err)
	}

	if carried {
		if err := m.requestPublication(ctx, tx, toolset.OrganizationID, toolset.ProjectID, actor, outcome); err != nil {
			return err
		}
	}

	if err := m.audit.LogToolsetDelete(ctx, tx, audit.LogToolsetDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: principal, ActorDisplayName: actor.Email, ActorSlug: nil,
		ToolsetURN: urn.NewToolset(toolset.ID), ToolsetName: deleted.Name, ToolsetSlug: deleted.Slug,
	}); err != nil {
		return fmt.Errorf("audit toolset deletion: %w", err)
	}
	outcome.Actions = append(outcome.Actions, "Delete the toolset")
	return nil
}
