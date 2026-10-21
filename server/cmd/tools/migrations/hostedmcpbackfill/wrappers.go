package hostedmcpbackfill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// WrapperCleanupActorComponent is the system principal (system:<WrapperCleanupActorComponent>) the orphan wrapper delete audits as.
const WrapperCleanupActorComponent = "hosted-mcp-orphan-wrapper-cleanup"

const (
	tableMcpServers                   = "mcp_servers"
	tableMetaMcpServerMembers         = "meta_mcp_server_members"
	tableMcpServerRemoteSessionClient = "mcp_server_remote_session_clients"
)

const (
	reasonCanonicalServer = "canonical server: deleting the toolset tombstones it, so this mode leaves it"
	reasonToolsetLive     = "toolset is live"
	reasonLiveEndpoint    = "the server has a live endpoint, so it is still addressable; delete it in the product"
	reasonSessionIssuer   = "the server owns a user session issuer, whose cascade revokes sessions; delete it in the product"
	reasonRiskPolicy      = "a risk policy targets only this server and would be deleted with it; delete it in the product"
)

// WrapperDetached counts the rows a server delete removed with the server.
type WrapperDetached struct {
	GatewayMemberships   int `json:"gateway_memberships"`
	PluginMembers        int `json:"plugin_members"`
	AssistantAttachments int `json:"assistant_attachments"`
	SessionClientLinks   int `json:"session_client_links"`

	// OtherSessionClientLinks are other servers' unservable links the same prune removed; the journal and restore leave them out.
	OtherSessionClientLinks int `json:"other_session_client_links"`

	RiskPolicyScopes int `json:"risk_policy_scopes"`
}

// WrapperDependents are the images of the rows a server delete changed along with the server.
type WrapperDependents struct {
	GatewayMemberships   []RowImage `json:"gateway_memberships"`
	PluginMembers        []RowImage `json:"plugin_members"`
	AssistantAttachments []RowImage `json:"assistant_attachments"`

	// SessionClientLinks are the deleted server's own links the issuer prune removed.
	SessionClientLinks []RowImage `json:"session_client_links"`

	// RiskPolicyScopes are the surviving policies the delete removed the server from.
	RiskPolicyScopes []PolicyScopeImage `json:"risk_policy_scopes"`
}

// PolicyScopeImage is a policy's mcp_scope before and after the delete removed the server.
type PolicyScopeImage struct {
	PolicyID    uuid.UUID       `json:"policy_id"`
	ScopeBefore json.RawMessage `json:"scope_before"`
	ScopeAfter  json.RawMessage `json:"scope_after"`
}

func (d ListToolsetOrphanWrappersRow) label() string { return tableMcpServers + " " + d.ID.String() }

func (r *Runner) orphanWrappersPhase() dependentPhase[ListToolsetOrphanWrappersRow] {
	return dependentPhase[ListToolsetOrphanWrappersRow]{
		listToolsets: func(ctx context.Context, after uuid.UUID) ([]uuid.UUID, error) {
			page, err := New(r.pool).ListOrphanWrapperToolsets(ctx, ListOrphanWrapperToolsetsParams{ProjectID: r.options.ProjectID, AfterID: after, PageSize: conv.SafeInt32(r.options.PageSize)})
			if err != nil {
				return nil, fmt.Errorf("list orphan wrapper toolsets: %w", err)
			}
			return page, nil
		},
		listDependents: func(ctx context.Context, toolsetID uuid.UUID) ([]ListToolsetOrphanWrappersRow, error) {
			servers, err := New(r.pool).ListToolsetOrphanWrappers(ctx, ListToolsetOrphanWrappersParams{ToolsetID: toolsetID, ProjectID: r.options.ProjectID})
			if err != nil {
				return nil, fmt.Errorf("list orphan wrappers: %w", err)
			}
			return servers, nil
		},
		process: r.deleteOrphanWrapperOne,
	}
}

