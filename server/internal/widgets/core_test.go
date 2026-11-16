package widgets_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	dashboardsrepo "github.com/speakeasy-api/gram/server/internal/dashboards/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

func TestReadReportsTheVersionAtFullPrecision(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("versioned", validQuery(), barChart()))
	require.NoError(t, err)

	snapshot, err := ti.service.Read(ctx, created.ID)
	require.NoError(t, err)
	version, err := time.Parse(time.RFC3339Nano, snapshot.Version)
	require.NoError(t, err)
	viewed, err := time.Parse(time.RFC3339, snapshot.Widget.UpdatedAt)
	require.NoError(t, err)
	require.Equal(t, viewed, version.Truncate(time.Second), "the version is the updated_at the view rounds")

	all, err := ti.service.ReadAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, snapshot.Version, all[0].Version)
}

func TestReadRecentReadsNoMoreThanItsLimit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	for _, name := range []string{"first", "second", "third"} {
		_, err := ti.service.CreateWidget(ctx, createPayload(name, validQuery(), barChart()))
		require.NoError(t, err)
	}

	recent, err := ti.service.ReadRecent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	require.Equal(t, "third", recent[0].Widget.Name)
	require.Equal(t, "second", recent[1].Widget.Name)
	require.NotNil(t, recent[0].Widget.Dashboards)
}

func TestCheckEnforcesNameAndDescriptionLimits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	long := strings.Repeat("x", 2001)
	for name, input := range map[string]widgets.Input{
		"empty name":       {Name: "", Description: nil, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()},
		"long name":        {Name: long[:201], Description: nil, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()},
		"long description": {Name: "ok", Description: &long, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()},
	} {
		err := ti.service.Check(ctx, input)
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	require.NoError(t, ti.service.Check(ctx, widgets.Input{Name: long[:200], Description: new(long[:2000]), Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()}))
}

func TestReadForChangeAppliesTheCreatorOrWriteRule(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	member := asMember(t, ctx, ti, "user_member")
	mine, err := ti.service.CreateWidget(member, createPayload("mine", validQuery(), barChart()))
	require.NoError(t, err)
	theirs, err := ti.service.CreateWidget(ctx, createPayload("theirs", validQuery(), barChart()))
	require.NoError(t, err)

	_, err = ti.service.ReadForChange(member, mine.ID)
	require.NoError(t, err, "a creator may change their own widget with membership alone")
	_, err = ti.service.ReadForChange(member, theirs.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ReadForChange(ctx, mine.ID)
	require.NoError(t, err, "project write access may change anyone's widget")
}

func TestAuthorizeChangeAppliesTheCreatorOrWriteRuleBeforeAnyTransaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	member := asMember(t, ctx, ti, "user_member")
	mine, err := ti.service.CreateWidget(member, createPayload("mine", validQuery(), barChart()))
	require.NoError(t, err)
	theirs, err := ti.service.CreateWidget(ctx, createPayload("theirs", validQuery(), barChart()))
	require.NoError(t, err)

	_, err = ti.service.AuthorizeChange(member, mine.ID)
	require.NoError(t, err)
	_, err = ti.service.AuthorizeChange(member, theirs.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.AuthorizeChange(ctx, theirs.ID)
	require.NoError(t, err)

	missing := authorizeChange(t, member, ti, uuid.NewString())
	tx := beginTx(t, ctx, ti)
	_, err = ti.service.DeleteInTx(ctx, tx, missing, "")
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateInTxRefusesAStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("original", validQuery(), barChart()))
	require.NoError(t, err)
	read, err := ti.service.Read(ctx, created.ID)
	require.NoError(t, err)

	_, err = ti.service.UpdateWidget(ctx, &gen.UpdateWidgetPayload{ID: created.ID, Name: "someone else's edit", Description: nil, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	tx := beginTx(t, ctx, ti)
	_, err = ti.service.UpdateInTx(ctx, tx, authorizeChange(t, ctx, ti, created.ID), widgets.Input{Name: "stale edit", Description: nil, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()}, read.Version)
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorIs(t, err, widgets.ErrVersionChanged)
	require.NoError(t, tx.Rollback(ctx))

	current, err := ti.service.Read(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "someone else's edit", current.Widget.Name)

	tx = beginTx(t, ctx, ti)
	updated, err := ti.service.UpdateInTx(ctx, tx, authorizeChange(t, ctx, ti, created.ID), widgets.Input{Name: "fresh edit", Description: nil, Dataset: "tool_calls", Query: validQuery(), Visualization: barChart()}, current.Version)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, "fresh edit", updated.Widget.Name)
	require.NotEqual(t, current.Version, updated.Version, "every write moves the version")
}

func TestDeleteInTxRefusesAStaleVersionAndReportsDashboards(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("placed", validQuery(), barChart()))
	require.NoError(t, err)
	read, err := ti.service.Read(ctx, created.ID)
	require.NoError(t, err)

	dashboards := dashboardsrepo.New(ti.conn)
	dashboard, err := dashboards.CreateDashboard(ctx, dashboardsrepo.CreateDashboardParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, CreatedByUserID: pgtype.Text{String: ti.userID, Valid: true},
		Name: "Ops", Description: pgtype.Text{String: "", Valid: false}, Filters: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = dashboards.InsertPlacement(ctx, dashboardsrepo.InsertPlacementParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, DashboardID: dashboard.ID, WidgetID: uuid.MustParse(created.ID), X: 0, Y: 0, W: 4, H: 3,
	})
	require.NoError(t, err)

	tx := beginTx(t, ctx, ti)
	_, err = ti.service.DeleteInTx(ctx, tx, authorizeChange(t, ctx, ti, created.ID), "2001-01-01T00:00:00Z")
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorIs(t, err, widgets.ErrVersionChanged)
	require.NoError(t, tx.Rollback(ctx))

	tx = beginTx(t, ctx, ti)
	removed, err := ti.service.DeleteInTx(ctx, tx, authorizeChange(t, ctx, ti, created.ID), read.Version)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, []*gen.WidgetDashboard{{ID: dashboard.ID.String(), Name: "Ops"}}, removed)

	_, err = ti.service.Read(ctx, created.ID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDuplicateInTxRefusesAStaleSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created, err := ti.service.CreateWidget(ctx, createPayload("source", validQuery(), barChart()))
	require.NoError(t, err)

	tx := beginTx(t, ctx, ti)
	_, err = ti.service.DuplicateInTx(ctx, tx, authorizeCaller(t, ctx, ti), created.ID, "2001-01-01T00:00:00Z")
	requireOopsCode(t, err, oops.CodeConflict)
	require.NoError(t, tx.Rollback(ctx))

	read, err := ti.service.Read(ctx, created.ID)
	require.NoError(t, err)
	tx = beginTx(t, ctx, ti)
	copied, err := ti.service.DuplicateInTx(ctx, tx, authorizeCaller(t, ctx, ti), created.ID, read.Version)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, "source (copy)", copied.Widget.Name)
}

func authorizeCaller(t *testing.T, ctx context.Context, ti *testInstance) widgets.Caller {
	t.Helper()
	caller, err := ti.service.Authorize(ctx)
	require.NoError(t, err)
	return caller
}

func authorizeChange(t *testing.T, ctx context.Context, ti *testInstance, id string) widgets.Change {
	t.Helper()
	change, err := ti.service.AuthorizeChange(ctx, id)
	require.NoError(t, err)
	return change
}
