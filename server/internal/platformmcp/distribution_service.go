//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	distributionStateAttached = "attached"
	distributionStateRemoved  = "removed"

	publicationStatePending        = "pending"
	publicationStateCurrent        = "current"
	publicationStateRepairRequired = "repair_required"
)

var (
	ErrDistributionInvalid                = errors.New("invalid platform mcp distribution input")
	ErrDistributionConflict               = errors.New("platform mcp distribution version conflict")
	ErrDistributionNotReady               = errors.New("platform mcp distribution requires fresh readiness")
	ErrDistributionDefaultAbsent          = errors.New("platform mcp distribution requires an existing default plugin")
	ErrDistributionTargetUnavailable      = errors.New("platform mcp distribution target is unavailable")
	ErrDistributionBlockedPendingApproval = errors.New("platform mcp distribution blocked by Shadow MCP approval enforcement")
)

// DistributionInput identifies the project selected by its slug and the plugin
// inside it that receives the distribution. The active onboarding workflow
// still supplies the registration and MCP server, so a caller can choose which
// existing plugin receives an MCP but never which server is distributed.
type DistributionInput struct {
	// ProjectSlug is the explicit project the distribution acts in.
	ProjectSlug string

	// Plugin names an exact existing plugin by id, slug, or name. Empty selects
	// the project's default plugin, which is what the dashboard's own
	// "add to Default plugin" action asks for; the agent-facing tools require a
	// named target so a mistyped plugin is refused rather than silently
	// redirected.
	Plugin string

	// ExpectedVersion is the distribution version the caller read before
	// writing.
	ExpectedVersion int64
}

// Distribution is the bounded state used by management and MCP tool adapters.
// Version is internal to the application service; external adapters must turn
// it into a server-issued opaque token before returning it to callers.
type Distribution struct {
	// State is the recorded lifecycle state: attached or removed.
	State string

	// Version is the distribution version this result reflects.
	Version int64

	// AttachmentLive is whether the plugin currently carries the MCP.
	AttachmentLive bool

	// PublicationState is how far the package publication got.
	PublicationState string

	// Plugin is the name of the plugin the caller's target resolved to, echoed
	// so a caller sees where its distribution landed rather than inferring it.
	Plugin string
}

// ExistingPluginAttacher delegates attachment to the Plugins package. Keeping
// this narrow adapter outside platformmcp avoids an import cycle: the plugin
// publisher already consumes Platform MCP package-admission policy.
type ExistingPluginAttacher func(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, projectID, pluginID, mcpServerID uuid.UUID, displayName string) (uuid.UUID, bool, error)

// PluginTargetResolver resolves the exact plugin a distribution names. It is
// the same resolution the plugin inventory tools expose, so a target that
// list_plugins shows is a target a distribution can name.
type PluginTargetResolver interface {
	ResolvePlugin(ctx context.Context, principal Principal, projectID uuid.UUID, wanted string) (PluginRef, error)
}

// ProjectPublisher is a post-commit adapter to the existing plugin publisher.
// The caller supplies only bounded identity and intent; no provider values or
// package details cross the Platform MCP boundary.
type ProjectPublisher func(context.Context, uuid.UUID, string, string) error

// DistributionService changes only the selected workflow's attachment to the
// existing Default plugin. plugin_servers remains the attachment authority;
// platform_mcp_distributions records the caller-bound lifecycle projection.
type DistributionService struct {
	db        *pgxpool.Pool
	audit     *audit.Logger
	attach    ExistingPluginAttacher
	plugins   PluginTargetResolver
	publish   ProjectPublisher
	now       func() time.Time
	approvals DirectRemoteApprovalTxChecker
}

func NewDistributionService(db *pgxpool.Pool, auditLogger *audit.Logger, attach ExistingPluginAttacher, publish ProjectPublisher, plugins PluginTargetResolver) *DistributionService {
	if auditLogger == nil {
		auditLogger = audit.NewLogger()
	}
	return &DistributionService{db: db, audit: auditLogger, attach: attach, publish: publish, plugins: plugins, now: time.Now, approvals: NewPostgresDirectRemoteApprovals()}
}

