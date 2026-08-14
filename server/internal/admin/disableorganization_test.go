package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// readOrgState reads organization_metadata straight from the database rather
// than through the API type. disabled_at matters at full precision here: the
// API renders it as a second-resolution RFC3339 string, so a timestamp that
// moved by microseconds would still compare equal.
func readOrgState(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) testrepo.GetOrganizationMetadataStateFixtureRow {
	t.Helper()

	row, err := testrepo.New(conn).GetOrganizationMetadataStateFixture(ctx, orgID)
	require.NoError(t, err)
	return row
}

func readDisabledAt(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) *time.Time {
	t.Helper()

	disabledAt := readOrgState(t, ctx, conn, orgID).DisabledAt
	if !disabledAt.Valid {
		return nil
	}
	return &disabledAt.Time
}

func requireOopsCode(t *testing.T, err error, want oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, want, oopsErr.Code)
}

func TestDisableOrganization_SetsDisabledAt(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_dis", name: "Dis Co", slug: "dis-co", whitelisted: true})

	before := time.Now().UTC()
	res, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_dis"})
	require.NoError(t, err)
	require.NotNil(t, res.DisabledAt, "the response must report the organization as disabled")

	stamped := readDisabledAt(t, ctx, conn, "org_dis")
	require.NotNil(t, stamped)
	require.WithinDuration(t, before, *stamped, time.Minute, "disabled_at records the moment of the action")

	// The detail endpoint agrees with the write.
	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_dis"})
	require.NoError(t, err)
	require.NotNil(t, detail.DisabledAt)
}

func TestDisableOrganization_WithoutWorkosID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// The whole reason the operator path needs its own query: no WorkOS linkage
	// to key on.
	seedOrg(t, ctx, conn, orgFixture{id: "org_no_workos", name: "No Workos", slug: "no-workos", whitelisted: true, workosID: nil})

	res, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_no_workos"})
	require.NoError(t, err)
	require.Nil(t, res.WorkosID, "fixture must really have no WorkOS id")
	require.NotNil(t, res.DisabledAt)
	require.NotNil(t, readDisabledAt(t, ctx, conn, "org_no_workos"))
}

func TestDisableOrganization_AlreadyDisabledKeepsTimestamp(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_twice", name: "Twice Co", slug: "twice-co", whitelisted: true})

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_twice"})
	require.NoError(t, err)
	first := readDisabledAt(t, ctx, conn, "org_twice")
	require.NotNil(t, first)

	_, err = svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_twice"})
	require.NoError(t, err)
	second := readDisabledAt(t, ctx, conn, "org_twice")
	require.NotNil(t, second)

	require.True(t, first.Equal(*second), "re-disabling must not move disabled_at: %s then %s", first, second)
}

func TestEnableOrganization_ClearsDisabledAt(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	disabledAt := time.Now().UTC().Add(-24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_en", name: "En Co", slug: "en-co", whitelisted: true, disabledAt: &disabledAt})

	res, err := svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_en"})
	require.NoError(t, err)
	require.Nil(t, res.DisabledAt, "the response must report the organization as active")
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_en"))
}

func TestEnableOrganization_NeverDisabled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_already_on", name: "On Co", slug: "on-co", whitelisted: true})

	res, err := svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_already_on"})
	require.NoError(t, err)
	require.Nil(t, res.DisabledAt)
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_already_on"))
}

func TestDisableThenEnableOrganization_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_round", name: "Round Co", slug: "round-co", whitelisted: true})

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_round"})
	require.NoError(t, err)
	require.NotNil(t, readDisabledAt(t, ctx, conn, "org_round"))

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_round"})
	require.NoError(t, err)
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_round"))

	// A second disable after a re-enable stamps a fresh timestamp rather than
	// resurrecting the first one.
	_, err = svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_round"})
	require.NoError(t, err)
	require.NotNil(t, readDisabledAt(t, ctx, conn, "org_round"))
}

