package audit_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLogger_OutboxEntrySnapshotsAreInlineJSON(t *testing.T) {
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

	assetID, err := uuid.NewV7()
	require.NoError(t, err)

	logger := audit.NewLogger()
	err = logger.LogAssetCreate(ctx, conn, audit.LogAssetCreateEvent{
		OrganizationID: orgID,
		ProjectID:      uuid.NullUUID{},
		Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, "user_test01"),
		AssetURN:       urn.NewAsset(urn.AssetKindImage, assetID),
		AssetName:      "Test Asset",
	})
	require.NoError(t, err)

	payload, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: orgID,
		EventType:      string(outbox.EventTypeAuditLogCreated),
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	// metadata must be a JSON object inlined into the payload, not a base64-encoded string.
	_, ok := decoded["metadata"].(map[string]any)
	require.True(t, ok, "metadata should be a JSON object, not a base64 string; payload=%s", string(payload))
}

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
