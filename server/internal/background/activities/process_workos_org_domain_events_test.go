package activities_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// seededDomainEventID is the cursor stored on organizations created by
// createWorkOSLinkedOrg. Test events use larger IDs so the cursor guard
// accepts them.
const seededDomainEventID = "event_00"

func createWorkOSLinkedOrg(t *testing.T, conn *pgxpool.Pool, id, workosOrgID string, verifiedDomains []string) {
	t.Helper()

	_, err := orgrepo.New(conn).CreateOrganizationMetadataFromWorkOS(t.Context(), orgrepo.CreateOrganizationMetadataFromWorkOSParams{
		ID:                id,
		Name:              "Domains",
		Slug:              id,
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)),
		WorkosLastEventID: conv.ToPGText(seededDomainEventID),
		VerifiedDomains:   verifiedDomains,
	})
	require.NoError(t, err)
}

// flatDomainPayload builds an organization_domain.* payload that carries the
// Organization Domain object directly in data.
func flatDomainPayload(workosOrgID, domain string) []byte {
	return []byte(`{"object":"organization_domain","id":"org_domain_x","organization_id":"` + workosOrgID +
		`","domain":"` + domain + `","state":"verified"}`)
}

// nestedDomainPayload builds an organization_domain.* payload that nests the
// Organization Domain object under data.organization_domain.
func nestedDomainPayload(workosOrgID, domain string) []byte {
	return []byte(`{"organization_domain":{"object":"organization_domain","id":"org_domain_x","organization_id":"` + workosOrgID +
		`","domain":"` + domain + `","state":"verified"},"reason":""}`)
}

func domainVerifiedEvent(id string, data []byte) events.Event {
	return events.Event{ID: id, Event: "organization_domain.verified", CreatedAt: time.Now(), Data: data}
}

func domainDeletedEvent(id string, data []byte) events.Event {
	return events.Event{ID: id, Event: "organization_domain.deleted", CreatedAt: time.Now(), Data: data}
}

func organizationUpdatedEvent(id, workosOrgID, organizationID, domainsJSON string) events.Event {
	return organizationEvent("organization.updated", id, workosOrgID, organizationID, domainsJSON)
}

func organizationCreatedEvent(id, workosOrgID, organizationID, domainsJSON string) events.Event {
	return organizationEvent("organization.created", id, workosOrgID, organizationID, domainsJSON)
}

// organizationEvent builds an organization.* event whose payload lists
// domainsJSON, a comma-separated list of Organization Domain objects.
func organizationEvent(kind, id, workosOrgID, organizationID, domainsJSON string) events.Event {
	return events.Event{
		ID:        id,
		Event:     kind,
		CreatedAt: time.Now(),
		Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Domains","external_id":"` + organizationID +
			`","updated_at":"2026-05-06T12:00:00Z","domains":[` + domainsJSON + `]}`),
	}
}

