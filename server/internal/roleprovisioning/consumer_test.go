package roleprovisioning_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	provisioningrepo "github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type recordingReconciler struct {
	roles   []string
	fail    string
	crash   string
	failAll bool
}

func (r *recordingReconciler) Reconcile(_ context.Context, _, role string, actor roleprovisioning.Actor) (roleprovisioning.Result, error) {
	if actor.Principal.IsZero() {
		panic("missing system actor")
	}
	r.roles = append(r.roles, role)
	if role == r.crash {
		panic("simulated process crash")
	}
	if r.failAll || role == r.fail {
		return roleprovisioning.Result{}, errors.New("database unavailable")
	}
	return roleprovisioning.Result{}, nil
}
func maintenanceRequest(org string) *pluginsv1.RoleProvisioningRequested {
	return pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(org)}.Build()
}
func maintenanceMessages(t *testing.T, f *fixture) []*pluginsv1.RoleProvisioningRequested {
	t.Helper()
	rows, err := testrepo.New(f.db).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var result []*pluginsv1.RoleProvisioningRequested
	for _, row := range rows {
		if row.Topic != "gram.plugins.v1.RoleProvisioningRequested" {
			continue
		}
		message := new(pluginsv1.RoleProvisioningRequested)
		require.NoError(t, proto.Unmarshal(row.Message, message))
		result = append(result, message)
	}

	return result
}
func TestMaintenanceRolePageAndReplay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 101 {
		f.addRole(f.org, "Role")
	}
	r := &recordingReconciler{}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := maintenanceRequest(f.org)
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Len(t, r.roles, 100)
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 1)
	require.NotEmpty(t, messages[0].GetAfterRoleUrn())
	require.NoError(t, h.HandlePage(t.Context(), messages[0]))
	require.Len(t, r.roles, 102)
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Len(t, r.roles, 202)
}
func TestMaintenanceIncludesStaleSettingsAndManualAudiences(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	staleSetting := "role:organization:" + uuid.NewString()
	staleAudience := "role:global:" + uuid.NewString()
	require.NoError(t, provisioningrepo.New(f.db).EnsureRoleSetting(t.Context(), provisioningrepo.EnsureRoleSettingParams{
		OrganizationID: conv.ToPGText(f.org), RoleUrn: staleSetting, Enabled: false, ProjectID: uuid.NullUUID{},
	}))
	plugin, err := pluginrepo.New(f.db).CreatePlugin(t.Context(), pluginrepo.CreatePluginParams{
		OrganizationID: f.org, ProjectID: f.project, Name: "Manual", Slug: "manual", Description: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = pluginrepo.New(f.db).AddPluginAssignment(t.Context(), pluginrepo.AddPluginAssignmentParams{
		PluginID: plugin.ID, OrganizationID: f.org, PrincipalUrn: staleAudience,
	})
	require.NoError(t, err)
	r := &recordingReconciler{fail: staleSetting}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	require.NoError(t, h.HandlePage(t.Context(), maintenanceRequest(f.org)))
	require.Contains(t, r.roles, staleSetting)
	require.Contains(t, r.roles, staleAudience)
	require.Contains(t, r.roles, f.role)
	retries := maintenanceMessages(t, f)
	require.Len(t, retries, 1)
	require.Equal(t, staleSetting, retries[0].GetRoleUrn())
	require.ErrorContains(t, h.HandlePage(t.Context(), retries[0]), "database unavailable")
	request := pluginsv1.RoleProvisioningRequested_builder{GlobalRoleUrn: new(staleAudience)}.Build()
	require.NoError(t, h.HandlePage(t.Context(), request))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 2)
	require.Equal(t, f.org, messages[1].GetOrganizationId())
	require.Equal(t, staleAudience, messages[1].GetRoleUrn())
}
func TestMaintenanceGlobalFanoutIsBounded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 101 {
		org := fmt.Sprintf("org_maintenance_%03d", i)
		_, err := orgrepo.New(f.db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
			ID: org, Name: org, Slug: org, WorkosID: conv.ToPGTextEmpty(""), Whitelisted: pgtype.Bool{Bool: false, Valid: true}, CreationSource: conv.ToPGTextEmpty(""),
		})
		require.NoError(t, err)
		_, err = provisioningrepo.New(f.db).SaveSettings(t.Context(), provisioningrepo.SaveSettingsParams{
			OrganizationID: org, Enabled: pgtype.Bool{Bool: true, Valid: true}, ProjectID: uuid.NullUUID{}, Version: pgtype.Int8{Int64: 0, Valid: true},
		})
		require.NoError(t, err)
	}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, &recordingReconciler{})
	request := pluginsv1.RoleProvisioningRequested_builder{GlobalSweep: new(true)}.Build()
	require.NoError(t, h.HandlePage(t.Context(), request))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 101)
	continuation := messages[100]
	require.True(t, continuation.GetGlobalSweep())
	require.NotEmpty(t, continuation.GetAfterOrganizationId())
	require.NoError(t, h.HandlePage(t.Context(), continuation))
	require.Len(t, maintenanceMessages(t, f), 102)
}

