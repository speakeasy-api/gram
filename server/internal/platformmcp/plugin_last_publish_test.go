package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/publishstatus"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
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

// seedRecordedPublish records a publish for the project the way a successful
// push does, and returns the recorded time as get_plugin reports it.
func seedRecordedPublish(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) string {
	t.Helper()
	recorded, err := pluginsrepo.New(conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: projectID, InstallationID: 1, RepoOwner: "private-owner", RepoName: "private-repository",
		MarketplaceToken: conv.ToPGText("secret-marketplace-token"), PublishedMcpFingerprints: []byte(`{}`),
		PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)
	return recorded.UpdatedAt.Time.UTC().Format(time.RFC3339)
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
	recordedAt := seedRecordedPublish(t, ctx, conn, project.ID)

	requestedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	finishedAt := requestedAt.Add(16 * time.Minute)
	fresh := false
	service := testPluginTargets(t, conn).WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: "release-tools", Fresh: &fresh,
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
			want:   PluginLastPublish{LastRecordedPublishAt: recordedAt, State: publishstatus.StateNone},
		},
		{
			name:   "queued",
			status: publishstatus.Status{State: publishstatus.StateQueued, RequestedAt: &requestedAt},
			want:   PluginLastPublish{LastRecordedPublishAt: recordedAt, State: publishstatus.StateQueued, RequestedAt: "2026-10-01T12:00:00Z"},
		},
		{
			name:   "retrying",
			status: publishstatus.Status{State: publishstatus.StateRetrying, RequestedAt: &requestedAt, Attempt: 2, FailureCategory: publishstatus.FailurePublishFailed},
			want: PluginLastPublish{
				LastRecordedPublishAt: recordedAt, State: publishstatus.StateRetrying, RequestedAt: "2026-10-01T12:00:00Z", Attempt: 2,
				FailureCategory: publishstatus.FailurePublishFailed, FailureMessage: publishFailureMessage(publishstatus.FailurePublishFailed, publishHistoryRecorded),
			},
		},
		{
			name:   "failed on a repository conflict",
			status: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &requestedAt, FinishedAt: &finishedAt, FailureCategory: publishstatus.FailureRepositoryConflict},
			want: PluginLastPublish{
				LastRecordedPublishAt: recordedAt, State: publishstatus.StateFailed, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z",
				FailureCategory: publishstatus.FailureRepositoryConflict, FailureMessage: publishFailureMessage(publishstatus.FailureRepositoryConflict, publishHistoryRecorded),
			},
		},
		{
			name:   "failed after retries were exhausted",
			status: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &requestedAt, FinishedAt: &finishedAt, FailureCategory: publishstatus.FailurePublishFailed},
			want: PluginLastPublish{
				LastRecordedPublishAt: recordedAt, State: publishstatus.StateFailed, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z",
				FailureCategory: publishstatus.FailurePublishFailed, FailureMessage: publishFailureMessage(publishstatus.FailurePublishFailed, publishHistoryRecorded),
			},
		},
		{
			name:   "succeeded",
			status: publishstatus.Status{State: publishstatus.StateSucceeded, RequestedAt: &requestedAt, FinishedAt: &finishedAt},
			want:   PluginLastPublish{LastRecordedPublishAt: recordedAt, State: publishstatus.StateSucceeded, RequestedAt: "2026-10-01T12:00:00Z", FinishedAt: "2026-10-01T12:16:00Z"},
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
	recordedAt := seedRecordedPublish(t, ctx, conn, project.ID)
	service := testPluginTargets(t, conn).WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: "release-tools", Packages: []plugindelivery.PublicationPackageAddress{},
	}}})

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.Equal(t, &PluginLastPublish{LastRecordedPublishAt: recordedAt, Unavailable: true}, got.PublicationEvidence.LastPublish, "no describer configured")

	service.WithPublishStatus(&stubPluginPublishStatus{err: errors.New("describe plugin publish: rpc error: https://private-owner.example/secret-token")})
	got, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err, "an unreadable attempt keeps the admin inventory")
	require.Equal(t, &PluginLastPublish{LastRecordedPublishAt: recordedAt, Unavailable: true}, got.PublicationEvidence.LastPublish)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	for _, secret := range []string{"private-owner", "private-repository", "secret-token", "secret-marketplace-token", "rpc error"} {
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
	service := testPluginTargets(t, conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{err: errors.New("publication resolution failed")}).
		WithPublishStatus(&stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}})

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.True(t, got.PublicationEvidence.Unavailable)
	require.NotNil(t, got.PublicationEvidence.LastPublish, "a failed first publish is still explained")
	require.Equal(t, publishstatus.StateFailed, got.PublicationEvidence.LastPublish.State)
	require.Empty(t, got.PublicationEvidence.LastPublish.LastRecordedPublishAt, "no publish recorded yet")

	// A recorded publish survives package resolution failing.
	recordedAt := seedRecordedPublish(t, ctx, conn, project.ID)
	got, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.True(t, got.PublicationEvidence.Unavailable)
	require.Equal(t, recordedAt, got.PublicationEvidence.LastPublish.LastRecordedPublishAt)
}

