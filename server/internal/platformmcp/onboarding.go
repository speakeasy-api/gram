//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	onboardingWorkflowLifetime = 24 * time.Hour
	onboardingSourceDashboard  = "dashboard"
)

type OnboardingStage string

const (
	OnboardingStageNotStarted          OnboardingStage = "not_started"
	OnboardingStageInstallInstructions OnboardingStage = "install_instructions"
	OnboardingStageAuthorized          OnboardingStage = "authorized"
	OnboardingStageConnectionReady     OnboardingStage = "connection_ready"
)

var ErrOnboardingInvalid = errors.New("invalid platform mcp onboarding input")

type OnboardingClientFamily string

const (
	OnboardingClientClaudeCode   OnboardingClientFamily = "claude_code"
	OnboardingClientClaudeCowork OnboardingClientFamily = "claude_cowork"
	OnboardingClientCodex        OnboardingClientFamily = "codex"
	OnboardingClientCursor       OnboardingClientFamily = "cursor"
	OnboardingClientVSCode       OnboardingClientFamily = "vscode"
)

type OnboardingWorkflow struct {
	ID                         uuid.UUID
	SourceSurface              string
	ClientFamily               OnboardingClientFamily
	AgentConfigurationCopiedAt *time.Time
	Status                     string
	ExpiresAt                  time.Time
	SelectedProjectID          uuid.UUID
	SelectedRegistrationID     uuid.UUID
}

type OnboardingConnection struct {
	ID             uuid.UUID
	Generation     uuid.UUID
	AuthorizedAt   *time.Time
	ReauthorizedAt *time.Time
	Ready          bool
}

type OnboardingProjection struct {
	Workflow                  *OnboardingWorkflow
	Connections               []OnboardingConnection
	SelectedProject           *ResolvedProject
	CatalogExplored           bool
	RegistrationSucceeded     bool
	DistributionToolSucceeded bool
	ReadinessVerified         bool
	Stage                     OnboardingStage
}

// OnboardingService owns the user- and organization-bound workflow projection
// consumed by the session-authenticated dashboard. It never stores prompts,
// provider material, OAuth values, or setup handoffs.
type OnboardingService struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewOnboardingService(db *pgxpool.Pool) *OnboardingService {
	return &OnboardingService{db: db, now: time.Now}
}

func (s *OnboardingService) Get(ctx context.Context, organizationID, userID string) (OnboardingProjection, error) {
	if s == nil || s.db == nil || organizationID == "" || userID == "" {
		return OnboardingProjection{}, ErrOnboardingInvalid
	}

	q := platformrepo.New(s.db)
	workflow, err := s.activeWorkflow(ctx, q, organizationID, userID)
	if err != nil {
		return OnboardingProjection{}, err
	}
	connections, err := q.ListPlatformMCPSubjectConnections(ctx, platformrepo.ListPlatformMCPSubjectConnectionsParams{
		OrganizationID: organizationID,
		SubjectUrn:     userSubjectURN(userID),
	})
	if err != nil {
		return OnboardingProjection{}, fmt.Errorf("list platform mcp onboarding connections: %w", err)
	}

	projection := OnboardingProjection{
		Workflow:    workflow,
		Connections: onboardingConnectionsFromRows(connections),
	}
	if workflow != nil && workflow.SelectedProjectID != uuid.Nil && workflow.SelectedRegistrationID != uuid.Nil {
		project, err := q.GetPlatformMCPOnboardingSelectedProject(ctx, platformrepo.GetPlatformMCPOnboardingSelectedProjectParams{
			WorkflowID:           workflow.ID,
			OrganizationID:       organizationID,
			InitiatingSubjectUrn: userSubjectURN(userID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return OnboardingProjection{}, ErrUnavailable
		}
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("get platform mcp onboarding selected project: %w", err)
		}
		projection.SelectedProject = &ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}
	}
	if workflow != nil && len(projection.Connections) > 0 {
		connection := projection.Connections[0]
		projection.CatalogExplored, err = q.HasPlatformMCPOnboardingCatalogExplored(ctx, platformrepo.HasPlatformMCPOnboardingCatalogExploredParams{
			OrganizationID:       organizationID,
			ConnectionID:         uuid.NullUUID{UUID: connection.ID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: connection.Generation, Valid: true},
		})
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("check platform mcp catalog exploration: %w", err)
		}
		projection.RegistrationSucceeded, err = q.HasPlatformMCPOnboardingRegistrationSucceeded(ctx, platformrepo.HasPlatformMCPOnboardingRegistrationSucceededParams{
			OrganizationID:       organizationID,
			InitiatingSubjectUrn: userSubjectURN(userID),
			ConnectionID:         uuid.NullUUID{UUID: connection.ID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: connection.Generation, Valid: true},
		})
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("check platform mcp onboarding registration: %w", err)
		}
		projection.DistributionToolSucceeded, err = q.HasPlatformMCPOnboardingDistributionSucceeded(ctx, platformrepo.HasPlatformMCPOnboardingDistributionSucceededParams{
			OrganizationID:       organizationID,
			InitiatingSubjectUrn: userSubjectURN(userID),
			ConnectionID:         uuid.NullUUID{UUID: connection.ID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: connection.Generation, Valid: true},
		})
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("check platform mcp onboarding distribution: %w", err)
		}
		projection.ReadinessVerified, err = q.HasPlatformMCPOnboardingReadinessVerified(ctx, platformrepo.HasPlatformMCPOnboardingReadinessVerifiedParams{
			OrganizationID:       organizationID,
			InitiatingSubjectUrn: userSubjectURN(userID),
			ConnectionID:         uuid.NullUUID{UUID: connection.ID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: connection.Generation, Valid: true},
		})
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("check platform mcp onboarding readiness verification: %w", err)
		}
	}
	projection.Stage = deriveOnboardingStage(projection.Workflow, projection.Connections)
	return projection, nil
}