// HandlePage errors are staged as nacks by HandleBatchWithResult; nil permits ack.
func TestMaintenanceFailureAdvancesPageAndRetriesIndependently(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// Two full pages plus a terminal role, with every role failing persistently. Retrying
	// any failed role must not repeat a traversal page or enqueue more messages.
	for range 200 {
		f.addRole(f.org, "Role")
	}
	r := &recordingReconciler{failAll: true}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := maintenanceRequest(f.org)
	var retries []*pluginsv1.RoleProvisioningRequested
	offset := 0
	for page := range 3 {
		require.NoError(t, h.HandlePage(t.Context(), request))
		messages := maintenanceMessages(t, f)
		outgoing := messages[offset:]
		offset = len(messages)
		if page < 2 {
			require.Len(t, outgoing, 101)
			request = outgoing[100]
			require.NotEmpty(t, request.GetAfterRoleUrn())
			require.Empty(t, request.GetRoleUrn())
			outgoing = outgoing[:100]
		} else {
			require.Len(t, outgoing, 1)
		}
		for _, retry := range outgoing {
			require.NotEmpty(t, retry.GetRoleUrn())
			require.Empty(t, retry.GetAfterRoleUrn())
		}
		// One representative from each full page and the terminal page.
		retries = append(retries, outgoing[0])
	}
	require.Len(t, r.roles, 201)
	require.Len(t, retries, 3)
	require.Len(t, maintenanceMessages(t, f), 203)
	// Model repeated transport delivery up to DLQ: each sampled retry remains the
	// same role-scoped message, rather than resetting its delivery attempts.
	for range 3 {
		for _, retry := range retries {
			require.NotEmpty(t, retry.GetRoleUrn())
			require.Empty(t, retry.GetAfterRoleUrn())
			require.ErrorContains(t, h.HandlePage(t.Context(), retry), "database unavailable")
		}
		require.Len(t, maintenanceMessages(t, f), 203)
	}
	require.Len(t, r.roles, 210)
	r.failAll = false
	for _, retry := range retries {
		require.NoError(t, h.HandlePage(t.Context(), retry))
	}
	require.Len(t, maintenanceMessages(t, f), 203)
}

func TestMaintenanceCrashAfterCommitReplaysBoundedRetries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 100 {
		f.addRole(f.org, "Role")
	}
	r := &recordingReconciler{failAll: true}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := maintenanceRequest(f.org)
	require.NoError(t, h.HandlePage(t.Context(), request))
	// Simulate losing the ack after committing both retries and continuation.
	require.NoError(t, h.HandlePage(t.Context(), request))
	messages := maintenanceMessages(t, f)
	require.Len(t, messages, 202)
	for i := range 101 {
		require.True(t, proto.Equal(messages[i], messages[i+101]))
	}
	for _, message := range messages {
		if message.GetRoleUrn() != "" {
			require.ErrorContains(t, h.HandlePage(t.Context(), message), "database unavailable")
		} else {
			require.NoError(t, h.HandlePage(t.Context(), message))
		}
	}
	// Only the two duplicate last-page retries were added; failed deliveries
	// did not create any new retry or traversal messages.
	require.Len(t, maintenanceMessages(t, f), 204)
	require.Len(t, r.roles, 402)
}

