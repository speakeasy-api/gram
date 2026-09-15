package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// fakeWorkOSCreator is an identity provider a test can steer. workos.StubClient
// cannot be made to fail, and this slice has to prove what a rejected create
// leaves behind; it also mints its own organization IDs, and these tests need
// to know the ID up front so they can drive the webhook path at the same
// organization.
type fakeWorkOSCreator struct {
	mu sync.Mutex

	// organizationID is what CreateOrganization returns.
	organizationID string

	// createErr, when set, fails CreateOrganization and nothing else runs.
	createErr error

	// updateErr, when set, fails the external_id back-fill. That is the
	// half-created state: a WorkOS organization exists and carries no
	// external_id pointing back at Gram.
	updateErr error

	// createdNames records every name CreateOrganization was called with, in
	// order, so a test can assert that a rejected request never reached WorkOS.
	createdNames []string

	// externalIDs records the last external_id written per WorkOS organization.
	externalIDs map[string]string
}

func newFakeWorkOS(organizationID string) *fakeWorkOSCreator {
	return &fakeWorkOSCreator{
		mu:             sync.Mutex{},
		organizationID: organizationID,
		createErr:      nil,
		updateErr:      nil,
		createdNames:   nil,
		externalIDs:    map[string]string{},
	}
}

func (f *fakeWorkOSCreator) CreateOrganizationWithVerifiedDomain(_ context.Context, hostname string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.createdNames = append(f.createdNames, hostname)
	if f.createErr != nil {
		return "", f.createErr
	}

	return f.organizationID, nil
}

func (f *fakeWorkOSCreator) UpdateOrganizationExternalIDWithoutRetry(_ context.Context, workosOrgID, externalID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.updateErr != nil {
		return f.updateErr
	}

	f.externalIDs[workosOrgID] = externalID
	return nil
}

func (f *fakeWorkOSCreator) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.createdNames...)
}

func (f *fakeWorkOSCreator) externalID(workosOrgID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.externalIDs[workosOrgID]
}

// countOrganizationsForWorkOSID counts rows carrying a WorkOS organization ID.
// The point of every idempotency assertion below is that this stays at one, and
// asking the database directly is the only way to see a second row: every read
// the API offers returns at most one.
func countOrganizationsForWorkOSID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosOrgID string) int64 {
	t.Helper()

	count, err := testrepo.New(conn).CountOrganizationsForWorkosIDFixture(ctx, workosOrgID)
	require.NoError(t, err)

	return count
}

// runOrganizationWebhook runs the WorkOS event sync for one organization, which
// is the other writer that can create the row this endpoint creates.
//
// workosOrgID is passed separately from the event because the activity uses it
// to key the sync cursor and to filter the WorkOS events listing, neither of
// which an event ID would address. The stub ignores the filter, so an event ID
// here would still pass while modelling a call production never makes.
func runOrganizationWebhook(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosOrgID string, event events.Event) {
	t.Helper()

	stub := workos.NewStubClient()
	stub.SetEventPages([][]events.Event{{event}})

	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
		SinceEventID:         nil,
	})
	require.NoError(t, err, "the webhook path must not fail against an organization the admin endpoint created")
}

// organizationEvent builds a WorkOS organization event. externalID is passed
// separately because the two orderings differ precisely there: an event that
// arrives after the back-fill carries the Gram ID, and one that overtakes it
// carries nothing and makes the sync derive the ID instead.
func organizationEvent(eventID, kind, workosOrgID, name, externalID string) events.Event {
	payload := `{"id":"` + workosOrgID + `","object":"organization","name":"` + name +
		`","external_id":"` + externalID + `","updated_at":"2026-05-06T12:00:00Z"}`

	return events.Event{
		ID:        eventID,
		Event:     kind,
		CreatedAt: time.Now(),
		Data:      []byte(payload),
	}
}

func requireNoOrganizationRow(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosOrgID string) {
	t.Helper()

	require.Zero(t, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"a failed create must leave no organization row behind")

	_, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, orgid.FromWorkOSID(workosOrgID))
	require.Error(t, err, "a failed create must leave no row under the derived id either")
}