func (s *OnboardingService) Start(ctx context.Context, organizationID, userID string) (OnboardingProjection, error) {
	if s == nil || s.db == nil || organizationID == "" || userID == "" {
		return OnboardingProjection{}, ErrOnboardingInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OnboardingProjection{}, fmt.Errorf("begin platform mcp onboarding workflow: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	if err := q.LockPlatformMCPOnboardingWorkflow(ctx, platformrepo.LockPlatformMCPOnboardingWorkflowParams{
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	}); err != nil {
		return OnboardingProjection{}, fmt.Errorf("lock platform mcp onboarding workflow: %w", err)
	}
	workflow, err := s.activeWorkflow(ctx, q, organizationID, userID)
	if err != nil {
		return OnboardingProjection{}, err
	}
	if workflow == nil {
		row, err := q.CreatePlatformMCPOnboardingWorkflow(ctx, platformrepo.CreatePlatformMCPOnboardingWorkflowParams{
			OrganizationID:       organizationID,
			InitiatingSubjectUrn: userSubjectURN(userID),
			SourceSurface:        onboardingSourceDashboard,
			ClientFamily:         string(OnboardingClientClaudeCode),
			ExpiresAt:            timestamp(s.now().UTC().Add(onboardingWorkflowLifetime)),
		})
		if err != nil {
			return OnboardingProjection{}, fmt.Errorf("create platform mcp onboarding workflow: %w", err)
		}
		workflow = onboardingWorkflowFromRow(row)
	}

	if err := tx.Commit(ctx); err != nil {
		return OnboardingProjection{}, fmt.Errorf("commit platform mcp onboarding workflow: %w", err)
	}
	return s.Get(ctx, organizationID, userID)
}

func (s *OnboardingService) RecordInstallIntent(ctx context.Context, organizationID, userID string, client OnboardingClientFamily) (OnboardingProjection, error) {
	if s == nil || s.db == nil || organizationID == "" || userID == "" || !validOnboardingClient(client) {
		return OnboardingProjection{}, ErrOnboardingInvalid
	}

	projection, err := s.Start(ctx, organizationID, userID)
	if err != nil {
		return OnboardingProjection{}, err
	}
	if projection.Workflow == nil {
		return OnboardingProjection{}, ErrUnavailable
	}

	row, err := platformrepo.New(s.db).RecordPlatformMCPOnboardingInstallIntent(ctx, platformrepo.RecordPlatformMCPOnboardingInstallIntentParams{
		SourceSurface:        onboardingSourceDashboard,
		ClientFamily:         string(client),
		ExpiresAt:            timestamp(s.now().UTC().Add(onboardingWorkflowLifetime)),
		ID:                   projection.Workflow.ID,
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OnboardingProjection{}, ErrUnavailable
	}
	if err != nil {
		return OnboardingProjection{}, fmt.Errorf("record platform mcp onboarding install intent: %w", err)
	}

	projection.Workflow = onboardingWorkflowFromRow(row)
	projection.Stage = deriveOnboardingStage(projection.Workflow, projection.Connections)
	return projection, nil
}

// BindRegistration stores the server-owned selected target on the caller's
// active workflow. The registration ID remains internal and is never projected
// to dashboard callers.
func (s *OnboardingService) RecordCatalogExplored(ctx context.Context, principal Principal) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return err
	}
	if err := platformrepo.New(s.db).RecordPlatformMCPCatalogExplored(ctx, platformrepo.RecordPlatformMCPCatalogExploredParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
	}); err != nil {
		return fmt.Errorf("record platform mcp catalog exploration: %w", err)
	}
	return nil
}

