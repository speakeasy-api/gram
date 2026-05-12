package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLogger_WritesAuditLogAndOutboxEntry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := uuid.New().String()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        "test-org-" + orgID[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	logger := audit.NewLogger()
	displayName := "Test User"
	slug := "test-user"

	auditCountBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)
	outboxCountBefore, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(outbox.EventTypeAuditLogCreated))
	require.NoError(t, err)

	err = logger.LogOrganizationWebhooksToggled(ctx, conn, audit.LogOrganizationWebhooksToggledEvent{
		OrganizationID:   orgID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user_test01"),
		ActorDisplayName: &displayName,
		ActorSlug:        &slug,
		OrganizationName: "Test Org",
		OrganizationSlug: "test-org-" + orgID[:8],
		WebhooksEnabled:  true,
	})
	require.NoError(t, err)

	auditCountAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationWebhooksEnabled)
	require.NoError(t, err)
	require.Equal(t, auditCountBefore+1, auditCountAfter)

	outboxCountAfter, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(outbox.EventTypeAuditLogCreated))
	require.NoError(t, err)
	require.Equal(t, outboxCountBefore+1, outboxCountAfter)
}
