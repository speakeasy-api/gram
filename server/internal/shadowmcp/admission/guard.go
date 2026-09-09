package admission

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrApprovalRequired means the complete desired audience is not covered by a
	// standing Shadow MCP approval.
	ErrApprovalRequired = errors.New("shadow MCP distribution approval required")
	// ErrUnavailable means distribution admission could not reach a complete,
	// trustworthy decision.
	ErrUnavailable = errors.New("shadow MCP distribution admission unavailable")
	// ErrDistributionDisabled means the direct-remote distribution kill switch is
	// active for this project.
	ErrDistributionDisabled = errors.New("direct-remote distribution disabled")
)

// Guard applies one rollout decision to every direct-remote exposure mutation.
// Resolve must run before opening the caller's transaction so feature-provider
// I/O never occurs while database locks are held. The returned error is retained
// and passed to check methods: it blocks only when the transaction proves the
// requested operation is in scope.
type Guard struct {
	flags feature.Provider
}

func NewGuard(flags feature.Provider) *Guard {
	return &Guard{flags: flags}
}

func (g *Guard) Resolve(ctx context.Context, organizationID, organizationSlug, projectSlug string) (RolloutConfig, error) {
	if g == nil {
		return RolloutConfig{}, fmt.Errorf("%w: guard is nil", ErrUnavailable)
	}
	config, err := ResolveRollout(ctx, g.flags, organizationID, organizationSlug, projectSlug)
	if err != nil {
		return RolloutConfig{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return config, nil
}

func (g *Guard) ResolveProject(ctx context.Context, db projectsrepo.DBTX, organizationID, organizationSlug string, projectID uuid.UUID) (RolloutConfig, error) {
	if db == nil || projectID == uuid.Nil {
		return RolloutConfig{}, fmt.Errorf("%w: project rollout identity is incomplete", ErrUnavailable)
	}
	project, err := projectsrepo.New(db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: organizationID})
	if err != nil {
		return RolloutConfig{}, fmt.Errorf("%w: resolve project rollout identity: %w", ErrUnavailable, err)
	}
	return g.Resolve(ctx, organizationID, organizationSlug, project.Slug)
}

// CheckAttachment validates adding one MCP server to an exact existing plugin.
// The plugin's complete current audience is the resulting audience because
// attachment does not mutate assignments.
func (g *Guard) CheckAttachment(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, pluginID, mcpServerID uuid.UUID) error {
	target, scoped, err := directRemoteTarget(ctx, tx, organizationID, projectID, mcpServerID)
	if err != nil {
		return unavailable(err)
	}
	if !scoped {
		return nil
	}
	if target == "" {
		return unavailable(errors.New("direct-remote provenance has no live remote target"))
	}
	if err := requireUsableRollout(rollout, rolloutErr); err != nil {
		return err
	}

	assignments, err := listPluginAssignments(ctx, tx, organizationID, projectID, pluginID)
	if err != nil {
		return unavailable(err)
	}
	return g.checkURL(ctx, tx, rollout, organizationID, projectID, target, assignments)
}

// CheckAttachmentWithSeededAudience validates an attachment against an already
// resolved plugin plus the audience that plugin creation would seed. It avoids
// creating the plugin before admission while preserving the existing default-
// project Everyone behavior.
func (g *Guard) CheckAttachmentWithSeededAudience(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, mcpServerID uuid.UUID, desired []string) error {
	target, scoped, err := directRemoteTarget(ctx, tx, organizationID, projectID, mcpServerID)
	if err != nil {
		return unavailable(err)
	}
	if !scoped {
		return nil
	}
	if target == "" {
		return unavailable(errors.New("direct-remote provenance has no live remote target"))
	}
	if err := requireUsableRollout(rollout, rolloutErr); err != nil {
		return err
	}
	return g.checkURL(ctx, tx, rollout, organizationID, projectID, target, desired)
}

// CheckProspectiveDefaultAttachment checks an existing Default plugin's complete
// audience, or the exact audience EnsureDefaultPlugin would seed if missing,
// without creating rows or suppressing the established creation audit.
func (g *Guard) CheckProspectiveDefaultAttachment(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, mcpServerID uuid.UUID) error {
	plugin, err := pluginsrepo.New(tx).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: organizationID, ProjectID: projectID})
	if err == nil {
		return g.CheckAttachment(ctx, tx, rollout, rolloutErr, organizationID, projectID, plugin.ID, mcpServerID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return unavailable(fmt.Errorf("resolve prospective default plugin: %w", err))
	}
	isDefaultProject, err := pluginsrepo.New(tx).IsDefaultProject(ctx, pluginsrepo.IsDefaultProjectParams{OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return unavailable(fmt.Errorf("resolve prospective default plugin audience: %w", err))
	}
	var desired []string
	if isDefaultProject {
		desired = []string{urn.PrincipalWildcard}
	}
	return g.CheckAttachmentWithSeededAudience(ctx, tx, rollout, rolloutErr, organizationID, projectID, mcpServerID, desired)
}

// CheckPluginAudience validates the complete desired assignment set against
// every in-scope MCP currently attached to one exact plugin.
func (g *Guard) CheckPluginAudience(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, pluginID uuid.UUID, desired []string) error {
	targets, err := platformrepo.New(tx).ListDirectRemoteAdmissionTargetsForPlugin(ctx, platformrepo.ListDirectRemoteAdmissionTargetsForPluginParams{
		PluginID:       pluginID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return unavailable(fmt.Errorf("list direct-remote plugin targets: %w", err))
	}
	if len(targets) == 0 {
		return nil
	}
	if err := requireUsableRollout(rollout, rolloutErr); err != nil {
		return err
	}
	for _, target := range targets {
		if !target.RemoteUrl.Valid || target.RemoteUrl.String == "" {
			return unavailable(errors.New("direct-remote plugin target has no live remote URL"))
		}
		if err := g.checkURL(ctx, tx, rollout, organizationID, projectID, target.RemoteUrl.String, desired); err != nil {
			return err
		}
	}
	return nil
}

// CheckMCPServerTarget validates the proposed live URL against every current
// plugin audience for one provenance-bound MCP server. When targetChange is true
// the kill switch blocks the operation even if the server currently reaches no
// audience.
func (g *Guard) CheckMCPServerTarget(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, mcpServerID uuid.UUID, proposedURL string, targetChange bool) error {
	currentURL, scoped, err := directRemoteTarget(ctx, tx, organizationID, projectID, mcpServerID)
	if err != nil {
		return unavailable(err)
	}
	if !scoped {
		return nil
	}
	rows, err := platformrepo.New(tx).ListDirectRemoteAdmissionAudiencesForMCPServer(ctx, platformrepo.ListDirectRemoteAdmissionAudiencesForMCPServerParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
	})
	if err != nil {
		return unavailable(fmt.Errorf("list direct-remote MCP audiences: %w", err))
	}

	audiences := make(map[uuid.UUID][]string)
	for _, row := range rows {
		if !row.PluginID.Valid {
			continue
		}
		if _, ok := audiences[row.PluginID.UUID]; !ok {
			audiences[row.PluginID.UUID] = nil
		}
		if row.PrincipalUrn.Valid {
			audiences[row.PluginID.UUID] = append(audiences[row.PluginID.UUID], row.PrincipalUrn.String)
		}
	}
	if targetChange {
		if err := requireUsableRollout(rollout, rolloutErr); err != nil {
			return err
		}
		if rollout.DirectRemoteDistributionDisabled {
			return ErrDistributionDisabled
		}
	}
	if len(audiences) == 0 {
		return nil
	}
	if err := requireUsableRollout(rollout, rolloutErr); err != nil {
		return err
	}
	if targetChange && proposedURL == "" {
		if rollout.Mode == ModeEnforce {
			return unavailable(errors.New("attached direct-remote MCP cannot switch to an unresolved backend"))
		}
		return nil
	}
	if proposedURL == "" {
		proposedURL = currentURL
	}
	if proposedURL == "" {
		return unavailable(errors.New("direct-remote provenance has no live remote target"))
	}
	for _, audience := range audiences {
		if err := g.checkURL(ctx, tx, rollout, organizationID, projectID, proposedURL, audience); err != nil {
			return err
		}
	}
	return nil
}

// CheckRemoteTarget validates a proposed URL for every provenance-bound MCP
// server currently using one remote source.
func (g *Guard) CheckRemoteTarget(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, rolloutErr error, organizationID string, projectID, remoteMCPServerID uuid.UUID, proposedURL string) error {
	serverIDs, err := platformrepo.New(tx).ListDirectRemoteAdmissionMCPServersForRemote(ctx, platformrepo.ListDirectRemoteAdmissionMCPServersForRemoteParams{
		OrganizationID:    organizationID,
		ProjectID:         projectID,
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteMCPServerID, Valid: true},
	})
	if err != nil {
		return unavailable(fmt.Errorf("list direct-remote MCPs for remote target: %w", err))
	}
	if len(serverIDs) == 0 {
		return nil
	}
	for _, serverID := range serverIDs {
		if err := g.CheckMCPServerTarget(ctx, tx, rollout, rolloutErr, organizationID, projectID, serverID, proposedURL, false); err != nil {
			return err
		}
	}
	return nil
}

