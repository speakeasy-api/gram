//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
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
	ErrDistributionInvalid           = errors.New("invalid platform mcp distribution input")
	ErrDistributionConflict          = errors.New("platform mcp distribution version conflict")
	ErrDistributionNotReady          = errors.New("platform mcp distribution requires fresh readiness")
	ErrDistributionDefaultAbsent     = errors.New("platform mcp distribution requires an existing default plugin")
	ErrDistributionTargetUnavailable = errors.New("platform mcp distribution target is unavailable")
)

// DistributionInput deliberately identifies only the project selected by its
// slug. The active onboarding workflow supplies the registration, MCP server,
// and Default plugin so callers cannot retarget a distribution with internal
// identifiers.
type DistributionInput struct {
	ProjectSlug     string
	ExpectedVersion int64
}

// Distribution is the bounded state used by management and MCP tool adapters.
// Version is internal to the application service; external adapters must turn
// it into a server-issued opaque token before returning it to callers.
type Distribution struct {
	State            string
	Version          int64
	AttachmentLive   bool
	PublicationState string
}

// ExistingDefaultPluginAttacher delegates attachment to the Plugins package.
// Keeping this narrow adapter outside platformmcp avoids an import cycle: the
// plugin publisher already consumes Platform MCP package-admission policy.
type ExistingDefaultPluginAttacher func(context.Context, pgx.Tx, *contextvalues.AuthContext, string, uuid.UUID, uuid.UUID, string) (uuid.UUID, bool, error)

// ProjectPublisher is a post-commit adapter to the existing plugin publisher.
// The caller supplies only bounded identity and intent; no provider values or
// package details cross the Platform MCP boundary.
type ProjectPublisher func(context.Context, uuid.UUID, string, string) error

// DistributionService changes only the selected workflow's attachment to the
// existing Default plugin. plugin_servers remains the attachment authority;
// platform_mcp_distributions records the caller-bound lifecycle projection.
type DistributionService struct {
	db      *pgxpool.Pool
	audit   *audit.Logger
	attach  ExistingDefaultPluginAttacher
	publish ProjectPublisher
	now     func() time.Time
}

func NewDistributionService(db *pgxpool.Pool, auditLogger *audit.Logger, attach ExistingDefaultPluginAttacher, publish ProjectPublisher) *DistributionService {
	if auditLogger == nil {
		auditLogger = audit.NewLogger()
	}
	return &DistributionService{db: db, audit: auditLogger, attach: attach, publish: publish, now: time.Now}
}

