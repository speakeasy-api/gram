package activities_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newOrgHandlerTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
}

func mustMarshalEventData(t *testing.T, payload any) json.RawMessage {
	t.Helper()

	bs, err := json.Marshal(payload)
	require.NoError(t, err)
	return bs
}

func TestHandleWorkOSOrganizationEvent_CreatedLinksByExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_created_external")
	logger := testenv.NewLogger(t)

	gramOrgID := uuid.NewString()
	const workosOrgID = "org_01HZTESTCREATED"

	event := events.Event{
		ID:        "event_01HZCREATE",
		Event:     string(workos.EventKindOrganizationCreated),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":          workosOrgID,
			"object":      "organization",
			"name":        "Acme Inc",
			"external_id": gramOrgID,
		}),
	}

	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, event))

	gotID, err := orgrepo.New(conn).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, gramOrgID, gotID)

	meta, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, gramOrgID)
	require.NoError(t, err)
	require.Equal(t, "Acme Inc", meta.Name)
	require.True(t, meta.WorkosID.Valid)
	require.Equal(t, workosOrgID, meta.WorkosID.String)
}

func TestHandleWorkOSOrganizationEvent_UpdatedReusesExistingMapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_updated_existing")
	logger := testenv.NewLogger(t)

	gramOrgID := uuid.NewString()
	const workosOrgID = "org_01HZTESTUPDATE"

	create := events.Event{
		ID:        "event_01HZCREATE",
		Event:     string(workos.EventKindOrganizationCreated),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":          workosOrgID,
			"object":      "organization",
			"name":        "Acme Inc",
			"external_id": gramOrgID,
		}),
	}
	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, create))

	update := events.Event{
		ID:        "event_01HZUPDATE",
		Event:     string(workos.EventKindOrganizationUpdated),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":     workosOrgID,
			"object": "organization",
			"name":   "Acme Renamed",
			// external_id intentionally omitted to prove we use the existing mapping
		}),
	}
	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, update))

	meta, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, gramOrgID)
	require.NoError(t, err)
	require.Equal(t, "Acme Renamed", meta.Name)
}

func TestHandleWorkOSOrganizationEvent_CreatedRequiresExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_created_missing_ext")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTNOEXT"

	event := events.Event{
		ID:        "event_01HZNOEXT",
		Event:     string(workos.EventKindOrganizationCreated),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":     workosOrgID,
			"object": "organization",
			"name":   "Orphan Org",
		}),
	}

	err := activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, event)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no external_id")
}

func TestHandleWorkOSOrganizationEvent_DeletedDisablesOrg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_deleted")
	logger := testenv.NewLogger(t)

	gramOrgID := uuid.NewString()
	const workosOrgID = "org_01HZTESTDELETE"

	create := events.Event{
		ID:        "event_01HZCREATEDEL",
		Event:     string(workos.EventKindOrganizationCreated),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":          workosOrgID,
			"object":      "organization",
			"name":        "To Be Deleted",
			"external_id": gramOrgID,
		}),
	}
	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, create))

	del := events.Event{
		ID:        "event_01HZDEL",
		Event:     string(workos.EventKindOrganizationDeleted),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":     workosOrgID,
			"object": "organization",
			"name":   "To Be Deleted",
		}),
	}
	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, del))

	meta, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, gramOrgID)
	require.NoError(t, err)
	require.True(t, meta.DisabledAt.Valid)
}

func TestHandleWorkOSOrganizationEvent_DeletedUnknownOrgIsNoop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_deleted_unknown")
	logger := testenv.NewLogger(t)

	event := events.Event{
		ID:        "event_01HZDELUNKNOWN",
		Event:     string(workos.EventKindOrganizationDeleted),
		CreatedAt: time.Now().UTC(),
		Data: mustMarshalEventData(t, map[string]string{
			"id":     "org_01HZUNKNOWN",
			"object": "organization",
			"name":   "Never Synced",
		}),
	}

	require.NoError(t, activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, event))
}

func TestHandleWorkOSOrganizationEvent_UnsupportedEventErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgHandlerTestConn(t, "workos_org_unsupported")
	logger := testenv.NewLogger(t)

	event := events.Event{
		ID:        "event_01HZUNSUPPORTED",
		Event:     "organization_membership.created",
		CreatedAt: time.Now().UTC(),
		Data:      mustMarshalEventData(t, map[string]string{"id": "om_1"}),
	}

	err := activities.HandleWorkOSOrganizationEvent(ctx, logger, conn, event)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unhandled workos organization event")
}
