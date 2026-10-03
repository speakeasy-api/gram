package assistants

import (
	"context"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentityDiagnosticsAndProvisioningRollback(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_diagnostics")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-diagnostics")
	core := newProvisioningCore(t, db)
	normal := core.identities
	stopped, err := assistantidentity.New(normal.Issuer(), false, assistantidentity.Rollout{DisableProvisioning: true})
	require.NoError(t, err)
	core.identities = stopped
	legacy, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Legacy", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	require.Equal(t, "NEVER_CONFIGURED", legacy.IdentityState)
	view, err := core.identityDiagnostics(t.Context(), legacy)
	require.NoError(t, err)
	require.Equal(t, "legacy", view.Health)
	require.False(t, view.ProvisioningEnabled)
	require.True(t, view.ExecutionEnabled)
	_, err = core.UpgradeAssistantIdentity(t.Context(), "org-test", project, legacy.ID, "user-1")
	require.ErrorIs(t, err, assistantidentity.ErrProvisioningDisabled)
	core.identities = normal
	bound, err := core.UpgradeAssistantIdentity(t.Context(), "org-test", project, legacy.ID, "user-1")
	require.NoError(t, err)
	view, err = core.identityDiagnostics(t.Context(), bound)
	require.NoError(t, err)
	require.Equal(t, "ready", view.Health)
	require.NotEmpty(t, view.Bindings)
	require.Equal(t, "ready", view.Bindings[0].State)
	root := uuid.New()
	require.NoError(t, identityrepo.New(db).FixtureCreateRoot(t.Context(), identityrepo.FixtureCreateRootParams{ID: root, OrganizationID: "org-test", ProjectID: project, DefinitionSlug: "slack", TargetRef: bound.ID.String()}))
	missing, err := core.identityDiagnostics(t.Context(), bound)
	require.NoError(t, err)
	states := map[string]string{}
	for _, b := range missing.Bindings {
		states[b.TriggerID] = b.State
	}
	require.Equal(t, "missing", states[root.String()])
	again, err := core.UpgradeAssistantIdentity(t.Context(), "org-test", project, legacy.ID, "user-1")
	require.NoError(t, err)
	require.Equal(t, bound.AgentID, again.AgentID)
	repaired, err := core.identityDiagnostics(t.Context(), again)
	require.NoError(t, err)
	for _, b := range repaired.Bindings {
		require.Equal(t, "ready", b.State)
	}
	require.NoError(t, identityrepo.New(db).FixtureSuspendAgent(t.Context(), identityrepo.FixtureSuspendAgentParams{OrganizationID: "org-test", AgentID: uuid.MustParse(*again.AgentID)}))
	suspended, err := core.identityDiagnostics(t.Context(), again)
	require.NoError(t, err)
	require.Equal(t, "suspended", suspended.Health)

}

func TestIdentityAdmissionMetricsKeepDenialAndRetryDistinct(t *testing.T) {
	t.Parallel()
	require.Equal(t, "issued", identityAdmissionResult(nil))
	require.Equal(t, "rollout_disabled", identityAdmissionResult(assistantidentity.ErrRolloutDisabled))
	require.ErrorIs(t, classifyExecutionDispatchError(assistantidentity.ErrRolloutDisabled), assistantidentity.ErrRolloutDisabled)
	require.NotErrorIs(t, classifyExecutionDispatchError(assistantidentity.ErrRolloutDisabled), errExecutionDenied)
	require.Equal(t, "denied", identityAdmissionResult(assistantidentity.ErrInvalidIdentity))
	require.Equal(t, "retryable_error", identityAdmissionResult(context.DeadlineExceeded))
}
