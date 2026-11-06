package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	writerrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestLogger_OrdinaryMcpServerEvents(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	actor := urn.NewSystemPrincipal("audit-test")
	server := urn.NewMcpServer(uuid.New())
	logger := audit.NewLogger()
	actions := []struct {
		name string
		log  func(*pgxpool.Pool, string) error
	}{
		{"Create", func(conn *pgxpool.Pool, orgID string) error {
			return logger.LogMcpServerCreate(ctx, conn, audit.LogMcpServerCreateEvent{OrganizationID: orgID, Actor: actor, McpServerURN: server})
		}},
		{"Update", func(conn *pgxpool.Pool, orgID string) error {
			return logger.LogMcpServerUpdate(ctx, conn, audit.LogMcpServerUpdateEvent{OrganizationID: orgID, Actor: actor, McpServerURN: server})
		}},
		{"ToolMetadataUpdate", func(conn *pgxpool.Pool, orgID string) error {
			return logger.LogMcpServerToolMetadataUpdate(ctx, conn, audit.LogMcpServerToolMetadataUpdateEvent{OrganizationID: orgID, Actor: actor, McpServerURN: server})
		}},
		{"Delete", func(conn *pgxpool.Pool, orgID string) error {
			return logger.LogMcpServerDelete(ctx, conn, audit.LogMcpServerDeleteEvent{OrganizationID: orgID, Actor: actor, McpServerURN: server})
		}},
		{"ScopePinUpdate", func(conn *pgxpool.Pool, orgID string) error {
			return logger.LogMcpServerScopePinUpdate(ctx, conn, audit.LogMcpServerScopePinUpdateEvent{OrganizationID: orgID, Actor: actor, McpServerURN: server})
		}},
	}
	for _, action := range actions {
		t.Run(action.name, func(t *testing.T) {
			t.Parallel()
			conn, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			orgID := uuid.NewString()
			_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
				ID: orgID, Name: "Test Org", Slug: "test-org-" + orgID[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
			})
			require.NoError(t, err)
			require.NoError(t, action.log(conn, orgID))
			rows, err := writerrepo.New(conn).ListAuditLogs(ctx, writerrepo.ListAuditLogsParams{OrganizationID: orgID, IncludeInternalEvents: true})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.False(t, rows[0].IsInternal)
			count, err := auditrepo.New(conn).CountPublishOutboxByOrgAndEvent(ctx, auditrepo.CountPublishOutboxByOrgAndEventParams{OrganizationID: orgID, EventType: string(events.McpServerV1.EventType())})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
