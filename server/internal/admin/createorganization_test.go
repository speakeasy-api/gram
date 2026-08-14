package admin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
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

	// createdGramIDs records the third argument of every CreateOrganization
	// call. The real client feeds that argument to BOTH external_id and the
	// idempotency key, so what is passed here is load-bearing rather than
	// incidental.
	createdGramIDs []string

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
		createdGramIDs: nil,
		externalIDs:    map[string]string{},
	}
}

func (f *fakeWorkOSCreator) CreateOrganization(_ context.Context, name, gramOrgID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return "", f.createErr
	}

	f.createdNames = append(f.createdNames, name)
	f.createdGramIDs = append(f.createdGramIDs, gramOrgID)
	return f.organizationID, nil
}

func (f *fakeWorkOSCreator) UpdateOrganizationExternalID(_ context.Context, workosOrgID, externalID string) error {
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

func (f *fakeWorkOSCreator) gramIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.createdGramIDs...)
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
func runOrganizationWebhook(t *testing.T, ctx context.Context, conn *pgxpool.Pool, event events.Event) {
	t.Helper()

	stub := workos.NewStubClient()
	stub.SetEventPages([][]events.Event{{event}})

	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: event.ID,
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

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Acme Create Co", AdminSessionToken: nil})
	require.NoError(t, err)

	// The whole idempotency story rests on this equality. A generated ID would
	// pass every other assertion in this file except the two ordering tests.
	require.Equal(t, orgid.FromWorkOSID(workosOrgID), res.ID,
		"the Gram id must be derived from the WorkOS id, not minted")
	require.NotNil(t, res.WorkosID)
	require.Equal(t, workosOrgID, *res.WorkosID, "the row must be linked to the WorkOS organization")

	require.Equal(t, []string{"Acme Create Co"}, fake.names(), "WorkOS must be asked for exactly one organization")
	require.Equal(t, res.ID, fake.externalID(workosOrgID),
		"external_id must be back-filled with the Gram id, or the sync path resolves this organization by a different route")

	// The create must carry no Gram id, because the real client feeds this
	// argument to the idempotency key as well as to external_id. Anything
	// derived from the name would make WorkOS answer a second create for a
	// same-named organization with the first one, and the second Gram insert
	// would then collide on the unique index over workos_id. The value cannot
	// be filled in correctly here either: it is derived from the WorkOS id this
	// call has not returned yet.
	require.Equal(t, []string{""}, fake.gramIDs(),
		"the create must send an empty external_id and idempotency key")

	require.Equal(t, "Acme Create Co", res.Name)
	require.Equal(t, "acme-create-co", res.Slug)
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

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Hook After Co", AdminSessionToken: nil})
	require.NoError(t, err)

	// Both polarities of external_id. WorkOS emits the event before the
	// back-fill lands, so the first delivery carries no external_id and the
	// sync has to derive the id; a later organization.updated carries it.
	runOrganizationWebhook(t, ctx, conn, organizationEvent("event_01HZA", "organization.created", workosOrgID, "Hook After Co", ""))
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"a webhook with no external_id must derive the same id and update the existing row")

	runOrganizationWebhook(t, ctx, conn, organizationEvent("event_01HZB", "organization.updated", workosOrgID, "Hook After Co", res.ID))
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
	runOrganizationWebhook(t, ctx, conn, organizationEvent("event_01HZC", "organization.created", workosOrgID, "Race Co From The Sync", ""))

	derivedID := orgid.FromWorkOSID(workosOrgID)
	seeded, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, derivedID)
	require.NoError(t, err, "the sync must have created the row this test is about")

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Race Co", AdminSessionToken: nil})
	require.NoError(t, err, "a create that collides with the sync must not surface a unique violation")
	require.Equal(t, derivedID, res.ID)
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, workosOrgID),
		"the two writers must converge on one row")

	// The operator typed this name second and it wins, which is the
	// name = EXCLUDED.name arm of the upsert.
	require.Equal(t, "Race Co", res.Name, "the operator's name must overwrite the one the sync wrote")
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
	require.NoError(t, blockerQueries.LockOrganizationSlug(ctx, "lock-race-co"))

	type outcome struct {
		res *gen.AdminOrganization
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Lock Race Co", AdminSessionToken: nil})
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

	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Rejected Co", AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)

	requireNoOrganizationRow(t, ctx, conn, workosOrgID)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Empty(t, list.Organizations, "a rejected create must leave nothing for an operator to find")
}

// TestCreateOrganization_ExternalIDBackFillFailureLeavesNoGramRow covers the
// half-created state: WorkOS accepted the organization and then refused the
// external_id write. Gram must still store nothing, because a row whose WorkOS
// counterpart does not point back at it is worse than no row at all.
func TestCreateOrganization_ExternalIDBackFillFailureLeavesNoGramRow(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZBACKFILLFAIL"
	fake := newFakeWorkOS(workosOrgID)
	fake.updateErr = errors.New("workos said no")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Half Made Co", AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeGatewayError)

	require.Equal(t, []string{"Half Made Co"}, fake.names(), "the WorkOS organization really was created")
	requireNoOrganizationRow(t, ctx, conn, workosOrgID)
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

	_, err = svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Rolled Back Co", AdminSessionToken: nil})
	require.Error(t, err, "a failure seeding default entitlements must fail the request")

	require.Equal(t, []string{"Rolled Back Co"}, fake.names(),
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

	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "No Idp Co", AdminSessionToken: nil})

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