func (s *OnboardingService) RecordRegistrationSucceeded(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID) error {
	return s.recordLifecycleMilestone(ctx, principal, projectID, registrationID, "registration_succeeded")
}

func (s *OnboardingService) RecordReadinessVerified(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID) error {
	return s.recordLifecycleMilestone(ctx, principal, projectID, registrationID, "readiness_verified")
}

func (s *OnboardingService) RecordDistributionSucceeded(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID) error {
	return s.recordLifecycleMilestone(ctx, principal, projectID, registrationID, "distribution_succeeded")
}

func (s *OnboardingService) recordLifecycleMilestone(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID, milestone string) error {
	if s == nil || s.db == nil || projectID == uuid.Nil || registrationID == uuid.Nil {
		return ErrOnboardingInvalid
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return err
	}
	if err := platformrepo.New(s.db).RecordPlatformMCPOnboardingLifecycleMilestone(ctx, platformrepo.RecordPlatformMCPOnboardingLifecycleMilestoneParams{
		OrganizationID:       principal.OrganizationID,
		Milestone:            milestone,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		ProjectID:            uuid.NullUUID{UUID: projectID, Valid: true},
		AttemptID:            uuid.NullUUID{UUID: registrationID, Valid: true},
	}); err != nil {
		return fmt.Errorf("record platform mcp %s: %w", milestone, err)
	}
	return nil
}

