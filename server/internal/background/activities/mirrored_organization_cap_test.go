package activities_test

import (
	"context"
	"fmt"
	"log/slog"
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

var mirroredCapEventTime = time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

func deliverMembershipCreated(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosOrgID, workosUserID, suffix string) error {
	t.Helper()

	return deliverEvent(ctx, testenv.NewLogger(t), conn, workosOrgID, membershipCreatedEvent(t, workosOrgID, workosUserID, suffix))
}

func membershipCreatedEvent(t *testing.T, workosOrgID, workosUserID, suffix string) events.Event {
	t.Helper()

	return newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZCAP"+suffix, "mem_01HZCAP"+suffix, workosOrgID, workosUserID, mirroredCapEventTime, "admin")
}

func deliverEvent(ctx context.Context, logger *slog.Logger, conn *pgxpool.Pool, workosOrgID string, event events.Event) error {
	stub := newWorkOSClientWithEvents([][]events.Event{{event}})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub, cache.NoopCache, nil)
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
	require.Equal(t, "system", entry.ActorType)
	require.Equal(t, "organization-cap", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceSystem), *entry.ActingSurface)
	beforeSnapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Nil(t, beforeSnapshot["disabled_at"])
	afterSnapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.NotNil(t, afterSnapshot["disabled_at"])
}

func TestMirroredOrganizationCap_MembershipRecordedAtLoginBeforeTheEventIsChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_login_first")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_login_first", "org_01HZCAPLOGINA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_login_second", "org_01HZCAPLOGINB")
	seedWorkOSUser(t, ctx, conn, "user_cap_login", "user_01HZCAPLOGIN")
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPLOGINA", "user_01HZCAPLOGIN", "A"))

	_, err := orgrepo.New(conn).SetUserWorkOSMemberships(ctx, orgrepo.SetUserWorkOSMembershipsParams{
		UserID:              conv.ToPGText("user_cap_login"),
		PreserveExisting:    true,
		WorkosOrgIds:        []string{"org_01HZCAPLOGINB"},
		WorkosMembershipIds: []string{"mem_01HZCAPB"},
	})
	require.NoError(t, err)
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_login_second"))

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPLOGINB", "user_01HZCAPLOGIN", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_login_first"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_login_second"))
}

func TestMirroredOrganizationCap_UserWithoutSpeakeasyAccountIsCappedByIdentityProviderID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_unlinked_user")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_unlinked_first", "org_01HZCAPUNLINKEDA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_unlinked_second", "org_01HZCAPUNLINKEDB")

	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPUNLINKEDA", "user_01HZCAPUNLINKED", "A"))
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPUNLINKEDB", "user_01HZCAPUNLINKED", "B"))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_unlinked_first"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_unlinked_second"))
}

func TestMirroredOrganizationCap_UpdateForMembershipRecordedAtLoginIsNotChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_login_then_update")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_update_first", "org_01HZCAPUPDATEA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_update_team", "org_01HZCAPUPDATEB")
	seedWorkOSUser(t, ctx, conn, "user_cap_update", "user_01HZCAPUPDATE")
	require.NoError(t, deliverMembershipCreated(t, ctx, conn, "org_01HZCAPUPDATEA", "user_01HZCAPUPDATE", "A"))

	_, err := orgrepo.New(conn).SetUserWorkOSMemberships(ctx, orgrepo.SetUserWorkOSMembershipsParams{
		UserID:              conv.ToPGText("user_cap_update"),
		PreserveExisting:    true,
		WorkosOrgIds:        []string{"org_01HZCAPUPDATEB"},
		WorkosMembershipIds: []string{"mem_01HZCAPB"},
	})
	require.NoError(t, err)

	updated := newWorkOSMembershipEvent(t, "organization_membership.updated", "event_01HZCAPB", "mem_01HZCAPB", "org_01HZCAPUPDATEB", "user_01HZCAPUPDATE", mirroredCapEventTime, "member")
	require.NoError(t, deliverEvent(ctx, testenv.NewLogger(t), conn, "org_01HZCAPUPDATEB", updated))

	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_update_team"))
}

func TestMirroredOrganizationCap_MembershipAlreadyRecordedIsNotChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "mirrored_cap_known_membership")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_known_first", "org_01HZCAPKNOWNA")
	seedWorkOSOrganization(t, ctx, conn, "gram_org_cap_known_second", "org_01HZCAPKNOWNB")
	seedWorkOSUser(t, ctx, conn, "user_cap_known", "user_01HZCAPKNOWN")

	for _, organizationID := range []string{"gram_org_cap_known_first", "gram_org_cap_known_second"} {
		require.NoError(t, orgrepo.New(conn).BackdateOrganizationCreatedAtForTest(ctx, orgrepo.BackdateOrganizationCreatedAtForTestParams{
			ID:        organizationID,
			CreatedAt: conv.ToPGTimestamptz(time.Now().AddDate(0, -6, 0)),
		}))
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

	testenv.WaitForBackendsBlockedBy(t, ctx, conn, testenv.BackendPID(first), 1)
	require.NoError(t, first.Commit(ctx))

	result := <-second
	require.NoError(t, result.err)
	require.True(t, result.capped, "the transaction that decides second sees the first organization")
	require.False(t, organizationDisabled(t, ctx, conn, "gram_org_cap_overlap_first"))
	require.True(t, organizationDisabled(t, ctx, conn, "gram_org_cap_overlap_second"))
}

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

	logger := testenv.NewLogger(t)
	burstEvents := make([]events.Event, burstOrganizations)
	for i := range burstOrganizations {
		burstEvents[i] = membershipCreatedEvent(t, workosOrgIDs[i], "user_01HZCAPBURST", fmt.Sprintf("%02d", i))
	}
	var group errgroup.Group
	for i := range burstOrganizations {
		group.Go(func() error {
			return deliverEvent(ctx, logger, conn, workosOrgIDs[i], burstEvents[i])
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