func TestDisableOrganization_LeavesWorkosCursorAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	workosID := "workos_org_placeholder"
	cursor := "event_placeholder"
	seedOrg(t, ctx, conn, orgFixture{
		id:                "org_cursor",
		name:              "Cursor Co",
		slug:              "cursor-co",
		whitelisted:       true,
		workosID:          &workosID,
		workosLastEventID: &cursor,
	})
	seeded := readOrgState(t, ctx, conn, "org_cursor").WorkosLastEventID
	require.True(t, seeded.Valid, "the cursor must really be set, or the assertions below prove nothing")
	require.Equal(t, cursor, seeded.String)

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_cursor"})
	require.NoError(t, err)
	got := readOrgState(t, ctx, conn, "org_cursor").WorkosLastEventID
	require.True(t, got.Valid, "disable must not clear the webhook cursor")
	require.Equal(t, cursor, got.String)

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_cursor"})
	require.NoError(t, err)
	got = readOrgState(t, ctx, conn, "org_cursor").WorkosLastEventID
	require.True(t, got.Valid, "enable must not clear the webhook cursor")
	require.Equal(t, cursor, got.String)
}

func TestDisableOrganization_LeavesWhitelistAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// Whitelisting is the not-yet-approved gate, a separate concept from being
	// disabled. Neither direction may move it.
	seedOrg(t, ctx, conn, orgFixture{id: "org_wl_on", name: "WL On", slug: "wl-on", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_wl_off", name: "WL Off", slug: "wl-off", whitelisted: false})

	disabled, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_wl_on"})
	require.NoError(t, err)
	require.True(t, disabled.Whitelisted, "disabling must not revoke the whitelist")

	enabled, err := svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_wl_off"})
	require.NoError(t, err)
	require.False(t, enabled.Whitelisted, "enabling must not grant the whitelist")
}

func TestDisableOrganization_AgreesWithListGate(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_gate", name: "Gate Co", slug: "gate-co", whitelisted: true})

	include := true
	listIDs := func(payload *gen.ListOrganizationsPayload) []string {
		res, err := svc.ListOrganizations(ctx, payload)
		require.NoError(t, err)
		ids := make([]string, 0, len(res.Organizations))
		for _, o := range res.Organizations {
			ids = append(ids, o.ID)
		}
		return ids
	}

	require.Contains(t, listIDs(&gen.ListOrganizationsPayload{}), "org_gate")

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_gate"})
	require.NoError(t, err)
	require.NotContains(t, listIDs(&gen.ListOrganizationsPayload{}), "org_gate", "a disabled organization drops out of the default list")
	require.Contains(t, listIDs(&gen.ListOrganizationsPayload{IncludeDisabled: &include}), "org_gate")

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_gate"})
	require.NoError(t, err)
	require.Contains(t, listIDs(&gen.ListOrganizationsPayload{}), "org_gate", "a re-enabled organization comes back to the default list")
}

func TestDisableOrganization_UnknownID(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_does_not_exist"})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_does_not_exist"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDisableOrganization_EmptyID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bystander", name: "Bystander", slug: "bystander", whitelisted: true})

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: ""})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: ""})
	requireOopsCode(t, err, oops.CodeNotFound)

	require.Nil(t, readDisabledAt(t, ctx, conn, "org_bystander"), "an unmatched id must not disable anything")
}

func TestDisableOrganization_DoesNotMatchOnSlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_slug_only", name: "Slug Only", slug: "slug-only", whitelisted: true})

	// The read path resolves id-or-slug; the write path is id-only. Passing a
	// slug must fail rather than report a success the database never saw.
	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "slug-only"})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_slug_only"))
}

func TestDisableOrganization_TouchesOnlyTheTargetRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_target", name: "Target", slug: "target", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_neighbour", name: "Neighbour", slug: "neighbour", whitelisted: true})

	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_target"})
	require.NoError(t, err)
	require.NotNil(t, readDisabledAt(t, ctx, conn, "org_target"))
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_neighbour"), "disable must not spill onto other organizations")

	disabledAt := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_stays_off", name: "Stays Off", slug: "stays-off", whitelisted: true, disabledAt: &disabledAt})

	_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_target"})
	require.NoError(t, err)
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_target"))
	require.NotNil(t, readDisabledAt(t, ctx, conn, "org_stays_off"), "enable must not spill onto other organizations")
}
