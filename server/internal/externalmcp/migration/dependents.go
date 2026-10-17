package migration

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// dependentMove moves everything that points at the old server, keyed by its
// toolset or by its canonical wrapper (both carry the toolset id), onto the
// replacement. It runs before the old wrapper is tombstoned, which would
// otherwise delete these rows.
type dependentMove struct {
	tx             pgx.Tx
	audit          *audit.Logger
	principal      urn.Principal
	actorEmail     *string
	organizationID string
	projectID      uuid.UUID
	backendID      uuid.UUID
	targetID       uuid.UUID
}

// run moves every dependent and reports whether any plugin entry moved.
func (d dependentMove) run(ctx context.Context, outcome *Outcome) (bool, error) {
	pluginRowsMoved, err := d.movePluginServers(ctx, outcome)
	if err != nil {
		return false, err
	}
	if err := d.moveAssistants(ctx, outcome); err != nil {
		return false, err
	}
	if err := d.moveMetadata(ctx, outcome); err != nil {
		return false, err
	}
	if err := d.moveGatewayMemberships(ctx, outcome); err != nil {
		return false, err
	}

	queries := repo.New(d.tx)
	duplicates, err := queries.RemoveDuplicateCollectionAttachments(ctx, repo.RemoveDuplicateCollectionAttachmentsParams{
		OrganizationID: d.organizationID, BackendID: uuid.NullUUID{UUID: d.backendID, Valid: true},
		McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("remove duplicate collection attachments: %w", err)
	}
	if duplicates > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Remove %d organization collection entries the replacement already covers", duplicates))
	}
	collections, err := queries.RekeyCollectionAttachments(ctx, repo.RekeyCollectionAttachmentsParams{
		McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true}, OrganizationID: d.organizationID,
		BackendID: uuid.NullUUID{UUID: d.backendID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("move collection attachments: %w", err)
	}
	if collections > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move %d organization collection entries", collections))
	}

	grants, err := queries.CopyMCPGrantsToServer(ctx, repo.CopyMCPGrantsToServerParams{
		McpServerID: d.targetID.String(), OrganizationID: d.organizationID, BackendID: d.backendID.String(),
	})
	if err != nil {
		return false, fmt.Errorf("copy MCP grants: %w", err)
	}
	if grants > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Copy %d access grants to the replacement", grants))
	}
	return pluginRowsMoved, nil
}