// runOrgEvents processes one page of events for workosOrgID.
func runOrgEvents(t *testing.T, conn *pgxpool.Pool, workosOrgID string, page ...events.Event) {
	t.Helper()

	stub := newWorkOSClientWithEvents([][]events.Event{page})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(t.Context(), activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
}

func getOrgByWorkOSID(t *testing.T, conn *pgxpool.Pool, workosOrgID string) orgrepo.OrganizationMetadatum {
	t.Helper()

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(t.Context(), conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	return row
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedAppendsToNullList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_null")
	const workosOrgID = "org_01HZDOMNULL"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_null", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedAppendsToEmptyList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_empty")
	const workosOrgID = "org_01HZDOMEMPTY"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_empty", workosOrgID, []string{})

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedKeepsExistingDomains(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_existing")
	const workosOrgID = "org_01HZDOMEXISTING"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_existing", workosOrgID, []string{"a.example.com", "b.example.com"})

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "c.example.com")))

	require.Equal(t, []string{"a.example.com", "b.example.com", "c.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedDuplicateIsIdempotent(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_duplicate")
	const workosOrgID = "org_01HZDOMDUP"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_dup", workosOrgID, []string{"example.com", "other.example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")),
		domainVerifiedEvent("event_02", flatDomainPayload(workosOrgID, "example.com")),
	)

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"example.com", "other.example.com"}, row.VerifiedDomains)
	// The cursor still advances when the list does not change.
	require.Equal(t, "event_02", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedStoresLowercase(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_lowercase")
	const workosOrgID = "org_01HZDOMLOWER"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_lower", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "Example.COM")))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainVerifiedMatchesStoredDomainIgnoringCase(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_verified_case_dup")
	const workosOrgID = "org_01HZDOMCASEDUP"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_case_dup", workosOrgID, []string{"Example.com"})

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "EXAMPLE.com")))

	require.Equal(t, []string{"Example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventsIgnoreTrailingDot(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_trailing_dot")
	const workosOrgID = "org_01HZDOMDOT"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_dot", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "Example.com.")))
	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_02", flatDomainPayload(workosOrgID, "example.com.")))
	require.Empty(t, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainDeletedRemovesOnlyThatDomain(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_deleted_one")
	const workosOrgID = "org_01HZDOMDELONE"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_del_one", workosOrgID, []string{"a.example.com", "b.example.com", "c.example.com"})

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01", flatDomainPayload(workosOrgID, "b.example.com")))

	require.Equal(t, []string{"a.example.com", "c.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainDeletedIgnoresCase(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_deleted_case")
	const workosOrgID = "org_01HZDOMDELCASE"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_del_case", workosOrgID, []string{"Example.com", "other.example.com"})

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01", flatDomainPayload(workosOrgID, "EXAMPLE.COM")))

	require.Equal(t, []string{"other.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainDeletedAbsentDomainIsNoOp(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_deleted_absent")
	const workosOrgID = "org_01HZDOMDELABSENT"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_del_absent", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01", flatDomainPayload(workosOrgID, "missing.example.com")))

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"example.com"}, row.VerifiedDomains)
	require.Equal(t, "event_01", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DomainDeletedFromNullListLeavesEmptyList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_deleted_null")
	const workosOrgID = "org_01HZDOMDELNULL"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_del_null", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")))

	require.Empty(t, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainDeletedLastDomainLeavesEmptyList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_deleted_last")
	const workosOrgID = "org_01HZDOMDELLAST"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_del_last", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")))

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.NotNil(t, row.VerifiedDomains)
	require.Empty(t, row.VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventsReadNestedPayload(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_nested_payload")
	const workosOrgID = "org_01HZDOMNESTED"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_nested", workosOrgID, []string{"old.example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", nestedDomainPayload(workosOrgID, "new.example.com")),
		domainDeletedEvent("event_02", nestedDomainPayload(workosOrgID, "old.example.com")),
	)

	require.Equal(t, []string{"new.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventsReadFlatPayload(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_flat_payload")
	const workosOrgID = "org_01HZDOMFLAT"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_flat", workosOrgID, []string{"old.example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "new.example.com")),
		domainDeletedEvent("event_02", flatDomainPayload(workosOrgID, "old.example.com")),
	)

	require.Equal(t, []string{"new.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventWithoutOrganizationIDUsesActivityOrg(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_no_org_id")
	const workosOrgID = "org_01HZDOMNOORG"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_no_org", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", []byte(`{"object":"organization_domain","domain":"example.com"}`)))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventForOtherOrganizationIsSkipped(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_other_org")
	const workosOrgID = "org_01HZDOMSELF"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_self", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload("org_01HZDOMOTHER", "intruder.example.com")),
		domainDeletedEvent("event_02", nestedDomainPayload("org_01HZDOMOTHER", "example.com")),
	)

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"example.com"}, row.VerifiedDomains)
	require.Equal(t, seededDomainEventID, row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DomainEventWithoutDomainIsSkipped(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_empty_domain")
	const workosOrgID = "org_01HZDOMEMPTYDOMAIN"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_empty_domain", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "")),
		domainDeletedEvent("event_02", []byte(`{"organization_domain":{"organization_id":"`+workosOrgID+`","domain":"  "}}`)),
	)

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"example.com"}, row.VerifiedDomains)
	require.Equal(t, seededDomainEventID, row.WorkosLastEventID.String)

	// The skipped events still advance the sync cursor, so they are not
	// retried.
	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(t.Context(), workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_02", cursor)
}