func (g *Guard) checkURL(ctx context.Context, tx pgx.Tx, rollout RolloutConfig, organizationID string, projectID uuid.UUID, rawURL string, desired []string) error {
	if rollout.DirectRemoteDistributionDisabled {
		return ErrDistributionDisabled
	}
	if rollout.Mode == ModeLegacy {
		return nil
	}
	canonical, ok := shadowmcp.CanonicalizeInventoryURL(rawURL)
	if !ok {
		if rollout.Mode == ModeReport {
			return nil
		}
		return unavailable(errors.New("direct-remote URL is not canonicalizable"))
	}
	verdict, err := Check(ctx, tx, organizationID, projectID, canonical.CanonicalURL, desired)
	if err != nil {
		if rollout.Mode == ModeReport {
			return nil
		}
		return unavailable(err)
	}
	if rollout.Mode == ModeEnforce && verdict.State == StateApprovalRequired {
		return ErrApprovalRequired
	}
	return nil
}

func directRemoteTarget(ctx context.Context, tx pgx.Tx, organizationID string, projectID, mcpServerID uuid.UUID) (string, bool, error) {
	row, err := platformrepo.New(tx).GetDirectRemoteAdmissionTargetForMCPServer(ctx, platformrepo.GetDirectRemoteAdmissionTargetForMCPServerParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve direct-remote admission target: %w", err)
	}
	if !row.RemoteMcpServerID.Valid || !row.RemoteUrl.Valid || row.RemoteUrl.String == "" {
		return "", true, nil
	}
	return row.RemoteUrl.String, true, nil
}

func listPluginAssignments(ctx context.Context, tx pgx.Tx, organizationID string, projectID, pluginID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT assignment.principal_urn
FROM plugin_assignments AS assignment
JOIN plugins AS plugin
  ON plugin.id = assignment.plugin_id
 AND plugin.organization_id = assignment.organization_id
 AND plugin.deleted IS FALSE
WHERE assignment.plugin_id = $1
  AND assignment.organization_id = $2
  AND plugin.project_id = $3
ORDER BY assignment.principal_urn`, pluginID, organizationID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list complete plugin audience: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var principal string
		if err := rows.Scan(&principal); err != nil {
			return nil, fmt.Errorf("scan plugin audience: %w", err)
		}
		result = append(result, principal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plugin audience: %w", err)
	}
	return result, nil
}

func requireUsableRollout(config RolloutConfig, rolloutErr error) error {
	if rolloutErr != nil {
		return unavailable(rolloutErr)
	}
	switch config.Mode {
	case ModeLegacy, ModeReport, ModeEnforce:
		return nil
	default:
		return unavailable(errors.New("distribution rollout mode is invalid"))
	}
}

func unavailable(cause error) error {
	if cause == nil {
		cause = ErrUnavailable
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, cause)
}
