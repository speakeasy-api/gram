package audit_test

import (
	"encoding/json"
	"github.com/google/uuid"
	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestAccountTypeAuditAtomicIntent(t *testing.T) {
	t.Parallel()
	for _, before := range []string{"free", "enterprise"} {
		t.Run(before, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			orgID := uuid.NewString()
			_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Synthetic Org", Slug: "synthetic-" + orgID[:8]})
			require.NoError(t, err)
			event := audit.LogOrganizationAccountTypeChangedEvent{OrganizationID: orgID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "staff_placeholder"), BeforeAccountType: before, AccountType: "enterprise"}
			tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction verifies audit and outbox rollback.
			require.NoError(t, err)
			require.NoError(t, audit.NewLogger().LogOrganizationAccountTypeChanged(ctx, tx, event))
			require.NoError(t, tx.Rollback(ctx))
			count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationAccountTypeChanged)
			require.NoError(t, err)
			require.Zero(t, count)
			_, err = auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: orgID, EventType: string(events.OrganizationAccountTypeV1.EventType())})
			require.Error(t, err)
			tx, err = conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction verifies audit and outbox commit.
			require.NoError(t, err)
			require.NoError(t, audit.NewLogger().LogOrganizationAccountTypeChanged(ctx, tx, event))
			require.NoError(t, tx.Commit(ctx))
			record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationAccountTypeChanged)
			require.NoError(t, err)
			require.JSONEq(t, `{"operation":"account_type_change"}`, string(record.Metadata))
			envelope, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: orgID, EventType: string(events.OrganizationAccountTypeV1.EventType())})
			require.NoError(t, err)
			var transport webhooksv1.Event
			require.NoError(t, proto.Unmarshal(envelope, &transport))
			require.NotEmpty(t, transport.GetEventId())
			var payload events.AuditLogCreatedPayloadV1
			require.NoError(t, json.Unmarshal(transport.GetPayload(), &payload))
			require.Equal(t, orgID, payload.SubjectID)
			require.Equal(t, "organization", payload.SubjectType)
			require.JSONEq(t, `{"account_type":"`+before+`"}`, string(payload.BeforeSnapshot))
			require.JSONEq(t, `{"account_type":"enterprise"}`, string(payload.AfterSnapshot))
		})
	}
}
