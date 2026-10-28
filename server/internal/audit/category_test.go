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
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Every category is stored and delivered as a webhook carrying it, so a
// subscriber can tell configuration changes from security events.
func TestLogger_Category(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category audit.Category
		want     audit.Category
	}{
		{name: "unset defaults to configuration", category: "", want: audit.CategoryConfiguration},
		{name: "configuration", category: audit.CategoryConfiguration, want: audit.CategoryConfiguration},
		{name: "security", category: audit.CategorySecurity, want: audit.CategorySecurity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			err = audit.NewLogger().LogWorkloadAdmissionAdmit(ctx, conn, audit.LogWorkloadAdmissionAdmitEvent{
				OrganizationID:         orgID,
				ProjectID:              uuid.NullUUID{},
				Actor:                  urn.NewSystemPrincipal(audit.KitRuntimeActorID),
				ActorDisplayName:       nil,
				ActorSlug:              nil,
				AdmissionURN:           urn.NewWorkloadAdmission(uuid.New()),
				AdmissionDisplayName:   "system:serviceaccount:ns:kit-test",
				AdmissionSnapshotAfter: nil,
				Category:               tt.category,
			})
			require.NoError(t, err)

			record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
			require.NoError(t, err)
			require.Equal(t, tt.want, record.Category)

			envelope, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{
				OrganizationID: orgID,
				EventType:      string(events.WorkloadAdmissionV1.EventType()),
			})
			require.NoError(t, err)
			var event webhooksv1.Event
			require.NoError(t, proto.Unmarshal(envelope, &event))
			var payload events.AuditLogCreatedPayloadV1
			require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
			require.Equal(t, string(tt.want), payload.Category)
		})
	}
}

func TestLogger_RejectsUnknownCategory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	err = audit.NewLogger().LogWorkloadAdmissionAdmit(ctx, conn, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         uuid.New().String(),
		ProjectID:              uuid.NullUUID{},
		Actor:                  urn.NewSystemPrincipal(audit.KitRuntimeActorID),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(uuid.New()),
		AdmissionDisplayName:   "subject",
		AdmissionSnapshotAfter: nil,
		Category:               "internal",
	})
	require.ErrorContains(t, err, "unknown audit category")
}