// Current returns the selected workflow target's live attachment state and its
// last persisted version. It does not require readiness, so dashboard resume can
// safely project the state before offering a mutation.
func (s *DistributionService) Current(ctx context.Context, principal Principal, projectSlug, targetPlugin string) (Distribution, error) {
	if s == nil || s.db == nil || projectSlug == "" {
		return Distribution{}, ErrDistributionInvalid
	}
	q := repo.New(s.db)
	target, err := s.onboardingTarget(ctx, q, principal, projectSlug)
	if err != nil {
		return Distribution{}, err
	}
	plugin, err := s.resolvePlugin(ctx, s.db, principal, target.ProjectID, targetPlugin, false)
	if err != nil {
		return Distribution{}, err
	}
	row, found, err := getDistribution(ctx, q, principal.OrganizationID, target.ProjectID, target.RegistrationID.UUID, plugin.ID)
	if err != nil {
		return Distribution{}, err
	}
	live, err := pluginsrepo.New(s.db).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    plugin.ID,
		McpServerID: uuid.NullUUID{UUID: target.McpServerID.UUID, Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, fmt.Errorf("get current platform mcp plugin attachment: %w", err)
	}
	attached := err == nil && live.ID != uuid.Nil
	if !found {
		return Distribution{AttachmentLive: attached, PublicationState: publicationStatePending, Plugin: plugin.Name}, nil
	}
	return Distribution{State: row.State, Version: row.Version, AttachmentLive: attached, PublicationState: row.PublicationState, Plugin: plugin.Name}, nil
}

func (s *DistributionService) Distribute(ctx context.Context, principal Principal, input DistributionInput) (Distribution, error) {
	if s == nil || s.db == nil || s.audit == nil || s.attach == nil || input.ProjectSlug == "" || input.ExpectedVersion < 0 {
		return Distribution{}, ErrDistributionInvalid
	}

	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return Distribution{}, ErrDistributionInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Distribution{}, fmt.Errorf("begin platform mcp distribution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	target, err := s.onboardingTarget(ctx, q, principal, input.ProjectSlug)
	if err != nil {
		return Distribution{}, err
	}
	if err := s.requireApprovedDirectRemoteDistribution(ctx, tx, q, principal, target); err != nil {
		return Distribution{}, err
	}
	if err := s.requireFreshReadiness(ctx, q, principal, target.ProjectID, target.RegistrationID.UUID, connectionID, generation); err != nil {
		return Distribution{}, err
	}

	plugin, err := s.resolvePlugin(ctx, tx, principal, target.ProjectID, input.Plugin, true)
	if err != nil {
		return Distribution{}, err
	}

	if err := q.LockPlatformMCPDistribution(ctx, repo.LockPlatformMCPDistributionParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      target.ProjectID.String(),
		RegistrationID: target.RegistrationID.UUID.String(),
		PluginID:       plugin.ID.String(),
	}); err != nil {
		return Distribution{}, fmt.Errorf("lock platform mcp distribution: %w", err)
	}

	existing, found, err := getDistribution(ctx, q, principal.OrganizationID, target.ProjectID, target.RegistrationID.UUID, plugin.ID)
	if err != nil {
		return Distribution{}, err
	}
	if err := requireExpectedDistributionVersion(found, existing.Version, input.ExpectedVersion); err != nil {
		return Distribution{}, err
	}

	pluginQueries := pluginsrepo.New(tx)
	live, err := pluginQueries.GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    plugin.ID,
		McpServerID: uuid.NullUUID{UUID: target.McpServerID.UUID, Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, fmt.Errorf("get live platform mcp plugin attachment: %w", err)
	}

	// Preserve ownership across reauthorization only while the exact attachment
	// created by this workflow remains live. An administrator-created replacement
	// for a deleted workflow attachment remains administration-owned.
	created := found &&
		existing.AttachmentWasCreated &&
		existing.PluginServerID.Valid &&
		existing.PluginServerID.UUID == live.ID
	if errors.Is(err, pgx.ErrNoRows) {
		pluginServerID, attached, err := s.attach(ctx, tx, distributionAuthContext(principal), principal.OrganizationID, target.ProjectID, plugin.ID, target.McpServerID.UUID, distributionDisplayName(target))
		if err != nil {
			return Distribution{}, fmt.Errorf("attach platform mcp to existing plugin: %w", err)
		}
		if !attached || pluginServerID == uuid.Nil {
			return Distribution{}, ErrDistributionConflict
		}
		live.ID = pluginServerID
		created = true
	}
	if found &&
		existing.State == distributionStateAttached &&
		existing.PluginServerID.Valid &&
		existing.PluginServerID.UUID == live.ID &&
		existing.ConnectionID.Valid &&
		existing.ConnectionID.UUID == connectionID &&
		existing.ConnectionGeneration.Valid &&
		existing.ConnectionGeneration.UUID == generation {
		if err := tx.Commit(ctx); err != nil {
			return Distribution{}, fmt.Errorf("commit idempotent platform mcp distribution: %w", err)
		}
		return Distribution{State: existing.State, Version: existing.Version, AttachmentLive: true, PublicationState: existing.PublicationState, Plugin: plugin.Name}, nil
	}

	row, err := persistDistribution(ctx, q, existing, found, distributionPersistenceInput{
		organizationID:       principal.OrganizationID,
		projectID:            target.ProjectID,
		registrationID:       target.RegistrationID.UUID,
		pluginID:             plugin.ID,
		pluginServerID:       uuid.NullUUID{UUID: live.ID, Valid: true},
		state:                distributionStateAttached,
		attachmentWasCreated: created,
		connectionID:         connectionID,
		connectionGeneration: generation,
	})
	if err != nil {
		return Distribution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Distribution{}, fmt.Errorf("commit platform mcp distribution: %w", err)
	}
	return s.publishCommittedDistribution(ctx, principal, row, plugin.Name, "Distribute Platform MCP to "+plugin.Name+" plugin")
}