// Current returns the selected workflow target's live attachment state and its
// last persisted version. It does not require readiness, so dashboard resume can
// safely project the state before offering a mutation.
func (s *DistributionService) Current(ctx context.Context, principal Principal, projectSlug string) (Distribution, error) {
	if s == nil || s.db == nil || projectSlug == "" {
		return Distribution{}, ErrDistributionInvalid
	}
	q := repo.New(s.db)
	target, err := s.onboardingTarget(ctx, q, principal, projectSlug)
	if err != nil {
		return Distribution{}, err
	}
	plugin, err := pluginsrepo.New(s.db).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      target.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, ErrDistributionDefaultAbsent
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("get current platform mcp distribution default plugin: %w", err)
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
		return Distribution{AttachmentLive: attached, PublicationState: publicationStatePending}, nil
	}
	return Distribution{State: row.State, Version: row.Version, AttachmentLive: attached, PublicationState: row.PublicationState}, nil
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
	if err := s.requireFreshReadiness(ctx, q, principal, target.ProjectID, target.RegistrationID.UUID, presentConnection(connectionID), presentConnection(generation)); err != nil {
		return Distribution{}, err
	}

	plugin, err := pluginsrepo.New(tx).GetDefaultPluginForUpdate(ctx, pluginsrepo.GetDefaultPluginForUpdateParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      target.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, ErrDistributionDefaultAbsent
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("lock platform mcp distribution default plugin: %w", err)
	}

	if err := q.LockPlatformMCPDistribution(ctx, repo.LockPlatformMCPDistributionParams{
		OrganizationID:  principal.OrganizationID,
		ProjectID:       target.ProjectID.String(),
		RegistrationID:  target.RegistrationID.UUID.String(),
		DefaultPluginID: plugin.ID.String(),
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
		pluginServerID, attached, err := s.attach(ctx, tx, distributionAuthContext(principal), principal.OrganizationID, target.ProjectID, target.McpServerID.UUID, distributionDisplayName(target))
		if err != nil {
			return Distribution{}, fmt.Errorf("attach platform mcp to existing default plugin: %w", err)
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
		existing.ConnectionID == presentConnection(connectionID) &&
		existing.ConnectionGeneration == presentConnection(generation) {
		if err := tx.Commit(ctx); err != nil {
			return Distribution{}, fmt.Errorf("commit idempotent platform mcp distribution: %w", err)
		}
		return Distribution{State: existing.State, Version: existing.Version, AttachmentLive: true, PublicationState: existing.PublicationState}, nil
	}

	row, err := persistDistribution(ctx, q, existing, found, distributionPersistenceInput{
		organizationID:       principal.OrganizationID,
		projectID:            target.ProjectID,
		registrationID:       target.RegistrationID.UUID,
		defaultPluginID:      plugin.ID,
		pluginServerID:       uuid.NullUUID{UUID: live.ID, Valid: true},
		state:                distributionStateAttached,
		attachmentWasCreated: created,
		connectionID:         presentConnection(connectionID),
		connectionGeneration: presentConnection(generation),
	})
	if err != nil {
		return Distribution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Distribution{}, fmt.Errorf("commit platform mcp distribution: %w", err)
	}
	return s.publishCommittedDistribution(ctx, principal, row, "Distribute Platform MCP to Default plugin")
}

// DistributeForOnboarding delegates to the same explicit-project distribution
// path used by the dashboard. The active workflow supplies the registered MCP;
// callers cannot target an arbitrary server or create a Default plugin.
func (s *DistributionService) DistributeForOnboarding(ctx context.Context, principal Principal, projectSlug string) (Distribution, error) {
	current, err := s.Current(ctx, principal, projectSlug)
	if err != nil {
		return Distribution{}, err
	}
	return s.Distribute(ctx, principal, DistributionInput{
		ProjectSlug:     projectSlug,
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
	plugin, err := pluginsrepo.New(tx).GetDefaultPluginForUpdate(ctx, pluginsrepo.GetDefaultPluginForUpdateParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      target.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, ErrDistributionDefaultAbsent
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("lock platform mcp distribution default plugin for removal: %w", err)
	}

	if err := q.LockPlatformMCPDistribution(ctx, repo.LockPlatformMCPDistributionParams{
		OrganizationID:  principal.OrganizationID,
		ProjectID:       target.ProjectID.String(),
		RegistrationID:  target.RegistrationID.UUID.String(),
		DefaultPluginID: plugin.ID.String(),
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
		return Distribution{State: existing.State, Version: existing.Version, AttachmentLive: err == nil, PublicationState: existing.PublicationState}, nil
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
		defaultPluginID:      plugin.ID,
		pluginServerID:       uuid.NullUUID{},
		state:                distributionStateRemoved,
		attachmentWasCreated: false,
		connectionID:         presentConnection(connectionID),
		connectionGeneration: presentConnection(generation),
	})
	if err != nil {
		return Distribution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Distribution{}, fmt.Errorf("commit platform mcp distribution removal: %w", err)
	}
	if _, err := s.publishCommittedDistribution(ctx, principal, row, "Remove Platform MCP from Default plugin"); err != nil {
		return Distribution{}, err
	}
	// Current reads the attachment authority after publication, including an
	// administration-owned attachment intentionally preserved above.
	return s.Current(ctx, principal, input.ProjectSlug)
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
	plugin, err := pluginsrepo.New(s.db).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: target.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{}, ErrDistributionDefaultAbsent
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("get platform mcp default plugin for publication repair: %w", err)
	}
	row, found, err := getDistribution(ctx, q, principal.OrganizationID, target.ProjectID, target.RegistrationID.UUID, plugin.ID)
	if err != nil {
		return Distribution{}, err
	}
	if !found || row.Version != input.ExpectedVersion {
		return Distribution{}, ErrDistributionConflict
	}
	return s.publishCommittedDistribution(ctx, principal, row, "Repair Platform MCP Default plugin publication")
}

func (s *DistributionService) publishCommittedDistribution(ctx context.Context, principal Principal, row repo.PlatformMcpDistribution, commitMessage string) (Distribution, error) {
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
		DefaultPluginID:  row.DefaultPluginID,
		Version:          row.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Distribution{State: row.State, Version: row.Version, AttachmentLive: row.State == distributionStateAttached, PublicationState: publicationStatePending}, nil
	}
	if err != nil {
		return Distribution{}, fmt.Errorf("record platform mcp publication outcome: %w", err)
	}
	return Distribution{State: updated.State, Version: updated.Version, AttachmentLive: updated.State == distributionStateAttached, PublicationState: updated.PublicationState}, nil
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

func (s *DistributionService) requireFreshReadiness(ctx context.Context, q *repo.Queries, principal Principal, projectID, registrationID uuid.UUID, connectionID, generation uuid.NullUUID) error {
	readiness, err := q.GetLatestPlatformMCPReadinessForLifecycle(ctx, repo.GetLatestPlatformMCPReadinessForLifecycleParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            projectID,
		RegistrationID:       registrationID,
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
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
	defaultPluginID      uuid.UUID
	pluginServerID       uuid.NullUUID
	state                string
	attachmentWasCreated bool
	connectionID         uuid.NullUUID
	connectionGeneration uuid.NullUUID
}

func getDistribution(ctx context.Context, q *repo.Queries, organizationID string, projectID, registrationID, defaultPluginID uuid.UUID) (repo.PlatformMcpDistribution, bool, error) {
	row, err := q.GetPlatformMCPDistribution(ctx, repo.GetPlatformMCPDistributionParams{
		OrganizationID:  organizationID,
		ProjectID:       projectID,
		RegistrationID:  registrationID,
		DefaultPluginID: defaultPluginID,
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
			DefaultPluginID:      input.defaultPluginID,
			PluginServerID:       input.pluginServerID,
			State:                input.state,
			Version:              1,
			AttachmentWasCreated: input.attachmentWasCreated,
			ConnectionID:         input.connectionID,
			ConnectionGeneration: input.connectionGeneration,
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
		ConnectionID:         input.connectionID,
		ConnectionGeneration: input.connectionGeneration,
		ID:                   existing.ID,
		OrganizationID:       input.organizationID,
		ProjectID:            input.projectID,
		RegistrationID:       input.registrationID,
		DefaultPluginID:      input.defaultPluginID,
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