func TestProcessWorkOSOrganizationEvents_DomainEventForUnknownOrganizationIsSkipped(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_unknown_org")
	const workosOrgID = "org_01HZDOMUNKNOWN"

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "example.com")))

	_, err := orgrepo.New(conn).GetOrganizationByWorkosID(t.Context(), conv.ToPGText(workosOrgID))
	require.ErrorIs(t, err, pgx.ErrNoRows)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(t.Context(), workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01", cursor)
}

func TestProcessWorkOSOrganizationEvents_StaleDomainEventIsNotApplied(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_stale")
	const workosOrgID = "org_01HZDOMSTALE"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_stale", workosOrgID, []string{"example.com"})

	// The first event moves the cursor to event_05, so the older event_03
	// and the replayed event_05 are both stale.
	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_05", flatDomainPayload(workosOrgID, "new.example.com")),
		domainDeletedEvent("event_03", flatDomainPayload(workosOrgID, "example.com")),
		domainDeletedEvent("event_05", flatDomainPayload(workosOrgID, "new.example.com")),
	)

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"example.com", "new.example.com"}, row.VerifiedDomains)
	require.Equal(t, "event_05", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DomainEventAtStoredCursorIsNotApplied(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_at_cursor")
	const workosOrgID = "org_01HZDOMATCURSOR"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_at_cursor", workosOrgID, []string{"example.com"})

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent(seededDomainEventID, flatDomainPayload(workosOrgID, "example.com")))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_DomainEventRecordsLastEventID(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_last_event_id")
	const workosOrgID = "org_01HZDOMLASTEVENT"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_last_event", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, domainVerifiedEvent("event_01HZDOMVER", flatDomainPayload(workosOrgID, "example.com")))
	require.Equal(t, "event_01HZDOMVER", getOrgByWorkOSID(t, conn, workosOrgID).WorkosLastEventID.String)

	runOrgEvents(t, conn, workosOrgID, domainDeletedEvent("event_01HZDOMVES", flatDomainPayload(workosOrgID, "example.com")))
	require.Equal(t, "event_01HZDOMVES", getOrgByWorkOSID(t, conn, workosOrgID).WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DomainEventSequenceYieldsFinalList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_domain_sequence")
	const workosOrgID = "org_01HZDOMSEQ"
	createWorkOSLinkedOrg(t, conn, "gram_org_dom_seq", workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "first.example.com")),
		domainVerifiedEvent("event_02", nestedDomainPayload(workosOrgID, "second.example.com")),
		domainDeletedEvent("event_03", flatDomainPayload(workosOrgID, "first.example.com")),
	)

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"second.example.com"}, row.VerifiedDomains)
	require.Equal(t, "event_03", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateStoresOnlyVerifiedDomains(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_update_verified_domains")

	const workosOrgID = "org_01HZDOMAINSMIXED"
	const organizationID = "gram_org_domains_mixed"

	require.NoError(t, orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Mixed",
		Slug: "mixed",
	}))

	runOrgEvents(t, conn, workosOrgID, organizationUpdatedEvent("event_01HZDOMMIX", workosOrgID, organizationID,
		`{"object":"organization_domain","id":"org_domain_1","domain":"Example.COM","state":"verified"},`+
			`{"object":"organization_domain","id":"org_domain_2","domain":"pending.example.com","state":"pending"},`+
			`{"object":"organization_domain","id":"org_domain_3","domain":"Legacy.Example.com","state":"legacy_verified"},`+
			`{"object":"organization_domain","id":"org_domain_4","domain":"failed.example.com","state":"failed"}`))

	require.Equal(t, []string{"example.com", "legacy.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateDropsCaseDuplicates(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_update_case_dup")
	const workosOrgID = "org_01HZDOMUPDCASE"
	const organizationID = "gram_org_domains_upd_case"
	createWorkOSLinkedOrg(t, conn, organizationID, workosOrgID, nil)

	runOrgEvents(t, conn, workosOrgID, organizationUpdatedEvent("event_01", workosOrgID, organizationID,
		`{"object":"organization_domain","domain":"Example.com","state":"verified"},`+
			`{"object":"organization_domain","domain":"example.COM","state":"legacy_verified"}`))

	require.Equal(t, []string{"example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateWithOnlyPendingDomainsClearsVerifiedDomains(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_update_pending_domains")

	const workosOrgID = "org_01HZDOMAINSPENDING"
	const organizationID = "gram_org_domains_pending"

	createWorkOSLinkedOrg(t, conn, organizationID, workosOrgID, []string{"old.example.com"})

	runOrgEvents(t, conn, workosOrgID, organizationUpdatedEvent("event_01HZDOMPEND", workosOrgID, organizationID,
		`{"object":"organization_domain","id":"org_domain_1","domain":"example.com","state":"pending"}`))

	require.Empty(t, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateAfterDomainEventsReconcilesList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_update_reconciles")
	const workosOrgID = "org_01HZDOMRECONCILE"
	const organizationID = "gram_org_domains_reconcile"
	createWorkOSLinkedOrg(t, conn, organizationID, workosOrgID, []string{"a.example.com"})

	runOrgEvents(t, conn, workosOrgID,
		domainVerifiedEvent("event_01", flatDomainPayload(workosOrgID, "b.example.com")),
		domainVerifiedEvent("event_02", flatDomainPayload(workosOrgID, "c.example.com")),
	)
	require.Equal(t, []string{"a.example.com", "b.example.com", "c.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)

	// The organization payload lists every domain, so it replaces whatever
	// the incremental events built up.
	runOrgEvents(t, conn, workosOrgID, organizationUpdatedEvent("event_03", workosOrgID, organizationID,
		`{"object":"organization_domain","domain":"c.example.com","state":"verified"},`+
			`{"object":"organization_domain","domain":"a.example.com","state":"pending"},`+
			`{"object":"organization_domain","domain":"d.example.com","state":"legacy_verified"}`))

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, []string{"c.example.com", "d.example.com"}, row.VerifiedDomains)
	require.Equal(t, "event_03", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateStoresOnlyVerifiedDomains(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_create_verified_domains")
	const workosOrgID = "org_01HZDOMCREATEMIXED"
	const organizationID = "gram_org_domains_create_mixed"

	runOrgEvents(t, conn, workosOrgID, organizationCreatedEvent("event_01", workosOrgID, organizationID,
		`{"object":"organization_domain","domain":"Example.COM","state":"verified"},`+
			`{"object":"organization_domain","domain":"pending.example.com","state":"pending"},`+
			`{"object":"organization_domain","domain":"Legacy.Example.com","state":"legacy_verified"},`+
			`{"object":"organization_domain","domain":"failed.example.com","state":"failed"}`))

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.Equal(t, organizationID, row.ID)
	require.Equal(t, []string{"example.com", "legacy.example.com"}, row.VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateDropsCaseDuplicates(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_create_case_dup")
	const workosOrgID = "org_01HZDOMCREATECASE"
	const organizationID = "gram_org_domains_create_case"

	runOrgEvents(t, conn, workosOrgID, organizationCreatedEvent("event_01", workosOrgID, organizationID,
		`{"object":"organization_domain","domain":" Example.com ","state":"verified"},`+
			`{"object":"organization_domain","domain":"example.COM","state":"legacy_verified"},`+
			`{"object":"organization_domain","domain":"other.example.com","state":"verified"}`))

	require.Equal(t, []string{"example.com", "other.example.com"}, getOrgByWorkOSID(t, conn, workosOrgID).VerifiedDomains)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateWithoutVerifiedDomainsStoresEmptyList(t *testing.T) {
	t.Parallel()

	conn := newOrgEventsTestConn(t, "workos_org_events_create_no_verified")
	const workosOrgID = "org_01HZDOMCREATENONE"
	const organizationID = "gram_org_domains_create_none"

	runOrgEvents(t, conn, workosOrgID, organizationCreatedEvent("event_01", workosOrgID, organizationID,
		`{"object":"organization_domain","domain":"pending.example.com","state":"pending"},`+
			`{"object":"organization_domain","domain":"failed.example.com","state":"failed"}`))

	row := getOrgByWorkOSID(t, conn, workosOrgID)
	require.NotNil(t, row.VerifiedDomains)
	require.Empty(t, row.VerifiedDomains)
}