// TestCreateOrganization_RejectsUnusableNames pins that this endpoint accepts
// exactly what signup accepts. The cases are the boundaries of
// orgprovision.ValidateName rather than a sample: one rune under and over each
// limit, and both polarities of the graphic-character rule.
func TestCreateOrganization_RejectsUnusableNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "spaces only", input: "   ", wantErr: true},
		{name: "unicode spaces only", input: "\u00a0\u3000", wantErr: true},
		{name: "one letter", input: "A", wantErr: true},
		{name: "punctuation only", input: "-- ...", wantErr: true},
		{name: "one letter and punctuation", input: "A.", wantErr: true},
		{name: "control character", input: "Acme\u202eCo", wantErr: true},
		{name: "one rune past the limit", input: strings.Repeat("a", orgprovision.MaxNameLength+1), wantErr: true},
		{name: "past the raw byte ceiling", input: strings.Repeat("a", orgprovision.MaxRawNameBytes+1), wantErr: true},

		{name: "two letters", input: "Ab"},
		{name: "at the limit", input: strings.Repeat("a", orgprovision.MaxNameLength)},
		{name: "letters and punctuation", input: "Bob's Bakery, Inc."},
		{name: "non-latin", input: "顶尖科技"},

		// The three cases below write their lengths out instead of deriving
		// them from the constants, which is the only way they can hold the
		// constants still. Every case above moves with MaxNameLength and
		// MaxRawNameBytes, so either limit can change by one and the whole
		// table still passes.
		//
		// This one is 100 characters, so it pins MaxNameLength from below.
		{name: "at the limit, written out", input: "Northwind Traders International Logistics and Freight Forwarding Company Ltd of Great Britain PLC Co"},
		// 101, which pins it from above.
		{name: "one rune past the limit, written out", input: "Northwind Traders International Logistics and Freight Forwarding Company Ltd of Great Britain PLC Co.", wantErr: true},
		// 4000 bytes exactly, which is MaxRawNameBytes today. The padding is
		// whitespace so the name normalizes to two characters and is accepted:
		// an input of 4000 letters would be refused for its rune count and
		// prove nothing about the byte ceiling. Accepting this pins both that
		// the ceiling is 4000 rather than 3999 and that the comparison is >
		// rather than >=.
		{name: "exactly at the raw byte ceiling", input: "Ab" + strings.Repeat(" ", 3998)},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Each case owns its own WorkOS organization so the accepted ones
			// do not collide on workos_id.
			workosOrgID := "org_01HZNAME" + string(rune('A'+i))
			fake := newFakeWorkOS(workosOrgID)
			ctx, svc, conn := newTestAdminServiceWithWorkOS(t, fake)

			_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: tc.input, AdminSessionToken: nil})

			if !tc.wantErr {
				require.NoError(t, err)
				require.Len(t, fake.names(), 1)
				return
			}

			requireOopsCode(t, err, oops.CodeInvalid)
			require.Empty(t, fake.names(),
				"an unusable name must be refused before WorkOS is asked for an organization, or every rejected request leaks one")
			requireNoOrganizationRow(t, ctx, conn, workosOrgID)
		})
	}
}

// TestCreateOrganization_NormalizesTheNameTheSameWaySignupDoes pins the other
// half of sharing the validator: the stored name is the normalized one, so two
// paths that were handed the same name store the same string.
func TestCreateOrganization_NormalizesTheName(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZNORMALIZE"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, _ := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "  Spaced\u00a0\u00a0Out  ", AdminSessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "Spaced Out", res.Name, "the stored name must be the normalized one")
	require.Equal(t, []string{"Spaced Out"}, fake.names(), "WorkOS must be given the normalized name too, or the two systems disagree")
	require.Equal(t, "spaced-out", res.Slug)
}

// TestCreateOrganization_NameWithNoLatinSlugFallsBackToTheWorkOSID pins
// StableBase. A name that slugifies to nothing has to fall back to something,
// and the fallback is the WorkOS organization id rather than randomness: two
// attempts at one create then compete for a single advisory-lock key, and a
// retry heads for the slug the first attempt was heading for.
func TestCreateOrganization_NameWithNoLatinSlugFallsBackToTheWorkOSID(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZNOLATIN"
	fake := newFakeWorkOS(workosOrgID)
	ctx, svc, _ := newTestAdminServiceWithWorkOS(t, fake)

	res, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "顶尖科技", AdminSessionToken: nil})
	require.NoError(t, err)

	// Written out rather than computed. Deriving the expectation from the same
	// function under test would accept a random fallback just as happily.
	require.Equal(t, "org-01hznolatin", res.Slug,
		"a name with no slug of its own must fall back to the WorkOS organization id")
	require.Equal(t, "顶尖科技", res.Name, "the fallback slug must not become the name")
}

func TestCreateOrganization_TwoOrganizationsCanShareAName(t *testing.T) {
	t.Parallel()

	firstFake := newFakeWorkOS("org_01HZSAMENAME1")
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, firstFake)

	first, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Duplicate Co", AdminSessionToken: nil})
	require.NoError(t, err)

	svc.workos = newFakeWorkOS("org_01HZSAMENAME2")
	second, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{Name: "Duplicate Co", AdminSessionToken: nil})
	require.NoError(t, err)

	require.NotEqual(t, first.ID, second.ID, "two WorkOS organizations must not derive one Gram id")
	require.NotEqual(t, first.Slug, second.Slug, "the second organization must get its own slug")
	require.Equal(t, "duplicate-co", first.Slug)

	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, "org_01HZSAMENAME1"))
	require.Equal(t, int64(1), countOrganizationsForWorkOSID(t, ctx, conn, "org_01HZSAMENAME2"))
}