// DistributeForOnboarding delegates to the same explicit-project distribution
// path used by the dashboard. The active workflow supplies the registered MCP;
// callers cannot target an arbitrary server or create a Default plugin.
func (s *DistributionService) DistributeForOnboarding(ctx context.Context, principal Principal, projectSlug, targetPlugin string) (Distribution, error) {
	current, err := s.Current(ctx, principal, projectSlug, targetPlugin)
	if err != nil {
		return Distribution{}, err
	}
	return s.Distribute(ctx, principal, DistributionInput{
		ProjectSlug:     projectSlug,
		Plugin:          targetPlugin,
		ExpectedVersion: current.Version,
	})
}

func (s *DistributionService) Remove(ctx context.Context, principal Principal, input DistributionInput) (Distribution, error) {
	if s == nil || s.db == nil || s.audit == nil || input.ProjectSlug == "" || input.ExpectedVersion < 0 {
		return Distribution{}, ErrDistributionInvalid
	}

	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return Distribution{}, ErrDistributionInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Distribution{}, fmt.Errorf("begin platform mcp distribution removal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := repo.New(tx)
	target, err := s.onboardingTarget(ctx, q, principal, input.ProjectSlug)
	if err != nil {
		return Distribution{}, err
	}
	plugin, err := s.resolvePlugin(ctx, tx, principal, target.ProjectID, input.Plugin, true)
	if err != nil {
		return Distribution{}, err
	}

	if err := q.LockPlatformMCPDistribution(ctx, repo.LockPlatformMCPDistributionParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      target.ProjectID.String(),
		RegistrationID: target.RegistrationID.UUID.String(),
		PluginID:       plugin.ID.String(),
	}); err != nil {
		return Distribution{}, fmt.Errorf("lock platform mcp distribution removal: %w", err)
	}

	existing, found, err := getDistribution(ctx, q, principal.OrganizationID, target.ProjectID, target.RegistrationID.UUID, plugin.ID)
	if err != nil {
		return Distribution{}, err
	}
	if err := requireExpectedDistributionVersion(found, existing.Version, input.ExpectedVersion); err != nil {
		return Distribution{}, err
	}

	pluginQueries := pluginsrepo.New(tx)
	live, err := pluginQueries.GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:    plugin.ID,
		McpServerID: uuid.NullUUID{UUID: target.McpServerID.UUID, Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, fmt.Errorf("get live platform mcp plugin attachment for removal: %w", err)
	}
	if found && existing.State == distributionStateRemoved && (errors.Is(err, pgx.ErrNoRows) || !existing.AttachmentWasCreated) {
		if err := tx.Commit(ctx); err != nil {
			return Distribution{}, fmt.Errorf("commit idempotent platform mcp distribution removal: %w", err)
		}
		return Distribution{State: existing.State, Version: existing.Version, AttachmentLive: err == nil, PublicationState: existing.PublicationState, Plugin: plugin.Name}, nil
	}
	// A Default plugin may already have contained this MCP when Platform MCP
	// began tracking the onboarding distribution. Only remove attachments this
	// workflow created; pre-existing administration-owned attachments remain live.
	if err == nil && existing.AttachmentWasCreated && existing.PluginServerID.Valid && existing.PluginServerID.UUID == live.ID {
		removed, err := pluginQueries.RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: live.ID, PluginID: plugin.ID})
		if err != nil {
			return Distribution{}, fmt.Errorf("remove platform mcp plugin attachment: %w", err)
		}
		mcpServerURN := urn.NewMcpServer(target.McpServerID.UUID)
		if err := s.audit.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        target.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
			ActorSlug:        nil,
			PluginID:         plugin.ID,
			PluginName:       plugin.Name,
			PluginSlug:       plugin.Slug,
			ServerID:         removed.ID,
			ToolsetURN:       nil,
			McpServerURN:     &mcpServerURN,
		}); err != nil {
			return Distribution{}, fmt.Errorf("audit platform mcp plugin removal: %w", err)
		}
	}

	row, err := persistDistribution(ctx, q, existing, found, distributionPersistenceInput{
		organizationID:       principal.OrganizationID,
		projectID:            target.ProjectID,
		registrationID:       target.RegistrationID.UUID,
		pluginID:             plugin.ID,
		pluginServerID:       uuid.NullUUID{},
		state:                distributionStateRemoved,
		attachmentWasCreated: false,
		connectionID:         connectionID,
		connectionGeneration: generation,
	})
	if err != nil {
		return Distribution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Distribution{}, fmt.Errorf("commit platform mcp distribution removal: %w", err)
	}
	if _, err := s.publishCommittedDistribution(ctx, principal, row, plugin.Name, "Remove Platform MCP from "+plugin.Name+" plugin"); err != nil {
		return Distribution{}, err
	}
	// Current reads the attachment authority after publication, including an
	// administration-owned attachment intentionally preserved above.
	return s.Current(ctx, principal, input.ProjectSlug, input.Plugin)
}

