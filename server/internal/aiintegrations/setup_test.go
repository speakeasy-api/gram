package aiintegrations

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newStoreTestDB(t *testing.T) (context.Context, *pgxpool.Pool, *Store, string) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "aiintegrationstestdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "AI Integrations Test",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: orgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	_, err = projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "AI Integrations Test Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	store := NewStore(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t))
	return ctx, conn, store, orgID
}

// newTestChatOTELMirror builds a mirror over a noop publisher for tests that
// exercise imports without asserting on the OTEL dual-write.
func newTestChatOTELMirror(t *testing.T) *ChatOTELMirror {
	t.Helper()
	return NewChatOTELMirror(testenv.NewLogger(t), gcp.NewNoopPublisher[*otelv1.InboundLogRecord]())
}

// captureOTELLogPublisher records every published record so tests can assert
// on the mirror's exact output.
type captureOTELLogPublisher struct {
	mu   sync.Mutex
	sent []*otelv1.InboundLogRecord
}

var _ gcp.Publisher[*otelv1.InboundLogRecord] = (*captureOTELLogPublisher)(nil)

func (p *captureOTELLogPublisher) Publish(_ context.Context, msg *otelv1.InboundLogRecord) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, msg)
	return gcp.NewSuccessPublishResult()
}

func (p *captureOTELLogPublisher) Stop(context.Context) error { return nil }

func (p *captureOTELLogPublisher) Sent() []*otelv1.InboundLogRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*otelv1.InboundLogRecord, len(p.sent))
	copy(out, p.sent)
	return out
}
