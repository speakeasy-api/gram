package publish_outbox_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/topics"
	"github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const webhooksTopic = "gram.webhooks.v1.Event"

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
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

// fakePublisher records what was published and lets a test dictate the outcome
// per topic. It stands in for Pub/Sub so the relay's settlement logic can be
// driven through every branch without a broker.
type fakePublisher struct {
	mu sync.Mutex
	// calls counts attempts, published records deliveries. They are separate
	// because a failed broker call is an attempt that delivered nothing, and
	// conflating them would let messages() report a message no subscriber
	// could ever have seen.
	calls     int
	published []publishedMessage
	// failWith is consulted per call; return nil to succeed. The call index
	// counts every attempt, failed ones included, so a test can target the nth
	// publish in a batch.
	failWith func(topic string, call int) error
}

type publishedMessage struct {
	Topic      string
	Data       []byte
	Attributes map[string]string
}

var _ topics.Publisher = (*fakePublisher)(nil)

func newFakePublisher() *fakePublisher {
	return &fakePublisher{
		mu:        sync.Mutex{},
		published: nil,
		failWith:  nil,
	}
}

func (f *fakePublisher) Publish(ctx context.Context, topic string, data []byte, attributes map[string]string) gcp.PublishResult {
	f.mu.Lock()
	call := f.calls
	f.calls++
	fail := f.failWith
	f.mu.Unlock()

	if fail != nil {
		if err := fail(topic, call); err != nil {
			// Deliberately recorded nowhere. The real publisher delivers
			// nothing when the broker call fails — an unresolvable topic never
			// reaches Pub/Sub at all — so a test asserting that a retried or
			// dead-lettered row never went out has something truthful to read.
			return failedPublishResult{err: err}
		}
	}

	f.mu.Lock()
	f.published = append(f.published, publishedMessage{Topic: topic, Data: data, Attributes: attributes})
	f.mu.Unlock()

	return gcp.NewSuccessPublishResult()
}

func (f *fakePublisher) Stop(context.Context) error { return nil }

func (f *fakePublisher) messages() []publishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]publishedMessage(nil), f.published...)
}

type failedPublishResult struct{ err error }

func (r failedPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (r failedPublishResult) Get(context.Context) (string, error) { return "", r.err }

type relayTestInstance struct {
	conn  *pgxpool.Pool
	relay *publish_outbox.Relay
	pub   *fakePublisher
}

func newRelayTestInstance(t *testing.T) *relayTestInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	pub := newFakePublisher()

	return &relayTestInstance{
		conn:  conn,
		relay: publish_outbox.New(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, pub),
		pub:   pub,
	}
}

// publishOutboxRelayOver builds a second relay against an existing database,
// standing in for another worker process draining the same table.
func publishOutboxRelayOver(t *testing.T, conn *pgxpool.Pool) *relayTestInstance {
	t.Helper()

	pub := newFakePublisher()

	return &relayTestInstance{
		conn:  conn,
		relay: publish_outbox.New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, pub),
		pub:   pub,
	}
}

func seedOrg(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := uuid.NewString()
	_, err := orgsrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgsrepo.UpsertOrganizationMetadataParams{
		ID:   orgID,
		Name: orgID,
		Slug: orgID,
	})
	require.NoError(t, err)

	return orgID
}

type seedOptions struct {
	topic       string
	attempts    int32
	retryAfter  *time.Time
	lockedUntil *time.Time
	message     []byte
}

func seedRow(t *testing.T, conn *pgxpool.Pool, orgID string, opts seedOptions) testrepo.SeedPublishOutboxRowRow {
	t.Helper()

	if opts.topic == "" {
		opts.topic = webhooksTopic
	}
	if opts.message == nil {
		// Every seeded row gets a distinct body so tests can assert that a
		// message was published exactly once by identity, not just by count.
		eventID := uuid.NewString()
		eventType := "audit_log.asset_event_v1"
		opts.message = mustMarshal(t, webhooksv1.Event_builder{
			EventId:        &eventID,
			OrganizationId: &orgID,
			EventType:      &eventType,
			Payload:        []byte(`{"hello":"world"}`),
		}.Build())
	}

	attrs, err := json.Marshal(map[string]string{"event_type": "audit_log.asset_event_v1"})
	require.NoError(t, err)

	row, err := testrepo.New(conn).SeedPublishOutboxRow(t.Context(), testrepo.SeedPublishOutboxRowParams{
		PublicID:       uuid.NullUUID{},
		OrganizationID: orgID,
		Topic:          opts.topic,
		Message:        opts.message,
		Attributes:     attrs,
		Attempts:       opts.attempts,
		RetryAfter:     toTimestamptz(opts.retryAfter),
		LockedUntil:    toTimestamptz(opts.lockedUntil),
	})
	require.NoError(t, err)

	return row
}

func toTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *t, InfinityModifier: pgtype.Finite, Valid: true}
}

func mustMarshal(t *testing.T, msg proto.Message) []byte {
	t.Helper()

	bs, err := proto.Marshal(msg)
	require.NoError(t, err)

	return bs
}

func countRows(t *testing.T, conn *pgxpool.Pool) int64 {
	t.Helper()

	n, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)

	return n
}
