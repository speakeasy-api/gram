package assistantidentity_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRolloutGatesAreIndependentAndDoNotFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                        string
		gates                       assistantidentity.Rollout
		workloadDenied, slackDenied bool
	}{
		{"normal", assistantidentity.Rollout{}, false, false},
		{"provisioning only", assistantidentity.Rollout{DisableProvisioning: true}, false, false},
		{"execution", assistantidentity.Rollout{DisableExecution: true}, true, true},
		{"delegation", assistantidentity.Rollout{DisableSlackDelegation: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := assistantidentity.New("https://gram.example", false, tc.gates)
			require.NoError(t, err)
			e := assistantidentity.Execution{}
			if tc.workloadDenied {
				require.ErrorIs(t, s.CheckRollout(e), assistantidentity.ErrRolloutDisabled)
			} else {
				require.NoError(t, s.CheckRollout(e))
			}
			e.Slack = &assistantidentity.SlackDelegation{}
			if tc.slackDenied {
				require.ErrorIs(t, s.CheckRollout(e), assistantidentity.ErrRolloutDisabled)
			} else {
				require.NoError(t, s.CheckRollout(e))
			}
		})
	}
}

func TestProvisioningRollbackPreservesBindingHistory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	s, err := assistantidentity.New("https://gram.example", false)
	require.NoError(t, err)
	p := assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor}
	var first assistantidentity.Binding
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		var err error
		first, err = s.Provision(t.Context(), tx, p)
		if err != nil {
			return fmt.Errorf("fixture provisioning: %w", err)
		}
		return nil
	}))
	stopped, err := assistantidentity.New("https://gram.example", false, assistantidentity.Rollout{DisableProvisioning: true})
	require.NoError(t, err)
	require.ErrorIs(t, inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := stopped.Provision(t.Context(), tx, p)
		if err != nil {
			return fmt.Errorf("fixture provisioning: %w", err)
		}
		return nil
	}), assistantidentity.ErrProvisioningDisabled)
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		if err := stopped.BindRootTrigger(t.Context(), tx, f.org, f.project, f.trigger); err != nil {
			return fmt.Errorf("ensure existing root: %w", err)
		}
		return nil
	}))
	newRoot := uuid.New()
	require.NoError(t, repo.New(f.db).FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: newRoot, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "slack", TargetRef: f.assistant.String()}))
	require.ErrorIs(t, inTx(t, f.db, func(tx pgx.Tx) error {
		if err := stopped.BindRootTrigger(t.Context(), tx, f.org, f.project, newRoot); err != nil {
			return fmt.Errorf("provision new root: %w", err)
		}
		return nil
	}), assistantidentity.ErrProvisioningDisabled)
	resolved, err := stopped.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolved.State)
	require.Equal(t, first.AgentID, resolved.Identity.AgentID)
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		binding, err := s.Provision(t.Context(), tx, p)
		require.Equal(t, first, binding)
		if err != nil {
			return fmt.Errorf("fixture provisioning: %w", err)
		}
		return nil
	}))
}