// deleteOrphanWrapperOne soft-deletes one live server of a deleted toolset through tombstone, as DeleteMcpServer does.
func (r *Runner) deleteOrphanWrapperOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetOrphanWrappersRow) (RowReport, error) {
	row := RowReport{
		ToolsetID: toolsetID, Table: tableMcpServers, RowID: &dep.ID, ProjectID: dep.ProjectID, McpSlug: "", CustomDomainID: nil,
		Outcome: "", Reason: "", Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil, CollapsedTwin: nil, Removed: nil, PublicationRequested: false, Detached: nil,
	}
	if dep.ID == toolsetID {
		return skipped(row, reasonCanonicalServer), nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return row, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := New(tx)
	if err := q.SetMoveLockTimeout(ctx, fmt.Sprintf("%dms", r.options.LockTimeout.Milliseconds())); err != nil {
		return row, fmt.Errorf("set lock timeout: %w", err)
	}
	if err := q.LockToolsetBackfill(ctx, toolsetID.String()); err != nil {
		return row, fmt.Errorf("advisory lock: %w", err)
	}
	if err := admission.LockProject(ctx, tx, dep.ProjectID); err != nil {
		return row, fmt.Errorf("lock project distribution admission: %w", err)
	}
	toolset, err := q.LockOrphanToolset(ctx, LockOrphanToolsetParams{ID: toolsetID, ProjectID: dep.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeBlockedNoWrapper, "toolset is not in the row's project"), nil
	case err != nil:
		return row, fmt.Errorf("lock toolset: %w", err)
	case toolset.ProjectDeleted:
		return skipped(row, "project deleted"), nil
	case !toolset.Deleted:
		return skipped(row, reasonToolsetLive), nil
	}

	// tombstone.Lock takes admission (held), custom domains, endpoints, then the server, as every server writer does.
	locked, err := tombstone.Lock(ctx, tx, toolset.OrganizationID, toolset.ProjectID, dep.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return skipped(row, reasonRowChanged), nil
	case err != nil:
		return row, fmt.Errorf("lock server: %w", err)
	case locked.Server.ToolsetID != (uuid.NullUUID{UUID: toolsetID, Valid: true}):
		return skipped(row, reasonRowChanged), nil
	}
	row.McpSlug = locked.Server.Slug.String

	endpoints, err := q.CountLiveServerEndpoints(ctx, CountLiveServerEndpointsParams{McpServerID: dep.ID, ProjectID: toolset.ProjectID})
	switch {
	case err != nil:
		return row, fmt.Errorf("count live endpoints: %w", err)
	case endpoints > 0:
		row.LiveEndpoints = int(endpoints)
		return blocked(row, OutcomeBlockedLiveEndpoint, reasonLiveEndpoint), nil
	case locked.Server.UserSessionIssuerID.Valid:
		return blocked(row, OutcomeBlockedSessionIssuer, reasonSessionIssuer), nil
	}

	serverImage, err := q.WrapperServerImage(ctx, WrapperServerImageParams{ID: dep.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return row, fmt.Errorf("image server: %w", err)
	}
	// Rows are locked in tombstone's write order; new ones need the server row's key share, which the lock holds off.
	memberships, err := q.LockWrapperMemberships(ctx, LockWrapperMembershipsParams{McpServerID: dep.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return row, fmt.Errorf("lock gateway memberships: %w", err)
	}
	pluginMembers, err := q.LockWrapperPluginMembers(ctx, LockWrapperPluginMembersParams{McpServerID: dep.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return row, fmt.Errorf("lock plugin members: %w", err)
	}
	attachments, err := q.LockWrapperAssistantAttachments(ctx, LockWrapperAssistantAttachmentsParams{McpServerID: dep.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return row, fmt.Errorf("lock assistant attachments: %w", err)
	}
	tombstoned, err := tombstone.Tombstone(ctx, tx, r.audit, locked, tombstone.Input{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID, ActorUserID: "", ActorEmail: nil,
		ActorSystem: WrapperCleanupActorComponent, TracerProvider: noop.NewTracerProvider(),
	})
	if err != nil {
		return row, fmt.Errorf("tombstone server: %w", err)
	}
	// Their results need a post-commit Temporal cleanup this tool cannot run.
	if len(tombstoned.DeletedRiskPolicies) > 0 {
		return blocked(row, OutcomeBlockedRiskPolicy, reasonRiskPolicy), nil
	}
	if tombstoned.DetachedPluginServers != len(pluginMembers) {
		return row, fmt.Errorf("tombstone server: detached %d plugin members, locked %d", tombstoned.DetachedPluginServers, len(pluginMembers))
	}
	if r.beforeWrapperPrune != nil {
		r.beforeWrapperPrune(ctx)
	}
	// The app's post-commit resync with no issuers is this prune alone; RETURNING journals only what it deleted.
	pruned, err := q.PruneUnservableSessionClientLinks(ctx, PruneUnservableSessionClientLinksParams{ProjectID: toolset.ProjectID, OrganizationID: toolset.OrganizationID})
	if err != nil {
		return row, fmt.Errorf("prune session client links: %w", err)
	}
	if err := r.audit.LogMcpServerDelete(ctx, tx, audit.LogMcpServerDeleteEvent{
		OrganizationID: toolset.OrganizationID, ProjectID: toolset.ProjectID,
		Actor: urn.NewSystemPrincipal(WrapperCleanupActorComponent), ActorDisplayName: nil, ActorSlug: nil,
		McpServerURN:  urn.NewMcpServer(dep.ID),
		McpServerName: conv.FromPGTextOrEmpty[string](locked.Server.Name), McpServerSlug: conv.FromPGTextOrEmpty[string](locked.Server.Slug),
	}); err != nil {
		return row, fmt.Errorf("audit server delete: %w", err)
	}
	publication, err := r.options.Publication.ProjectWithOutcome(ctx, tx, toolset.OrganizationID, toolset.ProjectID, "")
	if err != nil {
		return row, fmt.Errorf("request plugin publication: %w", err)
	}

	removed, dependents, err := imageWrapperDelete(ctx, q, toolset.ProjectID, dep.ID, serverImage, memberships, pluginMembers, attachments, pruned)
	if err != nil {
		return row, err
	}
	for _, edit := range tombstoned.RiskPolicyScopeEdits {
		dependents.RiskPolicyScopes = append(dependents.RiskPolicyScopes, PolicyScopeImage{PolicyID: edit.PolicyID, ScopeBefore: edit.ScopeBefore, ScopeAfter: edit.ScopeAfter})
	}
	row.Detached = &WrapperDetached{
		GatewayMemberships: len(dependents.GatewayMemberships), PluginMembers: len(dependents.PluginMembers),
		AssistantAttachments: len(dependents.AssistantAttachments), SessionClientLinks: len(dependents.SessionClientLinks),
		OtherSessionClientLinks: len(pruned) - len(dependents.SessionClientLinks), RiskPolicyScopes: len(dependents.RiskPolicyScopes),
	}
	row.Reason = "toolset deleted"
	row.Removed = removed.withoutImages()
	row.Wrote = true
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeDeleted, OutcomeWouldDelete)
	if !r.options.Apply {
		return row, nil
	}
	row.PublicationRequested = publication == plugins.ProjectPublicationEnqueued
	return r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: dep.ID, Outcome: row.Outcome,
		From: RowKey{Table: tableMcpServers, Column: "toolset_id", ID: toolsetID}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
		CollapsedTwin: nil, Removed: removed, Rekeyed: nil, Dependents: dependents,
	})
}

// imageWrapperDelete reads back what the delete changed, so a restore can match only this tool's writes.
func imageWrapperDelete(
	ctx context.Context, q *Queries, projectID, serverID uuid.UUID, serverImage []byte,
	memberships []LockWrapperMembershipsRow, pluginMembers []LockWrapperPluginMembersRow, attachments []LockWrapperAssistantAttachmentsRow, pruned []PruneUnservableSessionClientLinksRow,
) (*RowImage, *WrapperDependents, error) {
	deletedAt, err := q.WrapperServerDeletedAt(ctx, WrapperServerDeletedAtParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return nil, nil, fmt.Errorf("read server deleted_at: %w", err)
	}
	if deletedAt == "" {
		return nil, nil, errors.New("tombstone server: server is still live")
	}
	removed := &RowImage{Table: tableMcpServers, ID: serverID, Deletion: "soft_deleted", Row: serverImage, EnvironmentConfigs: nil, SoftDeletedAt: deletedAt, RekeyedUpdatedAt: ""}
	dependents := &WrapperDependents{GatewayMemberships: []RowImage{}, PluginMembers: []RowImage{}, AssistantAttachments: []RowImage{}, SessionClientLinks: []RowImage{}, RiskPolicyScopes: []PolicyScopeImage{}}

	membershipIDs := make([]uuid.UUID, 0, len(memberships))
	for _, m := range memberships {
		membershipIDs = append(membershipIDs, m.ID)
	}
	membershipTimes, err := q.WrapperMembershipsDeletedAt(ctx, WrapperMembershipsDeletedAtParams{Ids: membershipIDs, ProjectID: projectID})
	if err != nil {
		return nil, nil, fmt.Errorf("read gateway membership deleted_at: %w", err)
	}
	for i, m := range memberships {
		if i >= len(membershipTimes) || membershipTimes[i].ID != m.ID || membershipTimes[i].DeletedAt == "" {
			return nil, nil, fmt.Errorf("tombstone server: gateway membership %s is still live", m.ID)
		}
		dependents.GatewayMemberships = append(dependents.GatewayMemberships, RowImage{Table: tableMetaMcpServerMembers, ID: m.ID, Deletion: "soft_deleted", Row: m.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: membershipTimes[i].DeletedAt, RekeyedUpdatedAt: ""})
	}

	pluginIDs := make([]uuid.UUID, 0, len(pluginMembers))
	for _, m := range pluginMembers {
		pluginIDs = append(pluginIDs, m.ID)
	}
	pluginTimes, err := q.WrapperPluginMembersDeletedAt(ctx, WrapperPluginMembersDeletedAtParams{Ids: pluginIDs, ProjectID: projectID})
	if err != nil {
		return nil, nil, fmt.Errorf("read plugin member deleted_at: %w", err)
	}
	for i, m := range pluginMembers {
		if i >= len(pluginTimes) || pluginTimes[i].ID != m.ID || pluginTimes[i].DeletedAt == "" {
			return nil, nil, fmt.Errorf("tombstone server: plugin member %s is still live", m.ID)
		}
		dependents.PluginMembers = append(dependents.PluginMembers, RowImage{Table: tablePluginServers, ID: m.ID, Deletion: "soft_deleted", Row: m.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: pluginTimes[i].DeletedAt, RekeyedUpdatedAt: ""})
	}

	attachmentIDs := make([]uuid.UUID, 0, len(attachments))
	for _, a := range attachments {
		attachmentIDs = append(attachmentIDs, a.ID)
		dependents.AssistantAttachments = append(dependents.AssistantAttachments, RowImage{Table: tableAssistantMcpServers, ID: a.ID, Deletion: "deleted", Row: a.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""})
	}
	left, err := q.CountWrapperAssistantAttachments(ctx, CountWrapperAssistantAttachmentsParams{Ids: attachmentIDs, ProjectID: projectID})
	if err != nil {
		return nil, nil, fmt.Errorf("count assistant attachments: %w", err)
	}
	if left != 0 {
		return nil, nil, fmt.Errorf("tombstone server: %d assistant attachments remain", left)
	}

	for _, l := range pruned {
		if l.McpServerID == serverID {
			dependents.SessionClientLinks = append(dependents.SessionClientLinks, RowImage{Table: tableMcpServerRemoteSessionClient, ID: l.ID, Deletion: "deleted", Row: l.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""})
		}
	}
	return removed, dependents, nil
}
