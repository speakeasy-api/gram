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
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLogger_Visibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		visibility   audit.Visibility
		wantStored   audit.Visibility
		wantInternal bool
	}{
		{name: "unset defaults to customer", visibility: "", wantStored: audit.VisibilityCustomer, wantInternal: false},
		{name: "customer", visibility: audit.VisibilityCustomer, wantStored: audit.VisibilityCustomer, wantInternal: false},
		{name: "internal", visibility: audit.VisibilityInternal, wantStored: audit.VisibilityInternal, wantInternal: true},
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
				Actor:                  urn.NewSystemPrincipal("kit-runtime"),
				ActorDisplayName:       nil,
				ActorSlug:              nil,
				AdmissionURN:           urn.NewWorkloadAdmission(uuid.New()),
				AdmissionDisplayName:   "system:serviceaccount:ns:kit-test",
				AdmissionSnapshotAfter: nil,
				Visibility:             tt.visibility,
			})
			require.NoError(t, err)

			record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
			require.NoError(t, err)
			require.Equal(t, tt.wantStored, record.Visibility)

			attributes, err := audittest.LatestOutboxAttributes(ctx, conn, orgID, string(events.WorkloadAdmissionV1.EventType()))
			require.NoError(t, err)
			require.Equal(t, tt.wantInternal, outbox.IsInternalEvent(attributes))
		})
	}
}

func TestLogger_RejectsUnknownVisibility(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	err = audit.NewLogger().LogWorkloadAdmissionAdmit(ctx, conn, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         uuid.New().String(),
		ProjectID:              uuid.NullUUID{},
		Actor:                  urn.NewSystemPrincipal("kit-runtime"),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(uuid.New()),
		AdmissionDisplayName:   "subject",
		AdmissionSnapshotAfter: nil,
		Visibility:             "public",
	})
	require.ErrorContains(t, err, "unknown audit visibility")
}
