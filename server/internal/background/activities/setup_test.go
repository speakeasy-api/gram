package activities_test

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true, Redis: true})
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

// captureLoopsClient records every transactional email the activity attempts
// to send so tests can assert on the exact payloads.
type captureLoopsClient struct {
	mu   sync.Mutex
	sent []loops.SendTransactionalInput
	// failNext makes the next SendTransactional calls fail without recording,
	// simulating a transport outage.
	failNext int
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext > 0 {
		c.failNext--
		return errors.New("loops unavailable")
	}
	c.sent = append(c.sent, input)
	return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]loops.SendTransactionalInput, len(c.sent))
	copy(out, c.sent)
	return out
}

func (c *captureLoopsClient) FailNext(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failNext = n
}

func setupSnapshotBillingCycleUsageTest(t *testing.T, dbName string) (act *activities.SnapshotBillingCycleUsage, conn *pgxpool.Pool, chConn clickhouse.Conn, orgID string, projectID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	chConn, err = infra.NewClickhouseClient(t)
	require.NoError(t, err)

	orgID = "org-" + uuid.NewString()[:8]
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
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	act = activities.NewSnapshotBillingCycleUsage(
		testenv.NewLogger(t),
		conn,
		chConn,
	)

	return act, conn, chConn, orgID, project.ID
}