func TestCreateOrganization_CreatesInWorkOSAndInGram(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZADMINCREATE"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "https://example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	// The whole idempotency story rests on this equality. A generated ID would
	// pass every other assertion in this file except the two ordering tests.
	require.Equal(t, orgid.FromWorkOSID(workosOrgID), res.ID,
		"the Gram id must be derived from the WorkOS id, not minted")
	require.NotNil(t, res.WorkosID)
	require.Equal(t, workosOrgID, *res.WorkosID, "the row must be linked to the WorkOS organization")

	require.Equal(t, []string{"example.com"}, fake.names(), "WorkOS must be asked for exactly one verified domain")
	require.Equal(t, res.ID, fake.externalID(workosOrgID),
		"external_id must be back-filled with the Gram id, or the sync path resolves this organization by a different route")

	require.Equal(t, "example", res.Name)
	require.Equal(t, "example", res.Slug)
	require.Equal(t, 0, res.MemberCount, "an admin-created organization starts empty")
	require.Nil(t, res.DisabledAt)

	// An operator creating an organization is not saying anything about paid
	// tier, the book-a-demo waiver, or a trial. Each of these is a separate
	// grant with its own endpoint.
	require.False(t, res.Whitelisted, "a created organization must not be whitelisted")
	require.Equal(t, "free", res.AccountType, "a created organization must not arrive on a paid tier")
	require.NotNil(t, res.TrialState)
	require.Equal(t, "none", *res.TrialState, "a created organization must not arrive with a trial")
	require.Nil(t, res.TrialEndsAt)

	_, err = trialsrepo.New(conn).GetTrial(ctx, res.ID)
	require.Error(t, err, "creating an organization must not write a trial row")

	// The defaults an organization cannot function without.
	for _, roleSlug := range []string{authz.SystemRoleAdmin, authz.SystemRoleMember} {
		role, err := accessrepo.New(conn).GetActiveOrganizationRoleBySlug(ctx, accessrepo.GetActiveOrganizationRoleBySlugParams{
			OrganizationID: res.ID,
			WorkosSlug:     roleSlug,
		})
		require.NoError(t, err, "the %s role must resolve for a created organization", roleSlug)

		grants, err := accessrepo.New(conn).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
			OrganizationID: res.ID,
			PrincipalUrns:  []string{role.RoleUrn},
		})
		require.NoError(t, err)
		require.NotEmpty(t, grants, "the %s role must be granted on a created organization", roleSlug)
	}

	enabled, err := featuresrepo.New(conn).IsFeatureEnabled(ctx, featuresrepo.IsFeatureEnabledParams{
		OrganizationID: res.ID,
		FeatureName:    string(productfeatures.FeaturePlatformMCP),
	})
	require.NoError(t, err)
	require.True(t, enabled, "the default entitlements must be seeded on a created organization")

	// The operator finds the organization again through both read surfaces, not
	// only in the response body of the write.
	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: res.ID, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, res.ID, detail.ID)
	require.Equal(t, res.Slug, detail.Slug)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.Equal(t, res.ID, list.Organizations[0].ID)
}

// TestCreateOrganization_WebhookAfterwardsDoesNotDuplicate is the ordering the
// endpoint produces on every successful call: WorkOS fires organization.created
// and the sync activity applies it against a row this endpoint already wrote.
func TestCreateOrganization_WebhookAfterwardsDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZADMINTHENHOOK"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "hook.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	// Both polarities of external_id. WorkOS emits the event before the
	// back-fill lands, so the first delivery carries no external_id and the
	// sync has to derive the id; a later organization.updated carries it.
	runOrganizationWebhook(t, ctx, conn, workosOrgID, organizationEvent("event_01HZA", "organization.created", workosOrgID, "Hook After Co", ""))
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"a webhook with no external_id must derive the same id and update the existing row")

	runOrganizationWebhook(t, ctx, conn, workosOrgID, organizationEvent("event_01HZB", "organization.updated", workosOrgID, "Hook After Co", res.ID))
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"a webhook carrying the external_id must resolve to the same row")

	after, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: res.ID, AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, res.Slug, after.Slug, "the sync must not move the slug of an organization the operator already sees")
	require.False(t, after.Whitelisted)
}

