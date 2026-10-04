package assistants

import (
	"context"
	"errors"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/stretchr/testify/require"
)

type failingIdentityFeatureProvider struct {
	feature.InMemory
	err error
}

func (p *failingIdentityFeatureProvider) EvaluateFlag(context.Context, feature.Flag, string, map[string]string) (feature.Evaluation, error) {
	return feature.EvaluationIndeterminate, p.err
}

func TestAssistantCreationFeatureProviderErrorRollsBack(t *testing.T) {
	t.Parallel()
	for _, managed := range []bool{false, true} {
		name := "new"
		if managed {
			name = "managed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "identity_flag_error_"+name)
			require.NoError(t, err)
			project := newProvisioningProject(t, db, "flag-error")
			core := newProvisioningCore(t, db)
			providerErr := errors.New("feature provider unavailable")
			core.SetFeatureProvider(&failingIdentityFeatureProvider{err: providerErr})
			create := func() (assistantRecord, error) {
				if managed {
					return core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1")
				}
				return core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Flag test", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
			}
			_, err = create()
			require.ErrorIs(t, err, providerErr)
			rows, err := core.ListAssistants(t.Context(), project)
			require.NoError(t, err)
			require.Empty(t, rows)
			agents, err := agentrepo.New(db).ListManagedAgents(t.Context(), "org-test")
			require.NoError(t, err)
			require.Empty(t, agents, "failed evaluation leaves no identity agent")
			flags := new(feature.InMemory)
			flags.SetFlag(feature.FlagAgentIdentityCredentials, "org-test", true)
			core.SetFeatureProvider(flags)
			created, err := create()
			require.NoError(t, err)
			require.Equal(t, "ACTIVE", created.IdentityState)
			rows, err = core.ListAssistants(t.Context(), project)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			_, err = identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{OrganizationID: "org-test", ProjectID: project, AssistantID: created.ID, CaptureSuspended: false})
			require.NoError(t, err)

		})
	}
}

func TestGetAssistantIdentityDiagnosticsFeatureGate(t *testing.T) {
	t.Parallel()
	svc, ctx, project, _ := newRBACServiceWithConn(t, "identity_diagnostics_api_gate")
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, project.String()),
	})
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagAgentIdentityCredentials, "org-test", false)
	svc.core.SetFeatureProvider(flags)
	assistant, err := svc.core.CreateAssistant(ctx, "org-test", project, "user-test", "Legacy", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	for _, enabled := range []bool{false, true, false} {
		flags.SetFlag(feature.FlagAgentIdentityCredentials, "org-test", enabled)
		view, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: assistant.ID.String()})
		require.NoError(t, err)
		if enabled {
			require.NotNil(t, view.IdentityDiagnostics)
			require.Equal(t, "legacy", view.IdentityDiagnostics.Health)
		} else {
			require.Nil(t, view.IdentityDiagnostics)
		}
	}
	svc.core.SetFeatureProvider(nil)
	view, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: assistant.ID.String()})
	require.NoError(t, err)
	require.Nil(t, view.IdentityDiagnostics)
	svc.core.SetFeatureProvider(&failingIdentityFeatureProvider{err: errors.New("feature provider unavailable")})
	view, err = svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: assistant.ID.String()})
	require.NoError(t, err)
	require.Nil(t, view.IdentityDiagnostics)
}
