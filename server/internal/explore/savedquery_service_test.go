package explore

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSavedQueryMutationRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, instance := newExploreTestService(t)
	ctx = authztest.WithExactGrants(
		t,
		ctx,
		authz.NewGrant(authz.ScopeOrgRead, instance.organizationID),
	)

	_, err := instance.service.CreateSavedQuery(ctx, newCreateSavedQueryPayload())
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestSavedQueryMutationsAreTransactionalAndAudited(t *testing.T) {
	t.Parallel()

	ctx, instance := newExploreTestService(t)

	createBefore, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryCreate)
	require.NoError(t, err)
	updateBefore, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryUpdate)
	require.NoError(t, err)
	deleteBefore, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryDelete)
	require.NoError(t, err)

	created, err := instance.service.CreateSavedQuery(ctx, newCreateSavedQueryPayload())
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "events", created.Dataset)
	require.Equal(t, []*gen.ExploreCalculation{{Op: "COUNT", Column: nil}}, created.Calculations)

	listed, err := instance.service.ListSavedQueries(ctx, &gen.ListSavedQueriesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Queries, 1)
	require.Equal(t, "events", listed.Queries[0].Dataset)
	require.Equal(t, []*gen.ExploreCalculation{{Op: "COUNT", Column: nil}}, listed.Queries[0].Calculations)

	updated, err := instance.service.UpdateSavedQuery(ctx, &gen.UpdateSavedQueryPayload{
		ID:           created.ID,
		Name:         "Turn cost by provider",
		ChartType:    "table",
		Window:       "30d",
		Dataset:      "turn_usage",
		Calculations: []*gen.ExploreCalculation{{Op: "SUM", Column: conv.PtrEmpty("cost_usd")}},
		GroupBy:      []string{"provider"},
		GroupExpressions: []*gen.ExploreGroupExpression{{
			Name:      "Is Claude",
			Dimension: "response_model",
			Op:        "in",
			Values:    []string{"claude"},
		}},
		Filters:            nil,
		GranularitySeconds: nil,
		SortBy:             nil,
		SortDesc:           true,
		Limit:              25,
		SessionToken:       nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Turn cost by provider", updated.Name)
	require.Equal(t, "turn_usage", updated.Dataset)
	require.Equal(t, "SUM", updated.Calculations[0].Op)
	require.Equal(t, "cost_usd", *updated.Calculations[0].Column)
	require.Equal(t, []*gen.ExploreGroupExpression{{
		Name:      "Is Claude",
		Dimension: "response_model",
		Op:        "in",
		Values:    []string{"claude"},
	}}, updated.GroupExpressions)

	listed, err = instance.service.ListSavedQueries(ctx, &gen.ListSavedQueriesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Queries, 1)
	require.Equal(t, updated.GroupExpressions, listed.Queries[0].GroupExpressions)

	require.NoError(t, instance.service.DeleteSavedQuery(ctx, &gen.DeleteSavedQueryPayload{
		ID:           created.ID,
		SessionToken: nil,
	}))

	listed, err = instance.service.ListSavedQueries(ctx, &gen.ListSavedQueriesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Queries)

	createAfter, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryCreate)
	require.NoError(t, err)
	updateAfter, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryUpdate)
	require.NoError(t, err)
	deleteAfter, err := audittest.AuditLogCountByAction(ctx, instance.conn, audit.ActionExploreSavedQueryDelete)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
	require.Equal(t, updateBefore+1, updateAfter)
	require.Equal(t, deleteBefore+1, deleteAfter)

	updateLog, err := audittest.LatestAuditLogByAction(ctx, instance.conn, audit.ActionExploreSavedQueryUpdate)
	require.NoError(t, err)
	require.NotEmpty(t, updateLog.BeforeSnapshot)
	require.NotEmpty(t, updateLog.AfterSnapshot)
	require.False(t, updateLog.ProjectID.Valid)
}

func newCreateSavedQueryPayload() *gen.CreateSavedQueryPayload {
	return &gen.CreateSavedQueryPayload{
		Name:               "Events by name",
		ChartType:          "bar",
		Window:             "7d",
		Dataset:            "events",
		Calculations:       []*gen.ExploreCalculation{{Op: "COUNT", Column: nil}},
		GroupBy:            []string{"event_name"},
		GroupExpressions:   nil,
		Filters:            nil,
		GranularitySeconds: nil,
		SortBy:             nil,
		SortDesc:           true,
		Limit:              10,
		SessionToken:       nil,
	}
}
