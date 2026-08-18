package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestOnboardingServicePersistsWorkflowAndUsesSubjectQualifiedEvidence(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_onboarding")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	registration, err := platformrepo.New(conn).CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            project.ID,
		SourceKind:           "catalog",
		CatalogProvider:      "fixture",
		CatalogReference:     "onboarding-test",
		Status:               registrationStatusPending,
		ConnectionID:         uuid.NullUUID{UUID: connectionIDFromPrincipal(t, principal), Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: connectionIDFromPrincipalGeneration(t, principal), Valid: true},
	})
	require.NoError(t, err)
	service := NewOnboardingService(conn)

	initial, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.Nil(t, initial.Workflow)
	require.Len(t, initial.Connections, 1)
	require.Equal(t, OnboardingStageAuthorized, initial.Stage)

	started, err := service.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.NotNil(t, started.Workflow)
	require.Equal(t, onboardingSourceDashboard, started.Workflow.SourceSurface)
	require.Equal(t, OnboardingClientClaudeCode, started.Workflow.ClientFamily)
	require.Equal(t, OnboardingStageAuthorized, started.Stage)

	resumed, err := service.Start(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.NotNil(t, resumed.Workflow)
	require.Equal(t, started.Workflow.ID, resumed.Workflow.ID)

	bound, err := service.BindRegistration(ctx, principal.OrganizationID, principal.UserID, project.ID, registration.ID)
	require.NoError(t, err)
	require.NotNil(t, bound.Workflow)
	require.Equal(t, project.ID, bound.Workflow.SelectedProjectID)
	require.Equal(t, registration.ID, bound.Workflow.SelectedRegistrationID)
	require.NotNil(t, bound.SelectedProject)
	require.Equal(t, project.Name, bound.SelectedProject.Name)
	require.Equal(t, project.Slug, bound.SelectedProject.Slug)

	intent, err := service.RecordInstallIntent(ctx, principal.OrganizationID, principal.UserID, OnboardingClientCursor)
	require.NoError(t, err)
	require.NotNil(t, intent.Workflow)
	require.Equal(t, started.Workflow.ID, intent.Workflow.ID)
	require.Equal(t, OnboardingClientCursor, intent.Workflow.ClientFamily)

	configurationCopied, err := service.RecordAgentConfigurationCopied(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.NotNil(t, configurationCopied.Workflow)
	require.NotNil(t, configurationCopied.Workflow.AgentConfigurationCopiedAt)

	require.NoError(t, service.RecordCatalogExplored(ctx, principal))
	require.NoError(t, service.RecordCatalogExplored(ctx, principal), "replaying catalogue search for the same live connection generation is idempotent")
	explored, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.True(t, explored.CatalogExplored)

	require.NoError(t, service.RecordRegistrationSucceeded(ctx, principal, project.ID, registration.ID))
	registered, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.True(t, registered.RegistrationSucceeded)

	require.NoError(t, service.RecordReadinessVerified(ctx, principal, project.ID, registration.ID))
	readinessVerified, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.True(t, readinessVerified.ReadinessVerified)

	connectionID := connectionIDFromPrincipal(t, principal)
	newGeneration := uuid.New()
	now := time.Now().UTC()
	_, err = platformrepo.New(conn).RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{
		ActiveGeneration:       newGeneration,
		ReauthorizedAt:         timestamp(now),
		AuthorizationExpiresAt: timestamp(now.Add(90 * 24 * time.Hour)),
		ConnectionID:           connectionID,
		OrganizationID:         principal.OrganizationID,
	})
	require.NoError(t, err)
	currentConnection, err := platformrepo.New(conn).GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: connectionID, OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         connectionID,
		OauthClientID:        currentConnection.OauthClientID,
		ConnectionGeneration: newGeneration,
		Jti:                  "jti-" + uuid.NewString(),
		RefreshTokenHash:     "refresh-" + uuid.NewString(),
		ExpiresAt:            timestamp(now.Add(time.Hour)),
		RefreshExpiresAt:     timestamp(now.Add(30 * 24 * time.Hour)),
	})
	require.NoError(t, err)
	principal.Generation = newGeneration.String()
	require.NoError(t, service.RecordRegistrationSucceeded(ctx, principal, project.ID, registration.ID), "reauthorization records fresh lifecycle evidence instead of conflicting with the previous generation")
	reauthorized, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.True(t, reauthorized.RegistrationSucceeded)

	otherUser := "user_" + uuid.NewString()
	other, err := service.Get(ctx, principal.OrganizationID, otherUser)
	require.NoError(t, err)
	require.Nil(t, other.Workflow)
	require.Empty(t, other.Connections)
	require.Equal(t, OnboardingStageNotStarted, other.Stage)

	_, err = service.BindRegistration(ctx, principal.OrganizationID, otherUser, project.ID, registration.ID)
	require.ErrorIs(t, err, ErrUnavailable, "a different user cannot bind the active workflow")

	generation := connectionIDFromPrincipalGeneration(t, principal)
	err = platformrepo.New(conn).RecordPlatformMCPConnectionReady(ctx, platformrepo.RecordPlatformMCPConnectionReadyParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionIDFromPrincipal(t, principal), Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
	})
	require.NoError(t, err)

	ready, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.Equal(t, OnboardingStageConnectionReady, ready.Stage)
	require.Len(t, ready.Connections, 1)
	require.True(t, ready.Connections[0].Ready)

	require.NoError(t, service.Dismiss(ctx, principal.OrganizationID, principal.UserID))
	dismissed, err := service.Get(ctx, principal.OrganizationID, principal.UserID)
	require.NoError(t, err)
	require.Nil(t, dismissed.Workflow)
	require.Equal(t, OnboardingStageConnectionReady, dismissed.Stage)
}

func TestOnboardingServiceValidatesClientFamily(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_onboarding_validation")
	require.NoError(t, err)

	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	service := NewOnboardingService(conn)

	_, err = service.RecordInstallIntent(ctx, principal.OrganizationID, principal.UserID, OnboardingClientFamily("unknown"))
	require.ErrorIs(t, err, ErrOnboardingInvalid)

	_, err = service.RecordInstallIntent(ctx, principal.OrganizationID, principal.UserID, OnboardingClientOpencode)
	require.NoError(t, err)

	workflow, err := platformrepo.New(conn).GetActivePlatformMCPOnboardingWorkflow(ctx, platformrepo.GetActivePlatformMCPOnboardingWorkflowParams{
		OrganizationID:       principal.OrganizationID,
		InitiatingSubjectUrn: userSubjectURN(principal.UserID),
	})
	require.NoError(t, err)
	require.Equal(t, string(OnboardingClientOpencode), workflow.ClientFamily)
}