// TestCreateOrganization_WebhookThatWonTheRaceIsUpdatedNotDuplicated is the
// other ordering. WorkOS fires organization.created the moment the create call
// returns, so the sync activity can insert the row before this handler's
// transaction opens. It must land on that row rather than beside it.
func TestCreateOrganization_WebhookThatWonTheRaceIsUpdatedNotDuplicated(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZHOOKTHENADMIN"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	// No external_id: at this instant the handler has not back-filled it yet,
	// so the sync derives the id the handler is about to write under. The name
	// differs from the one the operator types below on purpose: handing both
	// writers the same name would make the name assertion pass whichever of
	// them won, which is the half of this test that would otherwise only look
	// like coverage.
	runOrganizationWebhook(t, ctx, conn, workosOrgID, organizationEvent("event_01HZC", "organization.created", workosOrgID, "Race Co From The Sync", ""))

	derivedID := orgid.FromWorkOSID(workosOrgID)
	seeded, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, derivedID)
	require.NoError(t, err, "the sync must have created the row this test is about")

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "race.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err, "a create that collides with the sync must not surface a unique violation")
	require.Equal(t, derivedID, res.ID)
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"the two writers must converge on one row")

	// The operator typed this name second and it wins, which is the
	// name = EXCLUDED.name arm of the upsert.
	require.Equal(t, "example", res.Name, "the derived display name must overwrite the name the sync wrote")
	require.NotEqual(t, seeded.Name, res.Name)

	// The slug is in the organization's URL. Re-deriving one here would find the
	// base taken by this very row and write a suffixed variant over it.
	require.Equal(t, seeded.Slug, res.Slug, "a create landing on an existing row must keep its slug")

	// The sync's cursor is the record of which events have been applied.
	// Nothing in this handler may roll it back.
	cursor := readWorkOSLastEventID(t, ctx, conn, derivedID)
	require.Equal(t, "event_01HZC", cursor, "a create must not clear the webhook cursor")
}

// TestCreateOrganization_SyncCommittingUnderTheSlugLockKeepsItsSlug is the
// narrow version of the race above. There the sync had already committed before
// the handler started; here it commits in the window between the handler's read
// of the organization and the handler taking the slug lock, which is the window
// READ COMMITTED leaves open and the reason the handler reads a second time
// once it holds the lock. Deciding from the first read would hand back a
// suffixed slug and the upsert would write it over a slug already in use.
func TestCreateOrganization_SyncCommittingUnderTheSlugLockKeepsItsSlug(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZLOCKRACE"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	// The competing writer takes the slug lock first and holds it in its own
	// transaction, exactly as the sync activity does. The handler will park on
	// that lock until this transaction commits.
	blocker, err := conn.Begin(ctx) //nolint:glint // the raw-SQL rule catches tx.Exec with a query string; this transaction only ever runs SQLc-generated methods, and it exists to hold an advisory lock the handler must wait on
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()

	blockerQueries := orgrepo.New(blocker)
	require.NoError(t, blockerQueries.LockOrganizationSlug(ctx, "example"))

	type outcome struct {
		res *gen.AdminOrganization
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "lock.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
		done <- outcome{res: res, err: err}
	}()

	// The handler calls WorkOS before it opens its transaction, so a recorded
	// name means it is at or past its first read of the organization and about
	// to ask for the slug lock this test is holding. Committing earlier than
	// that cannot fail the test, because the handler would then see the row in
	// its first read and reach the same slug; it would only prove less.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Len(c, fake.names(), 1)
	}, 10*time.Second, 10*time.Millisecond)

	_, err = blockerQueries.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgid.FromWorkOSID(workosOrgID),
		Name:        "Lock Race Co From The Sync",
		Slug:        "lock-race-co",
		WorkosID:    conv.ToPGText(workosOrgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, blocker.Commit(ctx))

	got := <-done
	require.NoError(t, got.err)

	require.Equal(t, "lock-race-co", got.res.Slug,
		"a row that appeared while the handler waited for the slug lock must keep its slug, not be given a suffixed one")
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"the two writers must still converge on one row")
}

func readWorkOSLastEventID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) string {
	t.Helper()

	row, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	return row.WorkosLastEventID.String
}

func TestCreateOrganization_WorkOSRejectionLeavesNoGramRow(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZREJECTED"
	fake := newFakeWorkOS(workosOrgID)
	fake.createErr = errors.New("workos said no")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "rejected.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Nil(t, res)
	require.EqualError(t, err, organizationCreationUncertain)
	require.ErrorIs(t, err, fake.createErr)
	require.Len(t, fake.names(), 1)

	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Empty(t, list.Organizations, "a rejected create must leave nothing for an operator to find")
}

// TestCreateOrganization_ExternalIDBackFillFailureLeavesNoGramRow covers the
// half-created state: this request stores nothing after an external_id failure,
// but a later webhook can still provision the remote organization.
func TestCreateOrganization_ExternalIDBackFillFailureLeavesNoGramRow(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZBACKFILLFAIL"
	fake := newFakeWorkOS(workosOrgID)
	fake.updateErr = errors.New("workos said no")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "half.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Nil(t, res)
	require.EqualError(t, err, organizationCreationUncertain)
	require.ErrorIs(t, err, fake.updateErr)

	require.Equal(t, []string{"half.example.com"}, fake.names(), "the WorkOS organization really was created")
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)
	runOrganizationWebhook(t, ctx, conn, workosOrgID, organizationEvent("event_backfill_failure", "organization.created", workosOrgID, "example.com", ""))
	require.EqualValues(t, 1, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID))
}

