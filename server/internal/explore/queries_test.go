package explore_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	explorerepo "github.com/speakeasy-api/gram/server/internal/explore/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func validSpec() map[string]any {
	return map[string]any{
		"chart_type": "bar",
		"window":     "7d",
		"grain":      "day",
		"dimensions": []any{"user"},
		"measures":   []any{map[string]any{"op": "count"}, map[string]any{"op": "sum", "field": "tool_call_count", "alias": "tool_calls"}},
		"filters":    []any{map[string]any{"field": "surface", "operator": "in", "values": []any{"claude-code"}}},
		"limit":      50,
	}
}

func createPayload(name string, spec map[string]any) *gen.CreateQueryPayload {
	return &gen.CreateQueryPayload{Name: name, Dataset: "sessions", Spec: spec, SessionToken: nil, ProjectSlugInput: nil}
}

func TestCreateQuery(t *testing.T) {
	t.Parallel()

	t.Run("it saves a valid query, records the creator, and audits it", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryCreate)
		require.NoError(t, err)

		created, err := ti.service.CreateQuery(ctx, createPayload("Tool calls by user", validSpec()))
		require.NoError(t, err)
		require.Equal(t, "Tool calls by user", created.Name)
		require.Equal(t, "sessions", created.Dataset)
		require.Equal(t, ti.projectID.String(), created.ProjectID)
		require.NotNil(t, created.CreatedByUserID)
		require.Equal(t, ti.userID, *created.CreatedByUserID)
		require.Equal(t, "bar", created.Spec["chart_type"], "presentation state passes through untouched")
		require.Nil(t, created.InvalidReason)

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryCreate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)
	})

	t.Run("it rejects a spec the catalog cannot plan, naming the field", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		spec := validSpec()
		spec["dimensions"] = []any{"department"}
		_, err := ti.service.CreateQuery(ctx, createPayload("bad", spec))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_field")
		require.ErrorContains(t, err, "department")
	})

	t.Run("it rejects an enum value the query endpoint would refuse on replay", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		spec := validSpec()
		spec["measures"] = []any{map[string]any{"op": "COUNT"}}
		_, err := ti.service.CreateQuery(ctx, createPayload("shouty", spec))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "measures[0].op")
		require.ErrorContains(t, err, "lowercase")
	})

	t.Run("it rejects an unknown dataset", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		payload := createPayload("bad", validSpec())
		payload.Dataset = "departments"
		_, err := ti.service.CreateQuery(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_dataset")
	})

	t.Run("it rejects an absolute or unknown window", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		spec := validSpec()
		spec["window"] = "2026-09-01/2026-09-08"
		_, err := ti.service.CreateQuery(ctx, createPayload("bad", spec))
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "window")
	})

	t.Run("it lets any member save", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		memberCtx := asMember(t, ctx, ti, "user_member_"+uuid.NewString())
		created, err := ti.service.CreateQuery(memberCtx, createPayload("member's", validSpec()))
		require.NoError(t, err)
		require.NotNil(t, created.CreatedByUserID)
	})

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestService(t)
		_, err := ti.service.CreateQuery(t.Context(), createPayload("x", validSpec()))
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