func TestGetPluginNeverDescribesPublishForNonAdmins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_non_admin")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	describer := &stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}}
	service := testPluginTargets(t, conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{PluginSlug: "release-tools", Packages: []plugindelivery.PublicationPackageAddress{}}}}).
		WithPublishStatus(describer)

	// The tool routes members to GetAssignedPlugin; this proves GetPlugin
	// gates evidence and the Temporal read on its own if a non-admin ever
	// reached it.
	got, err := service.GetPlugin(withOrganizationGrant(ctx, authz.ScopeOrgRead, principal.OrganizationID), principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.Zero(t, describer.calls, "a non-admin read must not reach Temporal")
	require.Nil(t, got.PublicationEvidence, "publication evidence is admin-only")
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "publication_evidence")
	require.NotContains(t, string(encoded), "last_publish")
}

func TestGetPluginFirstPublishFailureDoesNotOfferRepublish(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_first_publish")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)
	// A first publish that failed before its connection row was saved: the
	// project has no marketplace yet, and republish_plugin would refuse it as
	// not_configured.
	service := testPluginTargets(t, conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
			PluginSlug: "release-tools", NotConfigured: true, Packages: []plugindelivery.PublicationPackageAddress{},
		}}})

	for _, category := range []publishstatus.FailureCategory{publishstatus.FailurePublishFailed, publishstatus.FailureTimedOut, publishstatus.FailureCanceled} {
		service.WithPublishStatus(&stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: category}})
		got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
		require.NoError(t, err, category)
		require.True(t, got.PublicationEvidence.NotConfigured, category)
		lastPublish := got.PublicationEvidence.LastPublish
		require.Equal(t, publishFailureMessage(category, publishHistoryNone), lastPublish.FailureMessage, category)
		require.NotContains(t, lastPublish.FailureMessage, "republish it with", category)
		require.Contains(t, lastPublish.FailureMessage, "AICP dashboard", category)
		require.Contains(t, lastPublish.FailureMessage, "Speakeasy support", category)
	}

	// Once a publish is recorded, the project is configured and
	// republish_plugin is the offered remedy again.
	seedRecordedPublish(t, ctx, conn, project.ID)
	service.WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: "release-tools", NotConfigured: false, Packages: []plugindelivery.PublicationPackageAddress{},
	}}})
	service.WithPublishStatus(&stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}})
	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	require.Equal(t, publishFailureMessage(publishstatus.FailurePublishFailed, publishHistoryRecorded), got.PublicationEvidence.LastPublish.FailureMessage)
	require.Contains(t, got.PublicationEvidence.LastPublish.FailureMessage, "republish_plugin")
}

func TestGetPluginUnreadablePublishHistoryDoesNotOfferRepublish(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_last_publish_unknown_history")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Release Tools", "release-tools")
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)
	seedRecordedPublish(t, ctx, conn, project.ID)
	service := testPluginTargets(t, conn).
		WithPublicationEvidence(stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{PluginSlug: "release-tools", Packages: []plugindelivery.PublicationPackageAddress{}}}}).
		WithPublishStatus(&stubPluginPublishStatus{status: publishstatus.Status{State: publishstatus.StateFailed, FailureCategory: publishstatus.FailurePublishFailed}})
	service.recordedPublish = func(context.Context, uuid.UUID) (pluginsrepo.PluginGithubConnection, error) {
		return pluginsrepo.PluginGithubConnection{}, errors.New("connection reset")
	}

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "release-tools"})
	require.NoError(t, err)
	lastPublish := got.PublicationEvidence.LastPublish
	require.Empty(t, lastPublish.LastRecordedPublishAt, "an unreadable history is not a recorded publish")
	require.Equal(t, publishFailureMessage(publishstatus.FailurePublishFailed, publishHistoryUnknown), lastPublish.FailureMessage)
	require.NotContains(t, lastPublish.FailureMessage, "republish_plugin")
	require.Contains(t, lastPublish.FailureMessage, "AICP dashboard")
	require.Contains(t, lastPublish.FailureMessage, "Speakeasy support")
}
