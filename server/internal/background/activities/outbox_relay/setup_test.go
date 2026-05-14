package outbox_relay_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"

	"github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"
	bgactivitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	outboxrepo "github.com/speakeasy-api/gram/server/internal/outbox/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	svixtest "github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Printf("cleanup failed: %v", err)
	}
	os.Exit(code)
}

type relayTestInstance struct {
	conn     *pgxpool.Pool
	relay    *outbox_relay.Relay
	svixSrv  *svixtest.MockServer
	features *productfeatures.Client
}

func newRelayTestInstance(t *testing.T) *relayTestInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)

	svixClient, err := svix.New("test-token", &svix.SvixOptions{
		ServerUrl:     svixSrv.URL(),
		RetrySchedule: &[]time.Duration{},
	})
	require.NoError(t, err)

	features := productfeatures.NewClient(logger, tp, conn, redisClient)
	relay := outbox_relay.New(logger, tp, conn, svixClient, features)
	return &relayTestInstance{
		conn:     conn,
		relay:    relay,
		svixSrv:  svixSrv,
		features: features,
	}
}

// seedOrg inserts an organization_metadata row and configures its svix settings.
// Returns a unique orgID.
func seedOrg(t *testing.T, conn *pgxpool.Pool, svixAppID string, webhooksEnabled bool) string {
	t.Helper()
	ctx := t.Context()
	orgID := uuid.NewString()

	q := orgsrepo.New(conn)
	_, err := q.UpsertOrganizationMetadata(ctx, orgsrepo.UpsertOrganizationMetadataParams{
		ID:   orgID,
		Name: orgID,
		Slug: orgID,
	})
	require.NoError(t, err)

	err = testrepo.New(conn).SetOrgWebhookConfig(ctx, testrepo.SetOrgWebhookConfigParams{
		OrganizationID:  orgID,
		SvixAppID:       conv.ToPGTextEmpty(svixAppID),
		WebhooksEnabled: pgtype.Bool{Bool: webhooksEnabled, Valid: true},
	})
	require.NoError(t, err)

	return orgID
}

// seedOutboxEntry inserts a row into the outbox table and returns its ID.
func seedOutboxEntry(t *testing.T, conn *pgxpool.Pool, orgID, eventType string, payload []byte) int64 {
	t.Helper()
	ctx := t.Context()
	row, err := outboxrepo.New(conn).InsertOutboxEntry(ctx, outboxrepo.InsertOutboxEntryParams{
		OrganizationID: orgID,
		EventType:      eventType,
		Payload:        payload,
	})
	require.NoError(t, err)
	return row.ID
}

// enableWebhooksFeature enables the webhooks feature flag for an org in the DB.
func enableWebhooksFeature(t *testing.T, conn *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := t.Context()
	_, err := productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeatureWebhooks),
	})
	require.NoError(t, err)
}

// preloadAttempts calls MarkOutboxRelayFailed n times to simulate prior retry attempts.
func preloadAttempts(t *testing.T, conn *pgxpool.Pool, outboxID int64, n int) {
	t.Helper()
	ctx := t.Context()
	q := bgactivitiesrepo.New(conn)
	for range n {
		err := q.MarkOutboxRelayFailed(ctx, bgactivitiesrepo.MarkOutboxRelayFailedParams{
			OutboxID:   outboxID,
			LastError:  conv.ToPGTextEmpty("prior error"),
			RetryAfter: pgtype.Timestamptz{},
		})
		require.NoError(t, err)
	}
}
