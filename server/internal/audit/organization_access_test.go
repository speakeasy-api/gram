package audit_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestOrganizationAccessOutboxMasksStaffIdentity(t *testing.T) {
	t.Parallel()
	for _, surface := range []audit.Surface{audit.SurfaceAdmin, audit.SurfaceAdminMCP} {
		for _, action := range []audit.Action{audit.ActionOrganizationEnabled, audit.ActionOrganizationDisabled, audit.ActionOrganizationWhitelistUpdated} {
			t.Run(string(surface)+"/"+string(action), func(t *testing.T) {
				t.Parallel()
				ctx := contextvalues.SetOAuthClientID(contextvalues.SetActingSurface(t.Context(), string(surface)), "private-staff-client")
				conn, err := infra.CloneTestDatabase(t, "testdb")
				require.NoError(t, err)
				orgID := uuid.NewString()
				_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Synthetic Org", Slug: "synthetic-" + orgID[:8]})
				require.NoError(t, err)
				name, slug := "Private Staff", "private-staff"
				disabledAt := time.Now().UTC()
				before := &audit.OrganizationAccessSnapshot{DisabledAt: &disabledAt}
				after := &audit.OrganizationAccessSnapshot{DisabledAt: nil}
				if action == audit.ActionOrganizationDisabled {
					before, after = after, before
				}
				event := audit.LogOrganizationAccessEvent{OrganizationID: orgID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "private-staff-subject"), ActorDisplayName: &name, ActorSlug: &slug, OrganizationName: "Synthetic Org", OrganizationSlug: "synthetic-" + orgID[:8], OrganizationSnapshotBefore: before, OrganizationSnapshotAfter: after}
				logger := audit.NewLogger()
				switch action {
				case audit.ActionOrganizationWhitelistUpdated:
					err = logger.LogOrganizationWhitelistUpdated(ctx, conn, audit.LogOrganizationWhitelistUpdatedEvent{
						OrganizationID: orgID, Actor: event.Actor, ActorDisplayName: &name,
						OrganizationName: event.OrganizationName, OrganizationSlug: event.OrganizationSlug,
						OrganizationSnapshotBefore: &audit.OrganizationWhitelistSnapshot{Whitelisted: false},
						OrganizationSnapshotAfter:  &audit.OrganizationWhitelistSnapshot{Whitelisted: true},
					})
				case audit.ActionOrganizationEnabled:
					err = logger.LogOrganizationEnabled(ctx, conn, event)
				default:
					err = logger.LogOrganizationDisabled(ctx, conn, event)
				}
				require.NoError(t, err)
				row, err := audittest.LatestAuditLogByAction(ctx, conn, action)
				require.NoError(t, err)
				require.Equal(t, name, row.ActorDisplay)
				if action == audit.ActionOrganizationWhitelistUpdated {
					require.Empty(t, row.ActorSlug)
				} else {
					require.Equal(t, slug, row.ActorSlug)
				}
				envelope, err := auditrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, auditrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: orgID, EventType: string(events.OrganizationAccessV1.EventType())})
				require.NoError(t, err)
				var transport webhooksv1.Event
				require.NoError(t, proto.Unmarshal(envelope, &transport))
				var payload events.AuditLogCreatedPayloadV1
				require.NoError(t, json.Unmarshal(transport.GetPayload(), &payload))
				require.Equal(t, string(action), payload.Action)
				require.Empty(t, payload.ActorID)
				require.Empty(t, payload.ActorSlug)
				require.Empty(t, payload.ActingClientID)
				require.Equal(t, audit.SpeakeasyTeamActorLabel, payload.ActorDisplayName)
				require.NotContains(t, string(transport.GetPayload()), "private-staff")
				if action == audit.ActionOrganizationWhitelistUpdated {
					require.JSONEq(t, `{"whitelisted":true}`, string(payload.AfterSnapshot))
				} else {
					var snapshot audit.OrganizationAccessSnapshot
					require.NoError(t, json.Unmarshal(payload.AfterSnapshot, &snapshot))
					require.Equal(t, action == audit.ActionOrganizationDisabled, snapshot.DisabledAt != nil)
				}
			})
		}
	}
}
