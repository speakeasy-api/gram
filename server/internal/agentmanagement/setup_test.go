package agentmanagement

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
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

func newTestService(conn *pgxpool.Pool, engine authorizationEngine) *Service {
	return &Service{
		logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		db:         conn,
		authorizer: NewAuthorizer(engine),
		audit:      audit.NewLogger(),
	}
}
