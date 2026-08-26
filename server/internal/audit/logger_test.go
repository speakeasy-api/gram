package audit_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
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

	envelope, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: orgID,
		EventType:      string(events.AssetV1.EventType()),
	})
	require.NoError(t, err)

	// The outbox stores the marshaled transport envelope; the customer-facing
	// JSON payload is inside it.
	var event webhooksv1.Event
	require.NoError(t, proto.Unmarshal(envelope, &event))
	payload := event.GetPayload()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	// metadata must be a JSON object inlined into the payload, not a base64-encoded string.
	_, ok := decoded["metadata"].(map[string]any)
	require.True(t, ok, "metadata should be a JSON object, not a base64 string; payload=%s", string(payload))
}

func TestLogger_OutboxActorDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		actingSurface      string
		wantWebhookDisplay string
	}{
		{
			name:               "admin surface masks the actor display name",
			actingSurface:      string(audit.SurfaceAdmin),
			wantWebhookDisplay: audit.SpeakeasyTeamActorLabel,
		},
		{
			name:               "non-admin surface preserves the actor display name",
			actingSurface:      string(audit.SurfaceDashboard),
			wantWebhookDisplay: "Private Actor Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := contextvalues.SetActingSurface(t.Context(), tt.actingSurface)
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

			displayName := "Private Actor Name"
			slug := "private-actor"
			err = audit.NewLogger().LogOrganizationWebhooksToggled(ctx, conn, audit.LogOrganizationWebhooksToggledEvent{
				OrganizationID:   orgID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user_test01"),
				ActorDisplayName: &displayName,
				ActorSlug:        &slug,
				OrganizationName: "Test Org",
				OrganizationSlug: "test-org-" + orgID[:8],
				WebhooksEnabled:  true,
			})
			require.NoError(t, err)

			record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationWebhooksEnabled)
			require.NoError(t, err)
			require.Equal(t, displayName, record.ActorDisplay)
			require.Equal(t, slug, record.ActorSlug)

			envelope, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{
				OrganizationID: orgID,
				EventType:      string(events.OrganizationWebhooksV1.EventType()),
			})
			require.NoError(t, err)

			var event webhooksv1.Event
			require.NoError(t, proto.Unmarshal(envelope, &event))
			var payload events.AuditLogCreatedPayloadV1
			require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
			require.Equal(t, tt.wantWebhookDisplay, payload.ActorDisplayName)
		})
	}
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
	outboxCountBefore, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationWebhooksV1.EventType()))
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

	outboxCountAfter, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationWebhooksV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxCountBefore+1, outboxCountAfter)
}
