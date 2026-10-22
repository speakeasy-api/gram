package admission

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type gatewayMembershipFeatures struct {
	feature.InMemory
	evaluate func(feature.Flag, string, map[string]string) (feature.Evaluation, error)
}

func (p *gatewayMembershipFeatures) EvaluateFlag(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	return p.evaluate(flag, distinctID, groups)
}

func TestAutomaticGatewayPluginMembershipMissingProvider(t *testing.T) {
	t.Parallel()
	var missing *Guard
	require.False(t, missing.AutomaticGatewayPluginMembershipEnabled(t.Context(), nil, "organization", uuid.New()))
	require.Nil(t, missing.WithGatewayPluginMembershipFeatures(nil))
	require.False(t, NewGuard().AutomaticGatewayPluginMembershipEnabled(t.Context(), nil, "organization", uuid.New()))
}

func TestAutomaticGatewayPluginMembershipRollout(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	project, err := projectsrepo.New(fixture.conn).GetProjectByID(t.Context(), fixture.projectID)
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		state feature.Evaluation
		err   error
		want  bool
	}{
		{name: "enabled", state: feature.EvaluationEnabled, want: true},
		{name: "disabled", state: feature.EvaluationDisabled},
		{name: "indeterminate", state: feature.EvaluationIndeterminate},
		{name: "error even with enabled state", state: feature.EvaluationEnabled, err: errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			called := false
			provider := &gatewayMembershipFeatures{evaluate: func(flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
				called = true
				require.Equal(t, feature.FlagGatewayPluginMembership, flag)
				require.Equal(t, fixture.orgID, distinctID)
				require.Equal(t, feature.OrgProjectGroups(fixture.orgID, project.Slug), groups)
				return tc.state, tc.err
			}}
			guard := NewGuard().WithGatewayPluginMembershipFeatures(provider)
			tx := testenv.BeginTx(t, t.Context(), fixture.conn)
			require.Equal(t, tc.want, guard.AutomaticGatewayPluginMembershipEnabled(t.Context(), tx, fixture.orgID, fixture.projectID))
			require.True(t, called)
		})
	}
}

func TestAutomaticGatewayPluginMembershipRequiresLiveScope(t *testing.T) {
	t.Parallel()
	fixture := newAdmissionFixture(t)
	provider := &gatewayMembershipFeatures{evaluate: func(feature.Flag, string, map[string]string) (feature.Evaluation, error) {
		t.Fatal("must not evaluate a flag outside the live organization/project scope")
		return feature.EvaluationEnabled, nil
	}}
	guard := NewGuard().WithGatewayPluginMembershipFeatures(provider)
	for _, name := range []string{"wrong organization", "missing project", "deleted project", "disabled organization"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tx := testenv.BeginTx(t, t.Context(), fixture.conn)
			org, projectID := fixture.orgID, fixture.projectID
			switch name {
			case "wrong organization":
				org = "another-organization"
			case "missing project":
				projectID = uuid.New()
			case "deleted project":
				//nolint:glint // notestingrawsql: Simulate a deleted scope inside a rollback-only transaction.
				_, err := tx.Exec(t.Context(), "UPDATE projects SET deleted_at = clock_timestamp() WHERE id = $1 AND organization_id = $2", projectID, org)
				require.NoError(t, err)
			case "disabled organization":
				//nolint:glint // notestingrawsql: Simulate a disabled scope inside a rollback-only transaction.
				_, err := tx.Exec(t.Context(), "UPDATE organization_metadata SET disabled_at = clock_timestamp() WHERE id = $1", org)
				require.NoError(t, err)
			}
			require.False(t, guard.AutomaticGatewayPluginMembershipEnabled(t.Context(), tx, org, projectID))
		})
	}
}