func TestMaintenanceCrashBeforeContinuationReplaysPage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 100 {
		f.addRole(f.org, "Role")
	}
	r := &recordingReconciler{}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := maintenanceRequest(f.org)
	require.NoError(t, h.HandlePage(t.Context(), request))
	middle := r.roles[50]
	require.Len(t, maintenanceMessages(t, f), 1)
	r.roles, r.crash = nil, middle
	require.Panics(t, func() { _ = h.HandlePage(t.Context(), request) })
	require.Len(t, r.roles, 51)
	require.Len(t, maintenanceMessages(t, f), 1)
	r.crash = ""
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Len(t, r.roles, 151)
	require.Len(t, maintenanceMessages(t, f), 2)
}

func TestMaintenanceContinuationCommitFailureNacksAndReplays(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 100 {
		f.addRole(f.org, "Role")
	}
	// Deferred trigger fails COMMIT, not the insert: no continuation may escape
	// the failed transaction, and a completed page must not be acknowledged.
	f.exec(`CREATE FUNCTION reject_maintenance_continuation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'continuation commit unavailable'; END $$`)
	f.exec(`CREATE CONSTRAINT TRIGGER reject_maintenance_continuation AFTER INSERT ON publish_outbox DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_maintenance_continuation()`)
	r := &recordingReconciler{failAll: true}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := maintenanceRequest(f.org)
	require.ErrorContains(t, h.HandlePage(t.Context(), request), "continuation commit unavailable")
	require.Len(t, r.roles, 100)
	require.Empty(t, maintenanceMessages(t, f))
	f.exec(`DROP TRIGGER reject_maintenance_continuation ON publish_outbox`)
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Len(t, r.roles, 200)
	require.Len(t, maintenanceMessages(t, f), 101)
}

func TestMaintenancePluginHintFindsAssociationAfterDeletion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	applied := f.reconcile(f.role)
	_, err := pluginrepo.New(f.db).RemoveAllPluginAssignments(t.Context(), pluginrepo.RemoveAllPluginAssignmentsParams{
		PluginID: applied.PluginID, OrganizationID: f.org, ProjectID: f.project,
	})
	require.NoError(t, err)
	require.NoError(t, pluginrepo.New(f.db).DeletePlugin(t.Context(), pluginrepo.DeletePluginParams{
		ID: applied.PluginID, OrganizationID: f.org, ProjectID: f.project,
	}))
	r := &recordingReconciler{}
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, r)
	request := pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(f.org), PluginId: new(applied.PluginID.String())}.Build()
	require.NoError(t, h.HandlePage(t.Context(), request))
	require.Equal(t, []string{f.role}, r.roles)
}

func TestMaintenanceRejectsInvalidScopes(t *testing.T) {
	t.Parallel()
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), nil, nil)
	for _, request := range []*pluginsv1.RoleProvisioningRequested{
		nil,
		{},
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), GlobalSweep: new(true)}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalSweep: new(true), GlobalRoleUrn: new("role:global:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalSweep: new(true), RoleUrn: new("role:organization:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalSweep: new(true), PluginId: new(uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalSweep: new(true), AfterRoleUrn: new("role:global:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), AfterOrganizationId: new("org_cursor")}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), RoleUrn: new("role:organization:" + uuid.NewString()), AfterRoleUrn: new("role:organization:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalRoleUrn: new("role:organization:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalRoleUrn: new("role:global:" + uuid.Nil.String())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), RoleUrn: new("role:organization:" + uuid.Nil.String())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), PluginId: new(uuid.Nil.String())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), PluginId: new("malformed-sensitive-value")}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), RoleUrn: new("malformed-sensitive-value")}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new("org_fixture"), AfterRoleUrn: new("malformed-sensitive-value")}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{GlobalRoleUrn: new("malformed-sensitive-value")}.Build(),
	} {
		err := h.HandlePage(t.Context(), request)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "malformed-sensitive-value")
		require.Less(t, len(err.Error()), 80)
	}
}

func TestMaintenanceValidMissingResourcesAreNoops(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	h := roleprovisioning.NewConsumer(testenv.NewLogger(t), f.db, f.service)
	for _, request := range []*pluginsv1.RoleProvisioningRequested{
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(f.org), PluginId: new(uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(f.org), RoleUrn: new("role:organization:" + uuid.NewString())}.Build(),
		pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(f.org), RoleUrn: new("role:global:" + uuid.NewString())}.Build(),
	} {
		require.NoError(t, h.HandlePage(t.Context(), request))
	}
	require.Empty(t, maintenanceMessages(t, f))
}