func (s *OnboardingService) RecordAgentConfigurationCopied(ctx context.Context, organizationID, userID string) (OnboardingProjection, error) {
	if s == nil || s.db == nil || organizationID == "" || userID == "" {
		return OnboardingProjection{}, ErrOnboardingInvalid
	}

	projection, err := s.Start(ctx, organizationID, userID)
	if err != nil {
		return OnboardingProjection{}, err
	}
	if projection.Workflow == nil {
		return OnboardingProjection{}, ErrUnavailable
	}

	row, err := platformrepo.New(s.db).RecordPlatformMCPOnboardingAgentConfigurationCopied(ctx, platformrepo.RecordPlatformMCPOnboardingAgentConfigurationCopiedParams{
		ExpiresAt:            timestamp(s.now().UTC().Add(onboardingWorkflowLifetime)),
		ID:                   projection.Workflow.ID,
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OnboardingProjection{}, ErrUnavailable
	}
	if err != nil {
		return OnboardingProjection{}, fmt.Errorf("record platform mcp agent configuration copy: %w", err)
	}

	projection.Workflow = onboardingWorkflowFromRow(row)
	projection.Stage = deriveOnboardingStage(projection.Workflow, projection.Connections)
	return projection, nil
}

func (s *OnboardingService) BindRegistration(ctx context.Context, organizationID, userID string, projectID, registrationID uuid.UUID) (OnboardingProjection, error) {
	if s == nil || s.db == nil || organizationID == "" || userID == "" || projectID == uuid.Nil || registrationID == uuid.Nil {
		return OnboardingProjection{}, ErrOnboardingInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OnboardingProjection{}, fmt.Errorf("begin platform mcp onboarding registration bind: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	if err := q.LockPlatformMCPOnboardingWorkflow(ctx, platformrepo.LockPlatformMCPOnboardingWorkflowParams{
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	}); err != nil {
		return OnboardingProjection{}, fmt.Errorf("lock platform mcp onboarding registration bind: %w", err)
	}
	workflow, err := s.activeWorkflow(ctx, q, organizationID, userID)
	if err != nil {
		return OnboardingProjection{}, err
	}
	if workflow == nil {
		return OnboardingProjection{}, ErrUnavailable
	}
	if _, err := q.BindPlatformMCPOnboardingRegistration(ctx, platformrepo.BindPlatformMCPOnboardingRegistrationParams{
		SelectedProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		SelectedRegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
		ID:                     workflow.ID,
		OrganizationID:         organizationID,
		InitiatingSubjectUrn:   userSubjectURN(userID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OnboardingProjection{}, ErrUnavailable
		}
		return OnboardingProjection{}, fmt.Errorf("bind platform mcp onboarding registration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OnboardingProjection{}, fmt.Errorf("commit platform mcp onboarding registration bind: %w", err)
	}
	return s.Get(ctx, organizationID, userID)
}

// BindRegistrationForPrincipal associates an MCP-created registration with the
// caller's active workflow only when the project and registration were resolved
// by the same organization-bound principal.
func (s *OnboardingService) BindRegistrationForPrincipal(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID) (OnboardingProjection, error) {
	return s.BindRegistration(ctx, principal.OrganizationID, principal.UserID, projectID, registrationID)
}

func (s *OnboardingService) Dismiss(ctx context.Context, organizationID, userID string) error {
	if s == nil || s.db == nil || organizationID == "" || userID == "" {
		return ErrOnboardingInvalid
	}

	workflow, err := s.activeWorkflow(ctx, platformrepo.New(s.db), organizationID, userID)
	if err != nil {
		return err
	}
	if workflow == nil {
		return nil
	}
	_, err = platformrepo.New(s.db).CloseActivePlatformMCPOnboardingWorkflow(ctx, platformrepo.CloseActivePlatformMCPOnboardingWorkflowParams{
		Status:               "dismissed",
		ID:                   workflow.ID,
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("dismiss platform mcp onboarding workflow: %w", err)
	}
	return nil
}

func (s *OnboardingService) activeWorkflow(ctx context.Context, q *platformrepo.Queries, organizationID, userID string) (*OnboardingWorkflow, error) {
	if _, err := q.ExpireActivePlatformMCPOnboardingWorkflow(ctx, platformrepo.ExpireActivePlatformMCPOnboardingWorkflowParams{
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	}); err != nil {
		return nil, fmt.Errorf("expire platform mcp onboarding workflow: %w", err)
	}
	row, err := q.GetActivePlatformMCPOnboardingWorkflow(ctx, platformrepo.GetActivePlatformMCPOnboardingWorkflowParams{
		OrganizationID:       organizationID,
		InitiatingSubjectUrn: userSubjectURN(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active platform mcp onboarding workflow: %w", err)
	}
	return onboardingWorkflowFromRow(row), nil
}

func deriveOnboardingStage(workflow *OnboardingWorkflow, connections []OnboardingConnection) OnboardingStage {
	for _, connection := range connections {
		if connection.Ready {
			return OnboardingStageConnectionReady
		}
	}
	if len(connections) > 0 {
		return OnboardingStageAuthorized
	}
	if workflow != nil {
		return OnboardingStageInstallInstructions
	}
	return OnboardingStageNotStarted
}

func onboardingWorkflowFromRow(row platformrepo.PlatformMcpOnboardingWorkflow) *OnboardingWorkflow {
	return &OnboardingWorkflow{
		ID:                         row.ID,
		SourceSurface:              row.SourceSurface,
		ClientFamily:               OnboardingClientFamily(row.ClientFamily),
		AgentConfigurationCopiedAt: timePointer(row.AgentConfigurationCopiedAt),
		Status:                     row.Status,
		ExpiresAt:                  timestamptzValue(row.ExpiresAt),
		SelectedProjectID:          uuidValue(row.SelectedProjectID),
		SelectedRegistrationID:     uuidValue(row.SelectedRegistrationID),
	}
}

func onboardingConnectionsFromRows(rows []platformrepo.ListPlatformMCPSubjectConnectionsRow) []OnboardingConnection {
	connections := make([]OnboardingConnection, 0, len(rows))
	for _, row := range rows {
		connections = append(connections, OnboardingConnection{
			ID:             row.ID,
			Generation:     row.ActiveGeneration,
			AuthorizedAt:   timePointer(row.AuthorizedAt),
			ReauthorizedAt: timePointer(row.ReauthorizedAt),
			Ready:          row.Ready,
		})
	}
	return connections
}

func validOnboardingClient(client OnboardingClientFamily) bool {
	switch client {
	case OnboardingClientClaudeCode, OnboardingClientClaudeCowork, OnboardingClientCodex, OnboardingClientCursor, OnboardingClientVSCode:
		return true
	default:
		return false
	}
}

func uuidValue(value uuid.NullUUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return value.UUID
}

func timestamptzValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}
