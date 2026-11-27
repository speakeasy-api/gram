package activities_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgcap"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// mirroredCapEventTime is the timestamp carried by the membership events these
// tests deliver. It is later than the seeded organization rows.
var mirroredCapEventTime = time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

// deliverMembershipCreated runs the organization event sync for one
// organization with a single organization_membership.created event.
func deliverMembershipCreated(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosOrgID, workosUserID, suffix string) error {
	t.Helper()

	stub := newWorkOSClientWithEvents([][]events.Event{{
		newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZCAP"+suffix, "mem_01HZCAP"+suffix, workosOrgID, workosUserID, mirroredCapEventTime, "admin"),
	}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	if err != nil {
		return fmt.Errorf("process organization events for %s: %w", workosOrgID, err)
	}
	return nil
}

func organizationDisabled(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) bool {
	t.Helper()

	org, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	return org.DisabledAt.Valid
}

func TestMirroredOrganizationCap_SecondOrganizationIsDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_second_disabled")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_first", "org_01HZCAPFIRST")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_second", "org_01HZCAPSECOND")
	seedWorkOSUser(t, ctx, conn, "user_cap", "user_01HZCAP")

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationDisabledByMembershipCap)
	require.NoError(t, err)

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPFIRST", "user_01HZCAP", "A"))
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_first"), "a user's first organization stays enabled")

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPSECOND", "user_01HZCAP", "B"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_second"), "a second mirrored organization is disabled")
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_first"), "the first organization is untouched")

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationDisabledByMembershipCap)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionOrganizationDisabledByMembershipCap, "gram_org_cap_second")
	require.NoError(t, err)
	require.Equal(t, "gram_org_cap_second", entry.OrganizationID)
}

func TestMirroredOrganizationCap_UserWithoutSpeakeasyAccountIsCappedByIdentityProviderID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_unlinked_user")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_unlinked_first", "org_01HZCAPUNLINKEDA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_unlinked_second", "org_01HZCAPUNLINKEDB")

	// No users row: the member has never signed in to Speakeasy.
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPUNLINKEDA", "user_01HZCAPUNLINKED", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPUNLINKEDB", "user_01HZCAPUNLINKED", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_unlinked_first"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_unlinked_second"))
}

func TestMirroredOrganizationCap_MembershipAlreadyRecordedIsNotChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_known_membership")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_known_first", "org_01HZCAPKNOWNA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_known_second", "org_01HZCAPKNOWNB")
	seedWorkOSUser(t, ctx, conn, "user_cap_known", "user_01HZCAPKNOWN")

	// The user already belongs to both organizations, as a user with two
	// long-standing organizations does. A replayed membership event for one of
	// them must not disable it.
	for _, organizationID := range []string{"gram_org_cap_known_first", "gram_org_cap_known_second"} {
		_, err := orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: organizationID,
			UserID:         conv.ToPGText("user_cap_known"),
		})
		require.NoError(t, err)
	}

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPKNOWNB", "user_01HZCAPKNOWN", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_known_first"))
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_known_second"))
}

func TestMirroredOrganizationCap_OrganizationWithAnotherMemberIsNotDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_other_member")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_own", "org_01HZCAPOWN")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_team", "org_01HZCAPTEAM")
	seedWorkOSUser(t, ctx, conn, "user_cap_joiner", "user_01HZCAPJOINER")
	seedWorkOSUser(t, ctx, conn, "user_cap_teammate", "user_01HZCAPTEAMMATE")

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPOWN", "user_01HZCAPJOINER", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPTEAM", "user_01HZCAPTEAMMATE", "B"))

	// Joining a team that already has a member is an invitation, not sprawl.
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPTEAM", "user_01HZCAPJOINER", "C"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_team"))
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_own"))
}

func TestMirroredOrganizationCap_OrganizationCreatedBySpeakeasyFlowIsNotDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_recorded_source")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_source_first", "org_01HZCAPSOURCEA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_source_prospect", "org_01HZCAPSOURCEB")
	seedWorkOSUser(t, ctx, conn, "user_cap_source", "user_01HZCAPSOURCE")

	// A staff-created prospect organization, which its first customer member
	// joins while already holding a self-serve organization.
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             "gram_org_cap_source_prospect",
		Name:           "Test Org",
		Slug:           "gram_org_cap_source_prospect",
		WorkosID:       conv.ToPGText("org_01HZCAPSOURCEB"),
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: conv.ToPGText(orgprovision.SourcePlatformAdmin),
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPSOURCEA", "user_01HZCAPSOURCE", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPSOURCEB", "user_01HZCAPSOURCE", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_source_prospect"))
}

func TestMirroredOrganizationCap_OrganizationWithVerifiedDomainIsNotDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_verified_domain")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_domain_first", "org_01HZCAPDOMAINA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_domain_company", "org_01HZCAPDOMAINB")
	seedWorkOSUser(t, ctx, conn, "user_cap_domain", "user_01HZCAPDOMAIN")
	require.NoError(t, orgrepo.New(conn).SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{
		ID:              "gram_org_cap_domain_company",
		VerifiedDomains: []string{"company.example"},
	}))

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPDOMAINA", "user_01HZCAPDOMAIN", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPDOMAINB", "user_01HZCAPDOMAIN", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_domain_company"))
}