// TestCreateOrganization_FailureAfterTheUpsertLeavesNothing is the only test
// here that reaches the transaction. Every other rollback case above fails
// before tx.Begin, so writing the organization row through the pool rather than
// the transaction satisfies all of them, and the handler's headline promise
// stays a comment.
func TestCreateOrganization_FailureAfterTheUpsertLeavesNothing(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZROLLBACK"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	// Break the table the last write in the transaction touches. Data cannot
	// make that write fail: EnableFeature inserts ON CONFLICT DO NOTHING,
	// organization_features carries no foreign key, and its only CHECK is on a
	// feature name the handler supplies as a constant. Each test holds its own
	// database clone, dropped when the test ends, so this reaches nothing else.
	_, err := conn.Exec(ctx, "DROP TABLE organization_features;") //nolint:glint // no generated query can drop a table, and this database is a per-test clone
	require.NoError(t, err)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "rollback.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.Error(t, err, "a failure seeding default entitlements must fail the request")
	require.Nil(t, res)
	require.EqualError(t, err, organizationCreationUncertain)
	require.Contains(t, oops.Detail(err), "seed organization default entitlements")

	require.Equal(t, []string{"rollback.example.com"}, fake.names(),
		"WorkOS accepted the organization, so the failure really did happen after the upsert")

	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	grants, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgid.FromWorkOSID(workosOrgID),
		PrincipalUrn:   "",
	})
	require.NoError(t, err)
	require.Empty(t, grants, "the role grants seeded in the same transaction must be gone too")

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Empty(t, list.Organizations, "a rolled-back create must leave nothing for an operator to find")
}

func TestCreateOrganization_WithoutWorkOSConfiguration(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZUNCONFIGURED"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, orgprovision.Unavailable{})

	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "no-idp.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})

	// Not a gateway error: nothing was asked of WorkOS and retrying will not
	// help. The organization cannot be logged into, so reporting failure is the
	// only honest answer.
	//
	// The code matters beyond its name. server/design/shared/errors.go maps
	// invalid to 422 and invariant_violation, which reads like the better fit,
	// to 500; the admin app trusts a response body only below 500, so under
	// invariant_violation the operator would see a bare server error instead of
	// the sentence below.
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "WorkOS configuration",
		"the operator must be told why, or an unconfigured deployment is indistinguishable from a broken one")
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Empty(t, list.Organizations, "an unconfigured server must not mint a local-only organization")
}

func TestCreateOrganization_PostCommitReadFailureIsUncertain(t *testing.T) {
	t.Parallel()
	const workosOrgID = "org_read_failure"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)
	// Billing is read only by the response query, not the creation transaction.
	_, err := conn.Exec(ctx, "ALTER TABLE billing_metadata RENAME TO unavailable_billing_metadata") //nolint:glint // DDL fault injection in an isolated per-test database; SQLc cannot rename a table.
	require.NoError(t, err)
	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.Nil(t, res)
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.EqualError(t, err, organizationCreationUncertain)
	require.Contains(t, oops.Detail(err), "fetch organization after create")
	require.Equal(t, []string{"example.com"}, fake.names())
	require.EqualValues(t, 1, countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID), "the transaction committed despite the failed response")
}