// movePluginServers re-keys one entry per plugin onto the replacement and
// removes the rest. A plugin that already carries the replacement keeps that
// entry. Otherwise the entry kept is the one a Platform MCP distribution
// references, then the wrapper-keyed one, then the toolset-keyed one.
// Distributions of a removed entry move to the kept one.
func (d dependentMove) movePluginServers(ctx context.Context, outcome *Outcome) (bool, error) {
	queries := repo.New(d.tx)
	rows, err := queries.ListPluginServersForBackend(ctx, repo.ListPluginServersForBackendParams{
		ProjectID: d.projectID, BackendID: uuid.NullUUID{UUID: d.backendID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("list plugin entries: %w", err)
	}
	if len(rows) == 0 {
		return false, nil
	}

	byPlugin := map[uuid.UUID][]repo.ListPluginServersForBackendRow{}
	plugins := make([]uuid.UUID, 0)
	for _, row := range rows {
		if _, ok := byPlugin[row.PluginID]; !ok {
			plugins = append(plugins, row.PluginID)
		}
		byPlugin[row.PluginID] = append(byPlugin[row.PluginID], row)
	}

	targetURN := urn.NewMcpServer(d.targetID)
	for _, pluginID := range plugins {
		entries := byPlugin[pluginID]
		keep := -1
		retainedID, err := queries.GetPluginServerForServer(ctx, repo.GetPluginServerForServerParams{
			PluginID: pluginID, ProjectID: d.projectID, McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true},
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			keep = keptPluginEntry(entries)
			retainedID = entries[keep].ID
		case err != nil:
			return false, fmt.Errorf("check plugin entry: %w", err)
		}
		for i, entry := range entries {
			if i == keep {
				if _, err := queries.RekeyPluginServer(ctx, repo.RekeyPluginServerParams{
					McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true}, ID: entry.ID, ProjectID: d.projectID,
				}); err != nil {
					return false, fmt.Errorf("move plugin entry: %w", err)
				}
				var previous *urn.Toolset
				if entry.ToolsetID.Valid {
					previousURN := urn.NewToolset(entry.ToolsetID.UUID)
					previous = &previousURN
				}
				if err := d.audit.LogPluginServerUpdate(ctx, d.tx, audit.LogPluginServerUpdateEvent{
					OrganizationID: d.organizationID, ProjectID: d.projectID,
					Actor: d.principal, ActorDisplayName: d.actorEmail, ActorSlug: nil,
					PluginID: entry.PluginID, PluginName: entry.PluginName, PluginSlug: entry.PluginSlug,
					ServerID: entry.ID, ServerDisplayName: entry.DisplayName, ServerPolicy: entry.Policy, ServerSortOrder: entry.SortOrder,
					McpServerURN: &targetURN, PreviousToolsetURN: previous,
				}); err != nil {
					return false, fmt.Errorf("audit plugin entry move: %w", err)
				}
				outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move the plugin entry in %q to the replacement", entry.PluginName))
				continue
			}

			if _, err := queries.SoftDeletePluginServer(ctx, repo.SoftDeletePluginServerParams{ID: entry.ID, ProjectID: d.projectID}); err != nil {
				return false, fmt.Errorf("remove duplicate plugin entry: %w", err)
			}
			var toolsetURN *urn.Toolset
			var serverURN *urn.McpServer
			if entry.ToolsetID.Valid {
				value := urn.NewToolset(entry.ToolsetID.UUID)
				toolsetURN = &value
			}
			if entry.McpServerID.Valid {
				value := urn.NewMcpServer(entry.McpServerID.UUID)
				serverURN = &value
			}
			if err := d.audit.LogPluginServerRemove(ctx, d.tx, audit.LogPluginServerRemoveEvent{
				OrganizationID: d.organizationID, ProjectID: d.projectID,
				Actor: d.principal, ActorDisplayName: d.actorEmail, ActorSlug: nil,
				PluginID: entry.PluginID, PluginName: entry.PluginName, PluginSlug: entry.PluginSlug,
				ServerID: entry.ID, ToolsetURN: toolsetURN, McpServerURN: serverURN, MetaMcpServerURN: nil,
			}); err != nil {
				return false, fmt.Errorf("audit duplicate plugin entry removal: %w", err)
			}
			if entry.Distributed {
				if _, err := queries.RepointDistributionsToPluginServer(ctx, repo.RepointDistributionsToPluginServerParams{
					RetainedPluginServerID: uuid.NullUUID{UUID: retainedID, Valid: true}, ProjectID: d.projectID,
					RemovedPluginServerID: uuid.NullUUID{UUID: entry.ID, Valid: true},
				}); err != nil {
					return false, fmt.Errorf("move plugin entry distributions: %w", err)
				}
				outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move the distributions of a removed plugin entry in %q to the kept entry", entry.PluginName))
			}
		}
	}
	return true, nil
}

func keptPluginEntry(entries []repo.ListPluginServersForBackendRow) int {
	if i := slices.IndexFunc(entries, func(e repo.ListPluginServersForBackendRow) bool { return e.Distributed }); i >= 0 {
		return i
	}
	if i := slices.IndexFunc(entries, func(e repo.ListPluginServersForBackendRow) bool { return e.McpServerID.Valid }); i >= 0 {
		return i
	}
	return 0
}

// moveAssistants moves each assistant's attachment of the old server onto the
// replacement, keeping the attachment id and environment. An assistant that
// already has the replacement, or has both a toolset-keyed and a server-keyed
// attachment, keeps one.
func (d dependentMove) moveAssistants(ctx context.Context, outcome *Outcome) error {
	queries := repo.New(d.tx)
	rows, err := queries.ListAssistantAttachmentsForBackend(ctx, repo.ListAssistantAttachmentsForBackendParams{ProjectID: d.projectID, BackendID: d.backendID})
	if err != nil {
		return fmt.Errorf("list assistant attachments: %w", err)
	}
	moved := 0
	for _, row := range rows {
		present, err := queries.AssistantHasServer(ctx, repo.AssistantHasServerParams{AssistantID: row.AssistantID, ProjectID: d.projectID, McpServerID: d.targetID})
		if err != nil {
			return fmt.Errorf("check assistant attachment: %w", err)
		}
		switch {
		case present && row.Kind == "toolset":
			_, err = queries.DeleteAssistantToolset(ctx, repo.DeleteAssistantToolsetParams{ID: row.ID, ProjectID: d.projectID})
		case present:
			_, err = queries.DeleteAssistantMcpServer(ctx, repo.DeleteAssistantMcpServerParams{ID: row.ID, ProjectID: d.projectID})
		case row.Kind == "toolset":
			_, err = queries.MoveAssistantToolsetToServer(ctx, repo.MoveAssistantToolsetToServerParams{ID: row.ID, ProjectID: d.projectID, McpServerID: d.targetID})
			moved++
		default:
			_, err = queries.RepointAssistantMcpServer(ctx, repo.RepointAssistantMcpServerParams{McpServerID: d.targetID, ID: row.ID, ProjectID: d.projectID})
			moved++
		}
		if err != nil {
			return fmt.Errorf("move assistant attachment: %w", err)
		}
	}
	if moved > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move %d assistant attachments to the replacement", moved))
	}
	return nil
}

