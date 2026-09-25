package activities_test

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
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

func newTrialTestInstance(t *testing.T) (context.Context, *trialTestInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "trialdemotion")
	require.NoError(t, err)

	provisioner := &trialProvisioner{Development: openrouter.NewDevelopment(""), local: new(openrouter.OpenRouter)}
	notifier := &recordingTrialNotifier{inactive: nil}
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	productFeatures := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)

	return ctx, &trialTestInstance{
		conn:            conn,
		trials:          trialsrepo.New(conn),
		orgs:            orgrepo.New(conn),
		productFeatures: productFeatures,
		provisioner:     provisioner,
		notifier:        notifier,
		activity: activities.NewDemoteExpiredTrials(
			testenv.NewLogger(t),
			conn,
			provisioner,
			audit.NewLogger(),
			notifier,
			productFeatures,
			nil,
		),
	}
}

func newTrialFeatureCache(t *testing.T, conn *pgxpool.Pool) (*miniredis.Miniredis, *productfeatures.Client) {
	t.Helper()

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)
	return redisServer, features
}
