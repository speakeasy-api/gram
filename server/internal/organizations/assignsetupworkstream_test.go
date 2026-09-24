package organizations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_AssignSetupWorkstreamMembershipAndNotification(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	sent := make(chan struct{}, 2)
	ti.loops.On("SendTransactional", mock.Anything, mock.MatchedBy(func(input loops.SendTransactionalInput) bool {
		return input.DataVariables["task_title"] == "Observe agents" && strings.HasSuffix(input.DataVariables["setup_link"], "/setup")
	})).Run(func(mock.Arguments) { sent <- struct{}{} }).Return(nil).Once()
	payload := &gen.AssignSetupWorkstreamPayload{Workstream: "observe", Assignee: &gen.SetupTaskAssigneeInput{UserID: &ac.UserID}}
	result, err := ti.service.AssignSetupWorkstream(ctx, payload)
	require.NoError(t, err)
	for _, task := range result.Tasks {
		require.False(t, task.Hidden)
	}
	waitForAssignmentEmail(t, sent)
	rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, rows, 6)
	keys := []string{}
	for _, row := range rows {
		keys = append(keys, row.TaskKey)
		require.Equal(t, ac.UserID, row.AssigneeUserID.String)
		if row.TaskKey == "enable-logging" {
			require.True(t, row.HiddenAt.Valid, "assigning hidden tasks must not expose them")
		}
	}
	require.ElementsMatch(t, []string{"enable-logging", "anthropic-observability", "instrument-agents", "litellm", "additional-agent-config", "confirm-traffic"}, keys)
	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Equal(t, listed.Workstreams, result.Workstreams)
	require.Equal(t, []string{"anthropic-observability", "instrument-agents", "additional-agent-config"}, result.Workstreams[1].TaskKeys, "response omits hidden membership while assignment above includes it")
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	_, err = ti.service.AssignSetupWorkstream(ctx, payload)
	require.NoError(t, err)
	again, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	require.Equal(t, count, again, "no-op must not write or audit")
	_, err = ti.service.AssignSetupWorkstream(ctx, &gen.AssignSetupWorkstreamPayload{Workstream: "observe", ClearAssignee: conv.PtrEmpty(true)})
	require.NoError(t, err)
	rows, err = orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	for _, row := range rows {
		require.False(t, row.AssigneeUserID.Valid)
		require.False(t, row.AssigneeEmail.Valid)
	}
	require.Never(t, func() bool {
		select {
		case <-sent:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 5*time.Millisecond, "no-op and unassignment must not notify")
	ti.loops.AssertNumberOfCalls(t, "SendTransactional", 1)
}

func TestService_AssignSetupWorkstreamRollback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	// Audit runs after all task writes, so rejecting it proves the entire batch rolls back.
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated))
	result, err := ti.service.AssignSetupWorkstream(ctx, &gen.AssignSetupWorkstreamPayload{Workstream: "connect", Assignee: &gen.SetupTaskAssigneeInput{UserID: &ac.UserID}})
	require.Error(t, err)
	require.Nil(t, result)
	rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows)
	ti.loops.AssertNotCalled(t, "SendTransactional", mock.Anything, mock.Anything)
}

func TestService_AssignSetupWorkstreamValidationAndAuthorization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	assignee := &gen.SetupTaskAssigneeInput{UserID: &ac.UserID}
	cases := []*gen.AssignSetupWorkstreamPayload{
		{Workstream: "unknown", Assignee: assignee},
		{Workstream: "connect"},
		{Workstream: "connect", ClearAssignee: conv.PtrEmpty(false)},
		{Workstream: "connect", Assignee: assignee, ClearAssignee: conv.PtrEmpty(true)},
		{Workstream: "connect", Assignee: &gen.SetupTaskAssigneeInput{UserID: conv.PtrEmpty("outside-org")}},
		{Workstream: "connect", Assignee: &gen.SetupTaskAssigneeInput{Email: conv.PtrEmpty("invalid")}},
	}
	for _, payload := range cases {
		_, err := ti.service.AssignSetupWorkstream(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	payload := &gen.AssignSetupWorkstreamPayload{Workstream: "connect", Assignee: assignee}
	_, err := ti.service.AssignSetupWorkstream(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID)), payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.AssignSetupWorkstream(context.Background(), payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	rows, err := orgrepo.New(ti.conn).ListOrganizationSetupTasks(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows)
}