// moveMetadata re-keys the old server's metadata row in place, keeping its
// environment configuration, unless the replacement already has its own. The
// toolset-keyed row wins over a wrapper-keyed twin.
func (d dependentMove) moveMetadata(ctx context.Context, outcome *Outcome) error {
	queries := repo.New(d.tx)
	rows, err := queries.ListMetadataForBackend(ctx, repo.ListMetadataForBackendParams{ProjectID: d.projectID, BackendID: uuid.NullUUID{UUID: d.backendID, Valid: true}})
	if err != nil {
		return fmt.Errorf("list metadata: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	present, err := queries.ServerHasMetadata(ctx, repo.ServerHasMetadataParams{ProjectID: d.projectID, McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true}})
	if err != nil {
		return fmt.Errorf("check replacement metadata: %w", err)
	}
	for i, row := range rows {
		if i == 0 && !present {
			if _, err := queries.RekeyMetadata(ctx, repo.RekeyMetadataParams{McpServerID: uuid.NullUUID{UUID: d.targetID, Valid: true}, ID: row.ID, ProjectID: d.projectID}); err != nil {
				return fmt.Errorf("move metadata: %w", err)
			}
			outcome.Actions = append(outcome.Actions, "Move the server's metadata and environment configuration")
			continue
		}
		if _, err := queries.DeleteMetadata(ctx, repo.DeleteMetadataParams{ID: row.ID, ProjectID: d.projectID}); err != nil {
			return fmt.Errorf("remove duplicate metadata: %w", err)
		}
	}
	return nil
}

func (d dependentMove) moveGatewayMemberships(ctx context.Context, outcome *Outcome) error {
	moved, err := repo.New(d.tx).RepointMetaMcpMembers(ctx, repo.RepointMetaMcpMembersParams{
		McpServerID: d.targetID, ProjectID: d.projectID, BackendID: d.backendID,
	})
	if err != nil {
		return fmt.Errorf("move gateway memberships: %w", err)
	}
	for _, membership := range moved {
		if err := d.audit.LogMetaMcpMemberUpdate(ctx, d.tx, audit.LogMetaMcpMemberEvent{
			OrganizationID: d.organizationID, ProjectID: d.projectID,
			Actor: d.principal, ActorDisplayName: d.actorEmail, ActorSlug: nil,
			MetaMcpServerURN: urn.NewMetaMcpServer(membership.MetaMcpServerID), Name: membership.MetaMcpServerName,
			MembershipURN: urn.NewMetaMcpServerMember(membership.ID), McpServerURN: urn.NewMcpServer(d.targetID), SortOrder: membership.SortOrder,
		}); err != nil {
			return fmt.Errorf("audit gateway membership move: %w", err)
		}
	}
	if len(moved) > 0 {
		outcome.Actions = append(outcome.Actions, fmt.Sprintf("Move %d gateway memberships to the replacement", len(moved)))
	}
	return nil
}
