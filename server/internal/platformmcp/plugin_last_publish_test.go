package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/publishstatus"
)

type stubPluginPublishStatus struct {
	status    publishstatus.Status
	err       error
	projectID uuid.UUID
	calls     int
}

func (s *stubPluginPublishStatus) Describe(_ context.Context, projectID uuid.UUID) (publishstatus.Status, error) {
	s.projectID = projectID
	s.calls++
	return s.status, s.err
}

func withOrganizationGrant(ctx context.Context, scope authz.Scope, organizationID string) context.Context {
	return authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(scope, organizationID)})
}

func TestGetPluginReportsLastPublishAttempt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)

	pushedAt := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	requestedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	finishedAt := requestedAt.Add(16 * time.Minute)
	fresh := false
	service := testPluginTargets(conn).WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: "release-tools", Fresh: &fresh, LastSuccessfulPushAt: &pushedAt,
		Packages: []plugindelivery.PublicationPackageAddress{{ServerName: "MCP", MCPURL: "https://private.example/mcp/first"}},
	}}})

	cases := []struct {
		name   string
		status publishstatus.Status
		want   PluginLastPublish
	}{
		{
			name:   "no attempt on record",
			status: publishstatus.Status{State: publishstatus.StateNone},
			want:   PluginLastPublish{LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateNone},
		},
		{
			name:   "queued",
			status: publishstatus.Status{State: publishstatus.StateQueued, RequestedAt: &requestedAt},
			want:   PluginLastPublish{LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateQueued, RequestedAt: "2026-10-01T12:00:00Z"},
		},
		{
			name:   "retrying",
			status: publishstatus.Status{State: publishstatus.StateRetrying, RequestedAt: &requestedAt, Attempt: 2, FailureCategory: publishstatus.FailurePublishFailed},
			want: PluginLastPublish{
				LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateRetrying, RequestedAt: "2026-10-01T12:00:00Z", Attempt: 2,
				FailureCategory: publishstatus.FailurePublishFailed, FailureMessage: publishFailureMessage(publishstatus.FailurePublishFailed),
			},
		},
		{
			name:   "failed on a repository conflict",
			status: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &requestedAt, FinishedAt: &finishedAt, FailureCategory: publishstatus.FailureRepositoryConflict},
			want: PluginLastPublish{
				LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateFailed, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z",
				FailureCategory: publishstatus.FailureRepositoryConflict, FailureMessage: publishFailureMessage(publishstatus.FailureRepositoryConflict),
			},
		},
		{
			name:   "failed after retries were exhausted",
			status: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &requestedAt, FinishedAt: &finishedAt, FailureCategory: publishstatus.FailurePublishFailed},
			want: PluginLastPublish{
				LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateFailed, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z",
				FailureCategory: publishstatus.FailurePublishFailed, FailureMessage: publishFailureMessage(publishstatus.FailurePublishFailed),
			},
		},
		{
			name:   "succeeded",
			status: publishstatus.Status{State: publishstatus.StateSucceeded, RequestedAt: &requestedAt, FinishedAt: &finishedAt},
			want:   PluginLastPublish{LastSuccessfulPushAt: "2026-09-30T08:00:00Z", State: publishstatus.StateSucceeded, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z"},
		},
	}
	// Sequential: every case swaps the describer on one shared service.
	for _, tc := range cases {
		describer := &stubPluginPublishStatus{status: tc.status}
		service.WithPublishStatus(describer)
		got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
		require.NoError(t, err, tc.name)
		require.Equal(t, project.ID, describer.projectID, tc.name)
		require.NotNil(t, got.PublicationEvidence, tc.name)
		require.NotNil(t, got.PublicationEvidence.LastPublish, tc.name)
		require.Equal(t, tc.want, *got.PublicationEvidence.LastPublish, tc.name)
	}
}

func TestGetPluginReportsLastPublishUnavailable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_unavailable")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)
	pushedAt := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	service := testPluginTargets(conn).WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: "release-tools", LastSuccessfulPushAt: &pushedAt, Packages: []plugindelivery.PublicationPackageAddress{},
	}}})

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.Equal(t, &PluginLastPublish{LastSuccessfulPushAt: "2026-09-30T08:00:00Z", Unavailable: true}, got.PublicationEvidence.LastPublish, "no describer configured")

	service.WithPublishStatus(&stubPluginPublishStatus{err: errors.New("describe plugin publish: rpc error: https://private-owner.example/secret-token")})
	got, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err, "an unreadable attempt keeps the admin inventory")
	require.Equal(t, &PluginLastPublish{LastSuccessfulPushAt: "2026-09-30T08:00:00Z", Unavailable: true}, got.PublicationEvidence.LastPublish)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	for _, secret := range []string{"private-owner", "secret-token", "rpc error"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestGetPluginReportsLastPublishWhenEvidenceIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_no_evidence")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)
	service := testPluginTargets(conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{err: errors.New("publication resolution failed")}).
		WithPublishStatus(&stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}})

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.True(t, got.PublicationEvidence.Unavailable)
	require.NotNil(t, got.PublicationEvidence.LastPublish, "a failed first publish is still explained")
	require.Equal(t, publishstatus.StateFailed, got.PublicationEvidence.LastPublish.State)
	require.Empty(t, got.PublicationEvidence.LastPublish.LastSuccessfulPushAt)
}

func TestGetPluginNeverDescribesPublishForNonAdmins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_non_admin")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	describer := &stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}}
	service := testPluginTargets(conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{PluginSlug: "release-tools", Packages: []plugindelivery.PublicationPackageAddress{}}}}).
		WithPublishStatus(describer)

	// GetPlugin is the admin read; this proves the Temporal read is gated on
	// its own even if a non-admin caller ever reached it.
	got, err := service.GetPlugin(withOrganizationGrant(ctx, authz.ScopeOrgRead, principal.OrganizationID), principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.Zero(t, describer.calls, "a non-admin read must not reach Temporal")
	require.Equal(t, &PluginLastPublish{Unavailable: true}, got.PublicationEvidence.LastPublish)
}