func TestListQueries(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	first, err := ti.service.CreateQuery(ctx, createPayload("first", validSpec()))
	require.NoError(t, err)
	second, err := ti.service.CreateQuery(ctx, createPayload("second", validSpec()))
	require.NoError(t, err)

	// A row whose spec the catalog no longer accepts, as a catalog change
	// would leave behind. Written directly, since the service refuses it.
	stale, err := json.Marshal(map[string]any{"window": "7d", "dimensions": []string{"department"}, "measures": []map[string]any{{"op": "count"}}})
	require.NoError(t, err)
	_, err = explorerepo.New(ti.conn).CreateQuery(ctx, explorerepo.CreateQueryParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: "stale", Dataset: "sessions", Spec: stale,
	})
	require.NoError(t, err)

	result, err := ti.service.ListQueries(ctx, &gen.ListQueriesPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, result.Queries, 3)
	require.Equal(t, "stale", result.Queries[0].Name, "most recently updated first")
	require.NotNil(t, result.Queries[0].InvalidReason, "a spec a catalog change broke says so on read")
	require.Contains(t, *result.Queries[0].InvalidReason, "unknown_field")
	require.Equal(t, second.ID, result.Queries[1].ID)
	require.Nil(t, result.Queries[1].InvalidReason)
	require.Equal(t, first.ID, result.Queries[2].ID)

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.ListQueries(t.Context(), &gen.ListQueriesPayload{SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

func TestUpdateQuery(t *testing.T) {
	t.Parallel()

	t.Run("it replaces the query and audits before and after", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateQuery(ctx, createPayload("before", validSpec()))
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryUpdate)
		require.NoError(t, err)

		updated, err := ti.service.UpdateQuery(ctx, &gen.UpdateQueryPayload{ID: created.ID, Name: "after", Dataset: "tool_calls", Spec: map[string]any{
			"chart_type": "table", "window": "24h", "dimensions": []any{"tool_name", "status"}, "ungrouped": true,
		}, SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		require.Equal(t, "after", updated.Name)
		require.Equal(t, "tool_calls", updated.Dataset)
		require.Equal(t, true, updated.Spec["ungrouped"])

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryUpdate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionQueryUpdate)
		require.NoError(t, err)
		snapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
		require.NoError(t, err)
		require.Equal(t, "before", snapshot["Name"], "snapshot keys are the view's Go field names")
	})

	t.Run("it rejects a spec that does not plan", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateQuery(ctx, createPayload("before", validSpec()))
		require.NoError(t, err)
		spec := validSpec()
		spec["measures"] = []any{map[string]any{"op": "p95", "field": "turn_count"}}
		_, err = ti.service.UpdateQuery(ctx, &gen.UpdateQueryPayload{ID: created.ID, Name: "x", Dataset: "sessions", Spec: spec, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unsupported_aggregation")
	})

	t.Run("it reports an unknown query", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		_, err := ti.service.UpdateQuery(ctx, &gen.UpdateQueryPayload{ID: uuid.NewString(), Name: "x", Dataset: "sessions", Spec: validSpec(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})

	t.Run("it reports a deleted query as not found", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateQuery(ctx, createPayload("gone", validSpec()))
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteQuery(ctx, &gen.DeleteQueryPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		_, err = ti.service.UpdateQuery(ctx, &gen.UpdateQueryPayload{ID: created.ID, Name: "x", Dataset: "sessions", Spec: validSpec(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
		// A second delete is not found, not a fault.
		err = ti.service.DeleteQuery(ctx, &gen.DeleteQueryPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}

func TestDeleteQuery(t *testing.T) {
	t.Parallel()

	t.Run("its creator can delete it with membership alone", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		memberCtx := asMember(t, ctx, ti, "user_owner_"+uuid.NewString())
		created, err := ti.service.CreateQuery(memberCtx, createPayload("mine", validSpec()))
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryDelete)
		require.NoError(t, err)
		require.NoError(t, ti.service.DeleteQuery(memberCtx, &gen.DeleteQueryPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionQueryDelete)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		listed, err := ti.service.ListQueries(ctx, &gen.ListQueriesPayload{SessionToken: nil, ProjectSlugInput: nil})
		require.NoError(t, err)
		for _, q := range listed.Queries {
			require.NotEqual(t, created.ID, q.ID, "deleted queries are gone from the list")
		}
	})

	t.Run("another member cannot delete it without project write", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		created, err := ti.service.CreateQuery(ctx, createPayload("theirs", validSpec()))
		require.NoError(t, err)

		otherCtx := asMember(t, ctx, ti, "user_other_"+uuid.NewString())
		err = ti.service.DeleteQuery(otherCtx, &gen.DeleteQueryPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeForbidden)

		writerCtx := asMember(t, ctx, ti, "user_writer_"+uuid.NewString(), authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()))
		require.NoError(t, ti.service.DeleteQuery(writerCtx, &gen.DeleteQueryPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil}))
	})

	t.Run("it reports an unknown query", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		err := ti.service.DeleteQuery(ctx, &gen.DeleteQueryPayload{ID: uuid.NewString(), SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeNotFound)
	})
}