func TestMirroredOrganizationCap_StaffUserIsNotCapped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_staff")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_staff_first", "org_01HZCAPSTAFFA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_staff_second", "org_01HZCAPSTAFFB")
	seedWorkOSUser(t, ctx, conn, "user_cap_staff", "user_01HZCAPSTAFF")
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user_cap_staff",
		Email:       "user_cap_staff@example.com",
		DisplayName: "Test User",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       true,
	})
	require.NoError(t, err)

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPSTAFFA", "user_01HZCAPSTAFF", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPSTAFFB", "user_01HZCAPSTAFF", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_staff_first"))
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_staff_second"))
}

// joinInTransaction records a new membership and applies the cap inside tx, the
// way the event sync does, and reports whether the organization was disabled.
func joinInTransaction(ctx context.Context, tx pgx.Tx, organizationID, userID, workosUserID, membershipID string) (bool, error) {
	if err := orgrepo.New(tx).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(mirroredCapEventTime),
		WorkosLastEventID:  conv.ToPGText("event_" + membershipID),
	}); err != nil {
		return false, fmt.Errorf("record membership: %w", err)
	}
	capped, err := orgcap.Enforce(ctx, tx, audit.NewLogger(), orgcap.NewMembership{
		OrganizationID: organizationID,
		UserID:         userID,
		WorkOSUserID:   workosUserID,
	})
	if err != nil {
		return false, fmt.Errorf("enforce cap: %w", err)
	}
	return capped, nil
}

func TestMirroredOrganizationCap_OverlappingTransactionsDecideOneAfterAnother(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_overlapping_transactions")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_overlap_first", "org_01HZCAPOVERLAPA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_overlap_second", "org_01HZCAPOVERLAPB")
	seedWorkOSUser(t, ctx, conn, "user_cap_overlap", "user_01HZCAPOVERLAP")

	// The first transaction joins and decides, then stays open. Its membership
	// is invisible to everyone else until it commits.
	first := testenv.BeginTx(t, ctx, conn)
	capped, err := joinInTransaction(ctx, first, "gram_org_cap_overlap_first", "user_cap_overlap", "user_01HZCAPOVERLAP", "mem_01HZCAPOVERLAPA")
	require.NoError(t, err)
	require.False(t, capped)

	type outcome struct {
		capped bool
		err    error
	}
	second := make(chan outcome, 1)
	go func() {
		var result outcome
		result.err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			capped, err := joinInTransaction(ctx, tx, "gram_org_cap_overlap_second", "user_cap_overlap", "user_01HZCAPOVERLAP", "mem_01HZCAPOVERLAPB")
			result.capped = capped
			return err
		})
		second <- result
	}()

	// The second transaction must wait for the first. Deciding now, it would
	// see no other organization for the user and keep its own as well.
	testenv.WaitForBackendsBlockedBy(t, ctx, conn, testenv.BackendPID(first), 1)
	require.NoError(t, first.Commit(ctx))

	result := <-second
	require.NoError(t, result.err)
	require.True(t, result.capped, "the transaction that decides second sees the first organization")
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_overlap_first"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_overlap_second"))
}

// burstOrganizations is how many organizations the parallel test delivers for
// one user at once, roughly the size of the burst this cap was written for.
const burstOrganizations = 12

func TestMirroredOrganizationCap_ParallelBurstKeepsExactlyOneOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_parallel_burst")
	seedWorkOSUser(t, ctx, conn, "user_cap_burst", "user_01HZCAPBURST")

	organizationIDs := make([]string, burstOrganizations)
	workosOrgIDs := make([]string, burstOrganizations)
	for i := range burstOrganizations {
		organizationIDs[i] = fmt.Sprintf("gram_org_cap_burst_%02d", i)
		workosOrgIDs[i] = fmt.Sprintf("org_01HZCAPBURST%02d", i)
		seedWorkOSOrganization(t, ctx, conn, organizationIDs[i], workosOrgIDs[i])
	}

	// Each organization has its own event stream, so the sync handles them in
	// separate transactions at the same time.
	var group errgroup.Group
	for i := range burstOrganizations {
		group.Go(func() error {
			return deliverMembershipCreated(t, ctx, conn, workosOrgIDs[i], "user_01HZCAPBURST", fmt.Sprintf("%02d", i))
		})
	}
	require.NoError(t, group.Wait())

	enabled := 0
	for _, organizationID := range organizationIDs {
		if !organizationDisabled(t, ctx, conn, organizationID) {
			enabled++
		}
	}
	require.Equal(t, 1, enabled, "exactly one organization of the burst stays enabled")

	visible, err := orgrepo.New(conn).ListOrganizationsForUser(ctx, conv.ToPGText("user_cap_burst"))
	require.NoError(t, err)
	require.Len(t, visible, 1)
}
