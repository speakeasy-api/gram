package repo_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func TestUpdateTriggerInstanceEnvironmentTriState(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, orgID := newTriggerRepoTestContext(t)
	queries := triggerrepo.New(conn)
	envQueries := envrepo.New(conn)

	environment, err := envQueries.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           "Test Environment",
		Slug:           "test-environment",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	instance, err := queries.CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		DefinitionSlug: "cron",
		Name:           "Nightly Sync",
		EnvironmentID:  uuid.NullUUID{UUID: environment.ID, Valid: true},
		TargetKind:     "assistant",
		TargetRef:      "assistant-ref",
		TargetDisplay:  "Assistant",
		ConfigJson:     []byte(`{}`),
		Status:         "active",
	})
	require.NoError(t, err)
	require.True(t, instance.EnvironmentID.Valid)

	preserved, err := queries.UpdateTriggerInstance(ctx, triggerrepo.UpdateTriggerInstanceParams{
		Name:                pgtype.Text{String: "Renamed", Valid: true},
		UpdateEnvironmentID: false,
		EnvironmentID:       uuid.NullUUID{},
		ID:                  instance.ID,
		ProjectID:           projectID,
	})
	require.NoError(t, err)
	require.True(t, preserved.EnvironmentID.Valid)
	require.Equal(t, environment.ID, preserved.EnvironmentID.UUID)

	cleared, err := queries.UpdateTriggerInstance(ctx, triggerrepo.UpdateTriggerInstanceParams{
		UpdateEnvironmentID: true,
		EnvironmentID:       uuid.NullUUID{},
		ID:                  instance.ID,
		ProjectID:           projectID,
	})
	require.NoError(t, err)
	require.False(t, cleared.EnvironmentID.Valid)
}

func newTriggerRepoTestContext(t *testing.T) (context.Context, *pgxpool.Pool, uuid.UUID, string) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := fmt.Sprintf("org-%s", uuid.NewString()[:8])

	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           fmt.Sprintf("test-%s", uuid.NewString()[:8]),
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return ctx, conn, project.ID, orgID
}

func TestUpsertTriggerThreadRouteStateOnlyAdvancesCursor(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, _ := newTriggerRepoTestContext(t)
	queries := triggerrepo.New(conn)

	subscribe := func(cursor string) triggerrepo.TriggerThreadRoute {
		row, err := queries.UpsertTriggerThreadRouteState(ctx, triggerrepo.UpsertTriggerThreadRouteStateParams{
			ProjectID:      projectID,
			TargetKind:     "assistant",
			TargetRef:      "assistant-1",
			CorrelationID:  "C1:1.0",
			State:          "subscribed",
			LastSeenCursor: pgtype.Text{String: cursor, Valid: cursor != ""},
		})
		require.NoError(t, err)
		return row
	}

	require.Equal(t, "1700000002.000100", subscribe("1700000002.000100").LastSeenCursor.String)
	require.Equal(t, "1700000002.000100", subscribe("1700000001.999999").LastSeenCursor.String)
	require.Equal(t, "1700000002.000100", subscribe("").LastSeenCursor.String)
	require.Equal(t, "1700000003.000000", subscribe("1700000003.000000").LastSeenCursor.String)
}

func TestSetTriggerThreadRoutesStateForTargetCoversRoutedThreads(t *testing.T) {
	t.Parallel()

	ctx, conn, projectID, _ := newTriggerRepoTestContext(t)
	queries := triggerrepo.New(conn)

	for _, params := range []triggerrepo.RouteTriggerThreadParams{
		{ProjectID: projectID, TargetKind: "assistant", TargetRef: "assistant-1", CorrelationID: "C1:1.0", RouteToCorrelationID: pgtype.Text{}, State: "subscribed", LastSeenCursor: pgtype.Text{}},
		{ProjectID: projectID, TargetKind: "assistant", TargetRef: "assistant-1", CorrelationID: "C1:2.0", RouteToCorrelationID: pgtype.Text{String: "C1:1.0", Valid: true}, State: "subscribed", LastSeenCursor: pgtype.Text{}},
		{ProjectID: projectID, TargetKind: "assistant", TargetRef: "assistant-1", CorrelationID: "C1:3.0", RouteToCorrelationID: pgtype.Text{}, State: "subscribed", LastSeenCursor: pgtype.Text{}},
		{ProjectID: projectID, TargetKind: "assistant", TargetRef: "assistant-2", CorrelationID: "C1:1.0", RouteToCorrelationID: pgtype.Text{}, State: "subscribed", LastSeenCursor: pgtype.Text{}},
	} {
		_, err := queries.RouteTriggerThread(ctx, params)
		require.NoError(t, err)
	}

	updated, err := queries.SetTriggerThreadRoutesStateForTarget(ctx, triggerrepo.SetTriggerThreadRoutesStateForTargetParams{
		State:         "unsubscribed",
		ProjectID:     projectID,
		TargetKind:    "assistant",
		TargetRef:     "assistant-1",
		CorrelationID: "C1:1.0",
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, updated)

	state := func(targetRef, correlationID string) string {
		row, err := queries.GetTriggerThreadRoute(ctx, triggerrepo.GetTriggerThreadRouteParams{
			ProjectID:     projectID,
			TargetKind:    "assistant",
			TargetRef:     targetRef,
			CorrelationID: correlationID,
		})
		require.NoError(t, err)
		return row.State
	}
	require.Equal(t, "unsubscribed", state("assistant-1", "C1:1.0"))
	require.Equal(t, "unsubscribed", state("assistant-1", "C1:2.0"))
	require.Equal(t, "subscribed", state("assistant-1", "C1:3.0"))
	require.Equal(t, "subscribed", state("assistant-2", "C1:1.0"))
}