// RepairPublication replays the same post-commit desired-state publication
// without changing the attachment or distribution version.
func (s *DistributionService) RepairPublication(ctx context.Context, principal Principal, input DistributionInput) (Distribution, error) {
	if s == nil || s.db == nil || input.ProjectSlug == "" || input.ExpectedVersion <= 0 {
		return Distribution{}, ErrDistributionInvalid
	}
	q := repo.New(s.db)
	target, err := s.onboardingTarget(ctx, q, principal, input.ProjectSlug)
	if err != nil {
		return Distribution{}, err
	}
	plugin, err := s.resolvePlugin(ctx, s.db, principal, target.ProjectID, input.Plugin, false)
	if err != nil {
		return Distribution{}, err
	}
	row, found, err := getDistribution(ctx, q, principal.OrganizationID, target.ProjectID, target.RegistrationID.UUID, plugin.ID)
	if err != nil {
		return Distribution{}, err
	}
	if !found || row.Version != input.ExpectedVersion {
		return Distribution{}, ErrDistributionConflict
	}
	return s.publishCommittedDistribution(ctx, principal, row, plugin.Name, "Repair Platform MCP "+plugin.Name+" plugin publication")
}

func (s *DistributionService) publishCommittedDistribution(ctx context.Context, principal Principal, row repo.PlatformMcpDistribution, pluginName, commitMessage string) (Distribution, error) {
	publicationState := publicationStateRepairRequired
	if s.publish != nil && s.publish(ctx, row.ProjectID, principal.UserID, commitMessage) == nil {
		publicationState = publicationStateCurrent
	}
	updated, err := repo.New(s.db).UpdatePlatformMCPDistributionPublication(ctx, repo.UpdatePlatformMCPDistributionPublicationParams{
		PublicationState: publicationState,
		ID:               row.ID,
		OrganizationID:   row.OrganizationID,
		ProjectID:        row.ProjectID,
		RegistrationID:   row.RegistrationID,
		PluginID:         row.DefaultPluginID,
		Version:          row.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{State: row.State, Version: row.Version, AttachmentLive: row.State == distributionStateAttached, PublicationState: publicationStatePending, Plugin: pluginName}, nil
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("record platform mcp publication outcome: %w", err)
	}
	return Distribution{State: updated.State, Version: updated.Version, AttachmentLive: updated.State == distributionStateAttached, PublicationState: updated.PublicationState, Plugin: pluginName}, nil
}

func (s *DistributionService) onboardingTarget(ctx context.Context, q *repo.Queries, principal Principal, projectSlug string) (repo.GetPlatformMCPOnboardingDistributionTargetRow, error) {
	target, err := q.GetPlatformMCPOnboardingDistributionTarget(ctx, repo.GetPlatformMCPOnboardingDistributionTargetParams{
		OrganizationID:       principal.OrganizationID,
		InitiatingSubjectUrn: userSubjectURN(principal.UserID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.GetPlatformMCPOnboardingDistributionTargetRow{}, ErrDistributionInvalid
	}
	if err != nil {
		return repo.GetPlatformMCPOnboardingDistributionTargetRow{}, fmt.Errorf("resolve platform mcp onboarding distribution target: %w", err)
	}
	if target.ProjectSlug != projectSlug || !target.RegistrationID.Valid || !target.McpServerID.Valid {
		return repo.GetPlatformMCPOnboardingDistributionTargetRow{}, ErrDistributionInvalid
	}
	return target, nil
}

// requireApprovedDirectRemoteDistribution is the enforcement chokepoint for
// user-supplied URLs. It runs before readiness because an open server can be
// fresh-ready anonymously, which is insufficient to override an organization's
// existing Shadow MCP policy. Reviewed catalogue registrations are unaffected.
func (s *DistributionService) requireApprovedDirectRemoteDistribution(ctx context.Context, tx pgx.Tx, q *repo.Queries, principal Principal, target repo.GetPlatformMCPOnboardingDistributionTargetRow) error {
	registration, err := lifecycleRegistration(ctx, q, principal, target.ProjectID, target.RegistrationID.UUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDistributionInvalid
	}
	if err != nil {
		return fmt.Errorf("resolve direct remote distribution registration: %w", err)
	}
	if registration.CatalogProvider != directRemoteProviderKey {
		return nil
	}
	if s.approvals == nil || registration.CatalogReference == "" {
		return ErrDistributionBlockedPendingApproval
	}
	approval, err := s.approvals.CheckDirectRemoteApprovalTx(ctx, tx, principal.OrganizationID, principal.UserID, target.ProjectID, registration.CatalogReference)
	if err != nil {
		return fmt.Errorf("consult direct remote approval enforcement for distribution: %w", err)
	}
	if approval.EnforcementActive && !approval.Approved {
		return ErrDistributionBlockedPendingApproval
	}
	return nil
}

func (s *DistributionService) requireFreshReadiness(ctx context.Context, q *repo.Queries, principal Principal, projectID, registrationID, connectionID, generation uuid.UUID) error {
	readiness, err := q.GetLatestPlatformMCPReadinessForLifecycle(ctx, repo.GetLatestPlatformMCPReadinessForLifecycleParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            projectID,
		RegistrationID:       registrationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		SubjectUrn:           userSubjectURN(principal.UserID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDistributionNotReady
	}
	if err != nil {
		return fmt.Errorf("get platform mcp distribution readiness: %w", err)
	}
	if readiness.State != string(ReadinessReady) || !readiness.ExpiresAt.Valid || !readiness.ExpiresAt.Time.After(s.now()) {
		return ErrDistributionNotReady
	}
	return nil
}

type distributionPersistenceInput struct {
	organizationID       string
	projectID            uuid.UUID
	registrationID       uuid.UUID
	pluginID             uuid.UUID
	pluginServerID       uuid.NullUUID
	state                string
	attachmentWasCreated bool
	connectionID         uuid.UUID
	connectionGeneration uuid.UUID
}

func getDistribution(ctx context.Context, q *repo.Queries, organizationID string, projectID, registrationID, pluginID uuid.UUID) (repo.PlatformMcpDistribution, bool, error) {
	row, err := q.GetPlatformMCPDistribution(ctx, repo.GetPlatformMCPDistributionParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		RegistrationID: registrationID,
		PluginID:       pluginID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.PlatformMcpDistribution{}, false, nil
	}
	if err != nil {
		return repo.PlatformMcpDistribution{}, false, fmt.Errorf("get platform mcp distribution: %w", err)
	}
	return row, true, nil
}

func requireExpectedDistributionVersion(found bool, current, expected int64) error {
	if (!found && expected != 0) || (found && expected != current) {
		return ErrDistributionConflict
	}
	return nil
}

func persistDistribution(ctx context.Context, q *repo.Queries, existing repo.PlatformMcpDistribution, found bool, input distributionPersistenceInput) (repo.PlatformMcpDistribution, error) {
	if !found {
		row, err := q.CreatePlatformMCPDistribution(ctx, repo.CreatePlatformMCPDistributionParams{
			OrganizationID:       input.organizationID,
			ProjectID:            input.projectID,
			RegistrationID:       input.registrationID,
			PluginID:             input.pluginID,
			PluginServerID:       input.pluginServerID,
			State:                input.state,
			Version:              1,
			AttachmentWasCreated: input.attachmentWasCreated,
			ConnectionID:         uuid.NullUUID{UUID: input.connectionID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: input.connectionGeneration, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.PlatformMcpDistribution{}, ErrDistributionTargetUnavailable
		}
		if err != nil {
			return repo.PlatformMcpDistribution{}, fmt.Errorf("create platform mcp distribution: %w", err)
		}
		return row, nil
	}
	row, err := q.UpdatePlatformMCPDistribution(ctx, repo.UpdatePlatformMCPDistributionParams{
		PluginServerID:       input.pluginServerID,
		State:                input.state,
		Version:              existing.Version + 1,
		AttachmentWasCreated: input.attachmentWasCreated,
		ConnectionID:         uuid.NullUUID{UUID: input.connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: input.connectionGeneration, Valid: true},
		ID:                   existing.ID,
		OrganizationID:       input.organizationID,
		ProjectID:            input.projectID,
		RegistrationID:       input.registrationID,
		PluginID:             uuid.NullUUID{UUID: input.pluginID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.PlatformMcpDistribution{}, ErrDistributionTargetUnavailable
	}
	if err != nil {
		return repo.PlatformMcpDistribution{}, fmt.Errorf("update platform mcp distribution: %w", err)
	}
	return row, nil
}

func distributionAuthContext(principal Principal) *contextvalues.AuthContext {
	return &contextvalues.AuthContext{
		ActiveOrganizationID: principal.OrganizationID,
		UserID:               principal.UserID,
		Email:                nil,
	}
}

func distributionDisplayName(target repo.GetPlatformMCPOnboardingDistributionTargetRow) string {
	if target.ProjectName != "" {
		return target.ProjectName + " MCP"
	}
	return "Platform MCP"
}

// resolvePlugin resolves the plugin a distribution acts on. An empty target is
// the project's default plugin, which is the dashboard's own action; a named
// target is matched exactly by the same resolver the plugin inventory tools
// use and never falls back to the default. forUpdate serializes the write
// against concurrent deletion of the plugin.
func (s *DistributionService) resolvePlugin(ctx context.Context, db pluginsrepo.DBTX, principal Principal, projectID uuid.UUID, wanted string, forUpdate bool) (PluginRef, error) {
	if strings.TrimSpace(wanted) == "" {
		if forUpdate {
			plugin, err := pluginsrepo.New(db).GetDefaultPluginForUpdate(ctx, pluginsrepo.GetDefaultPluginForUpdateParams{
				OrganizationID: principal.OrganizationID,
				ProjectID:      projectID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return PluginRef{}, ErrDistributionDefaultAbsent
			}
			if err != nil {
				return PluginRef{}, fmt.Errorf("lock platform mcp distribution default plugin: %w", err)
			}
			return PluginRef{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, IsDefault: true}, nil
		}
		plugin, err := pluginsrepo.New(db).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
			OrganizationID: principal.OrganizationID,
			ProjectID:      projectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return PluginRef{}, ErrDistributionDefaultAbsent
		}
		if err != nil {
			return PluginRef{}, fmt.Errorf("get platform mcp distribution default plugin: %w", err)
		}
		return PluginRef{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, IsDefault: true}, nil
	}
	if s.plugins == nil {
		return PluginRef{}, ErrDistributionInvalid
	}
	target, err := s.plugins.ResolvePlugin(ctx, principal, projectID, wanted)
	if err != nil {
		// Returned unwrapped so its typed refusal — not_found or
		// ambiguous_target — survives to the tool result.
		return PluginRef{}, err //nolint:wrapcheck // typed refusal is the contract
	}
	if !forUpdate {
		return target, nil
	}
	locked, err := repo.New(db).GetPlatformMCPPluginForUpdate(ctx, repo.GetPlatformMCPPluginForUpdateParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		PluginID:       target.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginRef{}, ErrPluginNotFound
	}
	if err != nil {
		return PluginRef{}, fmt.Errorf("lock platform mcp distribution plugin: %w", err)
	}
	return PluginRef{ID: locked.ID, Name: locked.Name, Slug: locked.Slug, IsDefault: target.IsDefault}, nil
}
