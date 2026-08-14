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
	seeded := readOrgState(t, ctx, conn, "org_dis")

	before := time.Now().UTC()
	res, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_dis"})
	require.NoError(t, err)
	require.NotNil(t, res.DisabledAt, "the response must report the organization as disabled")

	after := readOrgState(t, ctx, conn, "org_dis")
	require.True(t, after.DisabledAt.Valid)
	stamped := after.DisabledAt.Time

	// disabled_at must be the moment of the action, not the row's creation time
	// and not an arbitrary offset from it. AGE-3187 counts organizations
	// disabled in a window off this column, so a stamp that is merely "close to
	// now" is not enough.
	//
	// The tight bounds compare two values written by the same statement, so
	// they stay inside the database clock. `before` comes from the test host
	// and the database runs in a container, and the two clocks can drift, so
	// the cross-clock bound below is deliberately left loose.
	require.True(t, stamped.After(seeded.CreatedAt.Time),
		"disabled_at must be stamped at the disable, not carried from created_at: created %s, stamped %s", seeded.CreatedAt.Time, stamped)
	require.WithinDuration(t, after.UpdatedAt.Time, stamped, time.Second,
		"disabled_at and updated_at are stamped by one statement, so they must agree: updated %s, stamped %s", after.UpdatedAt.Time, stamped)
	require.WithinDuration(t, before, stamped, time.Minute, "disabled_at must land near wall-clock now")

	// updated_at is rendered to the operator by both the list and the get
	// endpoints, so a write that left it behind is user-visible.
	require.True(t, after.UpdatedAt.Time.After(seeded.UpdatedAt.Time),
		"disable must move updated_at: was %s, now %s", seeded.UpdatedAt.Time, after.UpdatedAt.Time)

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
	seeded := readOrgState(t, ctx, conn, "org_en")

	res, err := svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_en"})
	require.NoError(t, err)
	require.Nil(t, res.DisabledAt, "the response must report the organization as active")

	after := readOrgState(t, ctx, conn, "org_en")
	require.False(t, after.DisabledAt.Valid)
	require.True(t, after.UpdatedAt.Time.After(seeded.UpdatedAt.Time),
		"enable must move updated_at: was %s, now %s", seeded.UpdatedAt.Time, after.UpdatedAt.Time)
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
	// disabled. Neither direction may move it, in either polarity.
	//
	// A boolean column has four mutations, not two, and seeding only
	// whitelisted organizations makes half of them unreachable: "disable must
	// not GRANT the whitelist" cannot fail if every organization under test is
	// already whitelisted. That half is the dangerous half. A disable that
	// silently set the flag would push a not-yet-approved organization straight
	// through the approval gate the moment an operator re-enabled it.
	//
	// So all four arms are seeded explicitly, and each asserts the column
	// rather than the response, which keeps the read-after-write path out of
	// the assertion.
	seededDisabledAt := time.Now().UTC().Add(-time.Hour)

	cases := []struct {
		id          string
		slug        string
		whitelisted bool
		enable      bool
	}{
		{id: "org_wl_dis_on", slug: "wl-dis-on", whitelisted: true},
		{id: "org_wl_dis_off", slug: "wl-dis-off", whitelisted: false},
		{id: "org_wl_en_on", slug: "wl-en-on", whitelisted: true, enable: true},
		{id: "org_wl_en_off", slug: "wl-en-off", whitelisted: false, enable: true},
	}

	for _, tc := range cases {
		fixture := orgFixture{id: tc.id, name: tc.id, slug: tc.slug, whitelisted: tc.whitelisted}
		action := "disable"
		if tc.enable {
			action = "enable"
			fixture.disabledAt = &seededDisabledAt
		}
		seedOrg(t, ctx, conn, fixture)

		var err error
		if tc.enable {
			_, err = svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: tc.id})
		} else {
			_, err = svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: tc.id})
		}
		require.NoError(t, err)

		state := readOrgState(t, ctx, conn, tc.id)
		require.Equal(t, tc.enable, !state.DisabledAt.Valid, "%s must have taken effect on %s, or the whitelist assertion proves nothing", action, tc.id)
		require.Equal(t, tc.whitelisted, state.Whitelisted, "%s must leave whitelisted at %v on %s", action, tc.whitelisted, tc.id)
	}
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

	// An empty id is rejected as a 400 at the HTTP boundary by MinLength(1) in
	// the design. That validation is generated into the request decoder, which
	// a direct service call does not run, so the service-level contract is
	// still not-found. Both layers matter: this one is what protects the
	// database if a future caller reaches the service another way.
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

	// This test pins the write side on its own: addressing a write by slug must
	// change nothing and must not report success.
	//
	// It used to be the only test that killed deleting the `rows == 0` guard in
	// both handlers, back when the read-after-write still resolved slugs: the
	// read would find the row the write had missed and return 200. Now that the
	// read is keyed on id alone it has the same predicate as the write, so
	// deleting the guard is behaviour-preserving and no test can kill it. The
	// guard stays because it makes the not-found contract a property of the
	// handler rather than a consequence of the read, which is what stops the
	// 200-on-an-untouched-row bug from returning if the read is ever widened
	// again.
	_, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "slug-only"})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_slug_only"))
}

func TestAdminOrganizationWrites_ReturnTheOrganizationWritten(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// id and slug are both bare TEXT, so nothing stops one organization's slug
	// from equalling another's id. Every admin write is keyed on id, so a
	// read-after-write that also resolved slugs could return the bystander:
	// the operator gets a 200 describing an organization that is still active
	// and concludes the disable failed.
	seedOrg(t, ctx, conn, orgFixture{id: "org_collide", name: "Target", slug: "target-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_shadow", name: "Shadow", slug: "org_collide", whitelisted: true})

	disabled, err := svc.DisableOrganization(ctx, &gen.DisableOrganizationPayload{ID: "org_collide"})
	require.NoError(t, err)
	require.Equal(t, "org_collide", disabled.ID, "the response must describe the organization that was written")
	require.NotNil(t, disabled.DisabledAt, "the response must show the write that just happened")
	require.Nil(t, readDisabledAt(t, ctx, conn, "org_shadow"), "the write must not land on the organization whose slug collides")

	enabled, err := svc.EnableOrganization(ctx, &gen.EnableOrganizationPayload{ID: "org_collide"})
	require.NoError(t, err)
	require.Equal(t, "org_collide", enabled.ID)
	require.Nil(t, enabled.DisabledAt)

	// The same helper serves UpdateOrganization, whose write has always been
	// id-only.
	whitelisted := false
	updated, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_collide", Whitelisted: &whitelisted})
	require.NoError(t, err)
	require.Equal(t, "org_collide", updated.ID)
	require.False(t, updated.Whitelisted)
	require.True(t, readOrgState(t, ctx, conn, "org_shadow").Whitelisted, "the update must not land on the organization whose slug collides")
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
