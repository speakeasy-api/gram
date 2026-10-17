package agentmanagement

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var cloneTestDatabase testenv.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, cloneFunc, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = cloneFunc
	code := m.Run()
	if err := container.Terminate(ctx); err != nil {
		log.Fatalf("terminate test postgres: %v", err)
	}
	os.Exit(code)
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := cloneTestDatabase(t, "agent_management")
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

func seedOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Test Organization", Slug: organizationID, WorkosID: conv.PtrToPGText(nil),
	})
	require.NoError(t, err)
}

func seedOrganizationUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID string) {
	t.Helper()
	_, err := usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID: userID, Email: userID + "@example.com", DisplayName: userID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func createAgent(t *testing.T, conn *pgxpool.Pool, organizationID, ownerUserID, name string) repo.Agent {
	t.Helper()
	agent, err := repo.New(conn).CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: ownerUserID, Name: name,
	})
	require.NoError(t, err)
	return agent
}

func agentWebhookOutboxActions(t *testing.T, conn *pgxpool.Pool, organizationID string) []string {
	t.Helper()

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)

	actions := make([]string, 0)
	for _, row := range rows {
		if row.OrganizationID != organizationID {
			continue
		}
		var attributes map[string]string
		require.NoError(t, json.Unmarshal(row.Attributes, &attributes))
		if attributes["event_type"] != string(events.AgentV1.EventType()) {
			continue
		}

		var event webhooksv1.Event
		require.NoError(t, proto.Unmarshal(row.Message, &event))
		var payload events.AuditLogCreatedPayloadV1
		require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
		require.Equal(t, organizationID, payload.OrganizationID)
		require.Equal(t, "agent", payload.SubjectType)
		actions = append(actions, payload.Action)
	}

	return actions
}

// auditFeedPageSize is the page size of the ListAuditLogs feed query (its
// LIMIT 51 in audit/queries.sql). A result this long may be truncated, so
// auditLogs treats it as a failure.
const auditFeedPageSize = 51

// auditLogs returns every audit row for one subject in an organization, oldest
// first. An empty subjectID lists the whole organization. A full feed page
// fails rather than silently truncating.
func auditLogs(t *testing.T, conn *pgxpool.Pool, organizationID, subjectID string) []auditrepo.ListAuditLogsRow {
	t.Helper()

	subject := conv.PtrToPGText(nil)
	if subjectID != "" {
		subject = conv.ToPGText(subjectID)
	}
	rows, err := auditrepo.New(conn).ListAuditLogs(t.Context(), auditrepo.ListAuditLogsParams{
		OrganizationID: organizationID, SubjectID: subject, IncludeAssistantEvents: true,
	})
	require.NoError(t, err)
	require.Less(t, len(rows), auditFeedPageSize, "audit rows exceed one feed page")
	slices.Reverse(rows)

	return rows
}

// auditActions returns the audit actions for one subject, oldest first.
func auditActions(t *testing.T, conn *pgxpool.Pool, organizationID, subjectID string) []string {
	t.Helper()

	rows := auditLogs(t, conn, organizationID, subjectID)
	actions := make([]string, 0, len(rows))
	for _, row := range rows {
		actions = append(actions, row.Action)
	}

	return actions
}

func newTestService(conn *pgxpool.Pool, engine authorizationEngine) *Service {
	return &Service{
		logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		db:         conn,
		authorizer: NewAuthorizer(engine),
		audit:      audit.NewLogger(),
	}
}

type activityQueryCounter struct{ count atomic.Int64 }

func (c *activityQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: BatchManagedAgentCredentialLastUsed") {
		c.count.Add(1)
	}
	return ctx
}
func (*activityQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func newActivityTracedTestDB(t *testing.T) (*pgxpool.Pool, *atomic.Int64) {
	t.Helper()
	base := newTestDB(t)
	config := base.Config()
	base.Close()
	tracer := new(activityQueryCounter)
	config.ConnConfig.Tracer = tracer
	conn, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn, &tracer.count
}
