package activities_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// autoSyncFixture is the minimal set of rows needed to exercise the
// AutoSyncToolsets activity end-to-end. Each helper below performs a single
// INSERT so failures localise cleanly.
type autoSyncFixture struct {
	orgID        string
	projectID    uuid.UUID
	deploymentID uuid.UUID
	functionID   uuid.UUID
	assetID      uuid.UUID
	toolsetID    uuid.UUID
}

func newAutoSyncFixture(t *testing.T, ctx context.Context, conn *pgxpool.Pool, toolsetAutoSyncSources []string, toolURNs []string, functionSlug string) autoSyncFixture {
	t.Helper()
	fx := autoSyncFixture{
		orgID:        "org_" + uuid.NewString(),
		projectID:    uuid.New(),
		deploymentID: uuid.New(),
		functionID:   uuid.New(),
		assetID:      uuid.New(),
		toolsetID:    uuid.New(),
	}

	_, err := conn.Exec(ctx,
		`INSERT INTO organization_metadata (id, name, slug) VALUES ($1, $2, $3)`,
		fx.orgID, "Auto-Sync Test Org", "auto-sync-test-"+uuid.NewString()[:8])
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO projects (id, organization_id, name, slug) VALUES ($1, $2, $3, $4)`,
		fx.projectID, fx.orgID, "auto-sync-project", "auto-sync-project-"+uuid.NewString()[:8])
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO assets (id, project_id, name, kind, content_type, sha256, content_length, url)
		 VALUES ($1, $2, 'function-asset', 'function', 'application/zip', repeat('a', 64), 0, 'memory://test')`,
		fx.assetID, fx.projectID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO deployments (id, organization_id, project_id, user_id, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5)`,
		fx.deploymentID, fx.orgID, fx.projectID, "test-user", "auto-sync-idempotency-"+uuid.NewString())
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO deployment_statuses (deployment_id, status) VALUES ($1, 'completed')`,
		fx.deploymentID)
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO deployments_functions (id, deployment_id, asset_id, name, slug, runtime)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		fx.functionID, fx.deploymentID, fx.assetID, functionSlug, functionSlug, "nodejs:22")
	require.NoError(t, err)

	for _, u := range toolURNs {
		_, err = conn.Exec(ctx,
			`INSERT INTO function_tool_definitions
			   (id, project_id, deployment_id, function_id, runtime, name, description, tool_urn)
			 VALUES ($1, $2, $3, $4, 'nodejs:22', $5, '', $6)`,
			uuid.New(), fx.projectID, fx.deploymentID, fx.functionID, u, u)
		require.NoError(t, err)
	}

	if toolsetAutoSyncSources == nil {
		toolsetAutoSyncSources = []string{}
	}
	_, err = conn.Exec(ctx,
		`INSERT INTO toolsets (id, organization_id, project_id, name, slug, auto_sync_sources)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		fx.toolsetID, fx.orgID, fx.projectID, "auto-sync-toolset", "auto-sync-toolset-"+uuid.NewString()[:8], toolsetAutoSyncSources)
	require.NoError(t, err)

	return fx
}

func TestAutoSyncToolsets_EmptyDeployment_NoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn, err := infra.CloneTestDatabase(t, "auto_sync_empty")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	fx := newAutoSyncFixture(t, ctx, conn,
		[]string{"function:my-tools"}, // toolset is subscribed
		nil,                           // but the deployment has no tools
		"my-tools",
	)

	act := activities.NewAutoSyncToolsets(logger, conn, audit.NewLogger())
	res, err := act.Do(ctx, activities.AutoSyncToolsetsRequest{
		ProjectID:    fx.projectID,
		DeploymentID: fx.deploymentID,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 0, res.ToolsetsExtended)
	require.Empty(t, res.AddedByToolset)
}

func TestAutoSyncToolsets_SubscribedToolsetExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn, err := infra.CloneTestDatabase(t, "auto_sync_extend")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	fx := newAutoSyncFixture(t, ctx, conn,
		[]string{"function:billing"},
		[]string{"tools:function:billing:list_invoices", "tools:function:billing:create_invoice"},
		"billing",
	)

	act := activities.NewAutoSyncToolsets(logger, conn, audit.NewLogger())
	res, err := act.Do(ctx, activities.AutoSyncToolsetsRequest{
		ProjectID:    fx.projectID,
		DeploymentID: fx.deploymentID,
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.ToolsetsExtended)
	require.Len(t, res.AddedByToolset, 1)

	var firstKey string
	for k := range res.AddedByToolset {
		firstKey = k
	}
	require.ElementsMatch(t,
		[]string{"tools:function:billing:list_invoices", "tools:function:billing:create_invoice"},
		res.AddedByToolset[firstKey],
	)

	// Toolset version row was created with the added URNs.
	tsRepo := toolsetsRepo.New(conn)
	latest, err := tsRepo.GetLatestToolsetVersion(ctx, fx.toolsetID)
	require.NoError(t, err)
	require.Equal(t, int64(1), latest.Version)
	require.Len(t, latest.ToolUrns, 2)

	// Audit event was recorded.
	var auditCount int
	err = conn.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE action = $1 AND subject_id = $2`,
		string(audit.ActionToolsetToolsAutoAdded), fx.toolsetID,
	).Scan(&auditCount)
	require.NoError(t, err)
	require.Equal(t, 1, auditCount)
}