func TestCreateOrganization_HTTPProviderFailuresAreSanitized(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, orgprovision.Unavailable{})
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	for _, tc := range []struct {
		status     int
		body       string
		failUpdate bool
		refusal    bool
	}{
		{status: 400, body: `{"message":"private provider detail","code":"unknown_code","unknown":"private field"}`, failUpdate: false, refusal: true},
		{status: 422, body: `{"message":"private provider detail","errors":[{"field":"domain_data","code":"unknown_code"}]}`, failUpdate: false, refusal: true},
		{status: 409, body: `{"message":"private provider detail","code":"unknown_code"}`, failUpdate: false, refusal: false},
		{status: 429, body: `{"message":"private provider detail"}`, failUpdate: false, refusal: false},
		{status: 500, body: `{"message":"private provider detail"}`, failUpdate: false, refusal: false},
		{status: 502, body: `private provider detail`, failUpdate: false, refusal: false},
		{status: 200, body: `private provider detail`, failUpdate: false, refusal: false},
		{status: 200, body: `{"id":"org_http_failure","domains":[{"domain":"example.com","state":"pending"}]}`, failUpdate: false, refusal: false},
		{status: 200, body: `{"id":"org_http_failure","domains":[{"domain":"example.com","state":"failed"}]}`, failUpdate: false, refusal: false},
		{status: 422, body: `{"message":"private provider detail"}`, failUpdate: true, refusal: false},
		{status: 500, body: `{"message":"private provider detail"}`, failUpdate: true, refusal: false},
	} {
		var calls atomic.Int32
		svc.workos = newAdminWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", "request_example")
			if tc.failUpdate && r.Method == http.MethodPost {
				_, _ = io.WriteString(w, `{"id":"org_http_failure","domains":[{"domain":"example.com","state":"verified"}]}`)
				return
			}
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		})
		var logs bytes.Buffer
		svc.logger = slog.New(slog.NewJSONHandler(&logs, nil))
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.create", strings.NewReader(`{"url":"https://example.com","ownership_confirmed":true}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var body struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		if tc.refusal {
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			require.Equal(t, "WorkOS rejected organization creation. Check the company URL and whether its domain is eligible for verification.", body.Message)
		} else {
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Equal(t, organizationCreationUncertain, body.Message)
		}
		for _, private := range []string{"private provider detail", "private field", "unknown_code", "request_example"} {
			require.NotContains(t, rec.Body.String(), private)
		}
		if tc.status != http.StatusOK {
			require.Contains(t, logs.String(), "request_example")
		}
		require.EqualValues(t, conv.Ternary(tc.failUpdate, 2, 1), calls.Load(), "no retry, tenant lookup, transfer, or cleanup")
		requireNoOrganizationRow(t, ctx, conn, "org_http_failure")
	}
}

func TestCreateOrganization_RejectsInvalidURLsBeforeWorkOS(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkOS("org_invalid_url")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)
	for _, input := range invalidOrganizationURLs() {
		_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: input, OwnershipConfirmed: true, AdminSessionToken: nil})
		requireOopsCode(t, err, oops.CodeInvalid)
		require.Empty(t, fake.names(), "invalid URL %q must cause no remote writes", input)
	}
	requireNoOrganizationRow(t, ctx, conn, "org_invalid_url")
}

func TestCreateOrganization_NormalizesTheExactHostname(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZNORMALIZE"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, _ := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "  https://WWW.Example.COM./about?x=1#team  ", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "example", res.Name)
	require.Equal(t, []string{"www.example.com"}, fake.names())
	require.Equal(t, "example", res.Slug)
}

func TestCreateOrganization_RequiresOwnershipConfirmation(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_unconfirmed"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)
	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "example.com", OwnershipConfirmed: false, AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Empty(t, fake.names())
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)
}

func TestCreateOrganization_TwoOrganizationsCanShareAName(t *testing.T) {
	t.Parallel()

	firstFake := newFakeWorkOS("org_01HZSAMENAME1")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, firstFake)

	first, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "duplicate.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	svc.workos = newFakeWorkOS("org_01HZSAMENAME2")
	second, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "duplicate.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	require.NotEqual(t, first.ID, second.ID, "two WorkOS organizations must not derive one Gram id")
	require.NotEqual(t, first.Slug, second.Slug, "the second organization must get its own slug")
	require.Equal(t, "example", first.Slug)

	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, "org_01HZSAMENAME1"))
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, "org_01HZSAMENAME2"))
}

func TestCreateOrganization_HTTPRequiredFields(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS("org_http_contract")
	ctx, svc, _ := newTestAdminServiceWithWorkOS(t, fake)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	sessionID := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")
	for _, body := range []string{
		`{}`, `{"name":"Old Name"}`, `{"url":"example.com"}`,
		`{"ownership_confirmed":true}`, `{"url":null,"ownership_confirmed":true}`,
		`{"url":"example.com","ownership_confirmed":null}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", body, rec.Body.String())
	}
	require.Empty(t, fake.names())
}

func TestCreateOrganization_HTTPRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS("org_http_auth")
	_, svc, _ := newTestAdminServiceWithWorkOS(t, fake)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	for _, token := range []string{"", "invalid-session"} {
		req := httptest.NewRequest(http.MethodPost, "/admin/organization.create", strings.NewReader(`{"url":"example.com","ownership_confirmed":true}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: token})
		}
		rec := httptest.NewRecorder()
		SessionMiddleware(mux).ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	require.Empty(t, fake.names())
}