func TestAutoSyncToolsets_Idempotent_ReplayIsNoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn, err := infra.CloneTestDatabase(t, "auto_sync_idempotent")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	fx := newAutoSyncFixture(t, ctx, conn,
		[]string{"function:catalog"},
		[]string{"tools:function:catalog:search"},
		"catalog",
	)

	act := activities.NewAutoSyncToolsets(logger, conn, audit.NewLogger())
	req := activities.AutoSyncToolsetsRequest{
		ProjectID:    fx.projectID,
		DeploymentID: fx.deploymentID,
	}

	res1, err := act.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, res1.ToolsetsExtended)

	res2, err := act.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 0, res2.ToolsetsExtended, "replay should add nothing")

	// Still exactly one toolset version row.
	var versionCount int
	err = conn.QueryRow(ctx,
		`SELECT count(*) FROM toolset_versions WHERE toolset_id = $1`, fx.toolsetID,
	).Scan(&versionCount)
	require.NoError(t, err)
	require.Equal(t, 1, versionCount)

	// Still exactly one audit event.
	var auditCount int
	err = conn.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE action = $1 AND subject_id = $2`,
		string(audit.ActionToolsetToolsAutoAdded), fx.toolsetID,
	).Scan(&auditCount)
	require.NoError(t, err)
	require.Equal(t, 1, auditCount)
}

func TestAutoSyncToolsets_NoSubscribers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn, err := infra.CloneTestDatabase(t, "auto_sync_no_subs")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	fx := newAutoSyncFixture(t, ctx, conn,
		nil, // toolset has no subscriptions
		[]string{"tools:function:billing:list_invoices"},
		"billing",
	)

	act := activities.NewAutoSyncToolsets(logger, conn, audit.NewLogger())
	res, err := act.Do(ctx, activities.AutoSyncToolsetsRequest{
		ProjectID:    fx.projectID,
		DeploymentID: fx.deploymentID,
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ToolsetsExtended)
	require.Empty(t, res.AddedByToolset)
}

func TestAutoSyncToolsets_SubscribedToWrongSource_NoExtension(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn, err := infra.CloneTestDatabase(t, "auto_sync_wrong_source")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	fx := newAutoSyncFixture(t, ctx, conn,
		[]string{"function:other-source"}, // subscribed to a source not in this deployment
		[]string{"tools:function:billing:list_invoices"},
		"billing",
	)

	act := activities.NewAutoSyncToolsets(logger, conn, audit.NewLogger())
	res, err := act.Do(ctx, activities.AutoSyncToolsetsRequest{
		ProjectID:    fx.projectID,
		DeploymentID: fx.deploymentID,
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ToolsetsExtended)
}
