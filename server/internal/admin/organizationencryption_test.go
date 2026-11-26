package admin

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testEncryptionUseCase = orgencryption.UseCaseOAuthClientSecrets

// countingCopyStore reports fixed counts and organizations with values and
// stores no values, so tests can place an organization anywhere in its
// transition without sealing values.
type countingCopyStore struct {
	mu         sync.Mutex
	counts     orgencryption.CopyCounts
	withValues []orgencryption.OrganizationValues
}

func (c *countingCopyStore) setOrganizationsWithValues(withValues ...orgencryption.OrganizationValues) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.withValues = withValues
}

func (c *countingCopyStore) setCounts(counts orgencryption.CopyCounts) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts = counts
}

func (c *countingCopyStore) Columns() orgencryption.CopyColumns {
	return orgencryption.CopyColumns{UseCase: testEncryptionUseCase, PlatformColumn: "test.platform", OrgColumn: "test.org"}
}

func (c *countingCopyStore) ListWaiting(context.Context, string, string, int32) ([]orgencryption.WaitingValue, error) {
	return nil, nil
}

func (c *countingCopyStore) SealWaiting(context.Context, string, int64, orgencryption.WaitingValue, string) (bool, error) {
	return false, nil
}

func (c *countingCopyStore) ListSealed(context.Context, string, string, int32) ([]orgencryption.SealedValue, error) {
	return nil, nil
}

func (c *countingCopyStore) Count(context.Context, string) (orgencryption.CopyCounts, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts, nil
}

func (c *countingCopyStore) ListOrganizationsWithValues(context.Context) ([]orgencryption.OrganizationValues, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]orgencryption.OrganizationValues(nil), c.withValues...), nil
}

func (c *countingCopyStore) ClearPlatformCopies(context.Context, string, int64, int32) (orgencryption.ClearedCopies, error) {
	return orgencryption.ClearedCopies{PlatformOnly: 0, BesideOrgCopy: 0, Empty: 0}, nil
}

// recordingRunStarter records the runs started and fails with err.
type recordingRunStarter struct {
	mu     sync.Mutex
	runs   []string
	actors []string
	err    error
}

func (r *recordingRunStarter) StartOrganizationEncryptionRun(_ context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error {
	return r.record("backfill", organizationID, useCase, actor)
}

func (r *recordingRunStarter) StartOrganizationEncryptionClear(_ context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error {
	return r.record("clear", organizationID, useCase, actor)
}

func (r *recordingRunStarter) record(kind string, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.runs = append(r.runs, kind+" "+organizationID+" "+string(useCase))
	r.actors = append(r.actors, actor.Principal.String())

	return r.err
}

func (r *recordingRunStarter) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.runs...)
}

type encryptionFixture struct {
	keys  *orgencryptiontest.Fixture
	svc   *Service
	conn  *pgxpool.Pool
	store *countingCopyStore
	runs  *recordingRunStarter
}

func newEncryptionFixture(t *testing.T) (context.Context, *encryptionFixture) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	keys := orgencryptiontest.New(t, conn)
	store := &countingCopyStore{mu: sync.Mutex{}, counts: orgencryption.CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 0, PlatformCopies: 0}, withValues: nil}
	runs := &recordingRunStarter{mu: sync.Mutex{}, runs: nil, actors: nil, err: nil}

	svc.SetOrganizationEncryption(OrganizationEncryption{
		Transitions: orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), keys.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond}),
		UseCases:    []orgencryption.UseCaseStores{{UseCase: testEncryptionUseCase, Stores: []orgencryption.CopyStore{store}}},
		Runs:        runs,
	})

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-encryption",
		Email:       "operator@example.test",
		OIDCSubject: "oidc-subject-encryption",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	return ctx, &encryptionFixture{keys: keys, svc: svc, conn: conn, store: store, runs: runs}
}

func (f *encryptionFixture) enroll(ctx context.Context, t *testing.T, organizationID string) *gen.AdminOrganizationEncryptionUseCase {
	t.Helper()

	view, err := f.svc.EnrollOrganizationEncryption(ctx, &gen.EnrollOrganizationEncryptionPayload{OrganizationID: organizationID, UseCase: string(testEncryptionUseCase)})
	require.NoError(t, err)

	return view
}

// readyToFinalize backdates the move to dual_write past the minimum soak and
// records a clean verification after it.
func (f *encryptionFixture) readyToFinalize(ctx context.Context, t *testing.T, organizationID string) {
	t.Helper()

	q := orgencryptionrepo.New(f.conn)
	enrolledAt := time.Now().Add(-2 * organizationEncryptionMinimumDualWrite)

	err := q.SetEncryptionTransitionStateChangedAt(ctx, orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: enrolledAt, InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: organizationID,
		UseCase:        string(testEncryptionUseCase),
	})
	require.NoError(t, err)

	verifiedAt := pgtype.Timestamptz{Time: enrolledAt.Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true}
	err = q.SetEncryptionTransitionRunTimesFixture(ctx, orgencryptionrepo.SetEncryptionTransitionRunTimesFixtureParams{
		LastBackfillStartedAt:   verifiedAt,
		LastBackfillCompletedAt: verifiedAt,
		LastVerifiedAt:          verifiedAt,
		OrganizationID:          organizationID,
		UseCase:                 string(testEncryptionUseCase),
	})
	require.NoError(t, err)
}

func (f *encryptionFixture) finalize(ctx context.Context, organizationID string, acknowledged int64) (*gen.AdminOrganizationEncryptionFinalizeResult, error) {
	return f.svc.FinalizeOrganizationEncryption(ctx, &gen.FinalizeOrganizationEncryptionPayload{
		OrganizationID:               organizationID,
		UseCase:                      string(testEncryptionUseCase),
		Confirmation:                 OrganizationEncryptionFinalizeConfirmation,
		AcknowledgedUnreadableValues: acknowledged,
	})
}

func TestOrganizationEncryption_UnavailableWithoutOrganizationEncryption(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_enc_unavailable", name: "Unavailable", slug: "enc-unavailable"})

	_, err := svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_unavailable"})
	requireOopsCode(t, err, oops.CodeUnavailable)

	_, err = svc.EnrollOrganizationEncryption(ctx, &gen.EnrollOrganizationEncryptionPayload{OrganizationID: "org_enc_unavailable", UseCase: string(testEncryptionUseCase)})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestOrganizationEncryption_GetReportsANewOrganizationInPlatformKey(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_get", name: "Get", slug: "enc-get"})
	f.store.setCounts(orgencryption.CopyCounts{Waiting: 3, Unreadable: 0, Unattributed: 0, PlatformCopies: 3})

	got, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "enc-get"})
	require.NoError(t, err)
	require.Equal(t, "org_enc_get", got.OrganizationID, "a slug resolves to the organization")
	require.Nil(t, got.Config, "no key is provisioned before enroll")
	require.True(t, got.RunsAvailable)
	require.Equal(t, OrganizationEncryptionFinalizeConfirmation, got.FinalizeConfirmation)
	require.Len(t, got.UseCases, 1, "only use cases that move to organization keys are listed")

	useCase := got.UseCases[0]
	require.Equal(t, string(testEncryptionUseCase), useCase.UseCase)
	require.Equal(t, string(orgencryption.TransitionStatePlatformKey), useCase.State)
	require.Nil(t, useCase.StateChangedAt)
	require.Equal(t, int64(3), useCase.WaitingValues, "counts are read live")
	require.Empty(t, useCase.FinalizeBlockers, "blockers are reported only in dual_write")
}

func TestOrganizationEncryption_GetUnknownOrganizationIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)

	_, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_missing"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationEncryption_EnrollProvisionsAndRecordsTheStaffMember(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_enroll", name: "Enroll", slug: "enc-enroll"})
	f.store.setCounts(orgencryption.CopyCounts{Waiting: 2, Unreadable: 0, Unattributed: 0, PlatformCopies: 2})

	view := f.enroll(ctx, t, "org_enc_enroll")
	require.Equal(t, string(orgencryption.TransitionStateDualWrite), view.State)
	require.NotNil(t, view.StateChangedAt)
	require.NotEmpty(t, view.FinalizeBlockers, "a fresh enrollment cannot be finalized")

	record, err := audittest.LatestAuditLogByAction(ctx, f.conn, audit.ActionOrganizationEncryptionTransitionEnroll)
	require.NoError(t, err)
	require.Equal(t, "org_enc_enroll", record.OrganizationID)
	require.Equal(t, "oidc-subject-encryption", record.ActorID)

	got, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_enroll"})
	require.NoError(t, err)
	require.NotNil(t, got.Config, "enroll provisions the organization's key")
	require.Equal(t, string(orgencryption.ModeSpeakeasyManaged), got.Config.Mode)
}

func TestOrganizationEncryption_RollbackReturnsToPlatformKey(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_rollback", name: "Rollback", slug: "enc-rollback"})

	_, err := f.svc.RollbackOrganizationEncryption(ctx, &gen.RollbackOrganizationEncryptionPayload{OrganizationID: "org_enc_rollback", UseCase: string(testEncryptionUseCase)})
	requireOopsCode(t, err, oops.CodeConflict)

	f.enroll(ctx, t, "org_enc_rollback")

	view, err := f.svc.RollbackOrganizationEncryption(ctx, &gen.RollbackOrganizationEncryptionPayload{OrganizationID: "org_enc_rollback", UseCase: string(testEncryptionUseCase)})
	require.NoError(t, err)
	require.Equal(t, string(orgencryption.TransitionStatePlatformKey), view.State)
}

func TestOrganizationEncryption_RefusesAUseCaseThatDoesNotMove(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_tailscale", name: "Tailscale", slug: "enc-tailscale"})

	_, err := f.svc.EnrollOrganizationEncryption(ctx, &gen.EnrollOrganizationEncryptionPayload{OrganizationID: "org_enc_tailscale", UseCase: string(orgencryption.UseCaseTailscaleGatewayState)})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationEncryption_RunNeedsAnEnrolledOrganization(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_run", name: "Run", slug: "enc-run"})
	payload := &gen.RunOrganizationEncryptionPayload{OrganizationID: "org_enc_run", UseCase: string(testEncryptionUseCase)}

	_, err := f.svc.RunOrganizationEncryption(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, f.runs.started())

	f.enroll(ctx, t, "org_enc_run")

	_, err = f.svc.RunOrganizationEncryption(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, []string{"backfill org_enc_run " + string(testEncryptionUseCase)}, f.runs.started())
}

func TestOrganizationEncryption_RunIsUnavailableWithoutTemporal(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	f.svc.orgEncryption.Runs = nil
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_norun", name: "No run", slug: "enc-norun"})
	f.enroll(ctx, t, "org_enc_norun")

	_, err := f.svc.RunOrganizationEncryption(ctx, &gen.RunOrganizationEncryptionPayload{OrganizationID: "org_enc_norun", UseCase: string(testEncryptionUseCase)})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestOrganizationEncryption_FinalizeRequiresTheConfirmationPhrase(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_phrase", name: "Phrase", slug: "enc-phrase"})
	f.enroll(ctx, t, "org_enc_phrase")
	f.readyToFinalize(ctx, t, "org_enc_phrase")

	_, err := f.svc.FinalizeOrganizationEncryption(ctx, &gen.FinalizeOrganizationEncryptionPayload{
		OrganizationID:               "org_enc_phrase",
		UseCase:                      string(testEncryptionUseCase),
		Confirmation:                 "yes",
		AcknowledgedUnreadableValues: 0,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationEncryption_FinalizeRefusesAStaleUnreadableAcknowledgement(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_stale", name: "Stale", slug: "enc-stale"})
	f.enroll(ctx, t, "org_enc_stale")
	f.readyToFinalize(ctx, t, "org_enc_stale")
	f.store.setCounts(orgencryption.CopyCounts{Waiting: 0, Unreadable: 2, Unattributed: 0, PlatformCopies: 5})

	_, err := f.finalize(ctx, "org_enc_stale", 1)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, f.runs.started(), "a refused finalize starts no clearing")

	got, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_stale"})
	require.NoError(t, err)
	require.Equal(t, string(orgencryption.TransitionStateDualWrite), got.UseCases[0].State)
}

func TestOrganizationEncryption_FinalizeMovesToOrgKMSAndStartsClearing(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_finalize", name: "Finalize", slug: "enc-finalize"})
	f.enroll(ctx, t, "org_enc_finalize")
	f.readyToFinalize(ctx, t, "org_enc_finalize")
	f.store.setCounts(orgencryption.CopyCounts{Waiting: 0, Unreadable: 2, Unattributed: 0, PlatformCopies: 5})

	got, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_finalize"})
	require.NoError(t, err)
	require.Empty(t, got.UseCases[0].FinalizeBlockers, "unreadable values do not block finalize")

	result, err := f.finalize(ctx, "org_enc_finalize", 2)
	require.NoError(t, err)
	require.Equal(t, string(orgencryption.TransitionStateOrgKMS), result.UseCase.State)
	require.True(t, result.ClearingStarted)
	require.Equal(t, []string{"clear org_enc_finalize " + string(testEncryptionUseCase)}, f.runs.started(), "finalize starts a clear, never a backfill that could already be running")
	require.Equal(t, []string{"user:oidc-subject-encryption"}, f.runs.actors, "the clear is attributed to the staff member who finalized")

	record, err := audittest.LatestAuditLogByAction(ctx, f.conn, audit.ActionOrganizationEncryptionTransitionFinalize)
	require.NoError(t, err)
	require.Equal(t, "oidc-subject-encryption", record.ActorID)

	_, err = f.svc.RollbackOrganizationEncryption(ctx, &gen.RollbackOrganizationEncryptionPayload{OrganizationID: "org_enc_finalize", UseCase: string(testEncryptionUseCase)})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = f.svc.RunOrganizationEncryption(ctx, &gen.RunOrganizationEncryptionPayload{OrganizationID: "org_enc_finalize", UseCase: string(testEncryptionUseCase)})
	require.NoError(t, err)
	require.Equal(t, "clear org_enc_finalize "+string(testEncryptionUseCase), f.runs.started()[1], "resuming an unfinished clear starts a clear")
}

func TestOrganizationEncryption_FinalizeSucceedsWhenClearingCannotStart(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	f.runs.err = errors.New("temporal unavailable")
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_nostart", name: "No start", slug: "enc-nostart"})
	f.enroll(ctx, t, "org_enc_nostart")
	f.readyToFinalize(ctx, t, "org_enc_nostart")

	result, err := f.finalize(ctx, "org_enc_nostart", 0)
	require.NoError(t, err, "the use case is in org_kms, so finalize reports success")
	require.Equal(t, string(orgencryption.TransitionStateOrgKMS), result.UseCase.State)
	require.False(t, result.ClearingStarted, "the hourly run finishes the clear")
}

func TestOrganizationEncryption_ListReportsOrganizationsByState(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_list_a", name: "List A", slug: "enc-list-a", createdAt: new(time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC))})
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_list_b", name: "List B", slug: "enc-list-b", createdAt: new(time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC))})
	f.enroll(ctx, t, "org_enc_list_b")

	platformKey := string(orgencryption.TransitionStatePlatformKey)
	list, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: &platformKey, Values: "all", Sort: "oldest", Limit: 200, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, string(testEncryptionUseCase), list.UseCase, "the first use case is listed when none is named")
	require.Equal(t, []string{string(testEncryptionUseCase)}, list.UseCases)
	ids := make([]string, 0, len(list.Organizations))
	for _, row := range list.Organizations {
		ids = append(ids, row.OrganizationID)
	}
	require.Contains(t, ids, "org_enc_list_a", "an organization never enrolled is in platform_key")
	require.NotContains(t, ids, "org_enc_list_b")

	dualWrite := string(orgencryption.TransitionStateDualWrite)
	list, err = f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: new(string(testEncryptionUseCase)), State: &dualWrite, Values: "all", Sort: "oldest", Limit: 1, Offset: 0})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.Equal(t, "org_enc_list_b", list.Organizations[0].OrganizationID)
	require.NotNil(t, list.Organizations[0].StateChangedAt)
	require.False(t, list.HasMore)

	counts := map[string]int64{}
	for _, count := range list.StateCounts {
		counts[count.State] = count.Organizations
	}
	require.Equal(t, int64(1), counts[dualWrite])
	require.GreaterOrEqual(t, counts[platformKey], int64(1))
}

func TestOrganizationEncryption_FinalizeRefusesWhileTheSoakIsNotOver(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_soak", name: "Soak", slug: "enc-soak"})
	f.enroll(ctx, t, "org_enc_soak")

	_, err := f.finalize(ctx, "org_enc_soak", 0)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, f.runs.started())
}

func TestOrganizationEncryption_FinalizeRefusesWhileTheKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_suspended", name: "Suspended", slug: "enc-suspended"})
	f.enroll(ctx, t, "org_enc_suspended")
	f.readyToFinalize(ctx, t, "org_enc_suspended")
	require.NoError(t, f.keys.Provisioner.Suspend(ctx, "org_enc_suspended", "key disabled", orgencryption.SystemActor()))

	got, err := f.svc.GetOrganizationEncryption(ctx, &gen.GetOrganizationEncryptionPayload{OrganizationID: "org_enc_suspended"})
	require.NoError(t, err)
	require.Len(t, got.UseCases[0].FinalizeBlockers, 1, "a suspended key is the only blocker")

	_, err = f.finalize(ctx, "org_enc_suspended", 0)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestOrganizationEncryption_WritesAreKeyedOnTheOrganizationIDAlone(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	seedOrg(t, ctx, f.conn, orgFixture{id: "org_enc_slug", name: "Slug", slug: "enc-slug"})

	_, err := f.svc.EnrollOrganizationEncryption(ctx, &gen.EnrollOrganizationEncryptionPayload{OrganizationID: "enc-slug", UseCase: string(testEncryptionUseCase)})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationEncryption_ListPages(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	for i, id := range []string{"org_enc_page_a", "org_enc_page_b", "org_enc_page_c"} {
		seedOrg(t, ctx, f.conn, orgFixture{id: id, name: id, slug: id, createdAt: new(time.Date(2020, time.January, i+1, 0, 0, 0, 0, time.UTC))})
	}

	first, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: nil, Values: "all", Sort: "oldest", Limit: 2, Offset: 0})
	require.NoError(t, err)
	require.Len(t, first.Organizations, 2)
	require.True(t, first.HasMore)
	require.Equal(t, "org_enc_page_a", first.Organizations[0].OrganizationID, "oldest first")

	second, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: nil, Values: "all", Sort: "oldest", Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, "org_enc_page_c", second.Organizations[0].OrganizationID)
}

// seedOrganizationsWithValues seeds three organizations, oldest first, of which
// the two newer have values: org_enc_values_b written first and org_enc_values_c
// written last.
func (f *encryptionFixture) seedOrganizationsWithValues(ctx context.Context, t *testing.T) (time.Time, time.Time) {
	t.Helper()

	for i, id := range []string{"org_enc_values_a", "org_enc_values_b", "org_enc_values_c"} {
		seedOrg(t, ctx, f.conn, orgFixture{id: id, name: id, slug: id, createdAt: new(time.Date(2019, time.January, i+1, 0, 0, 0, 0, time.UTC))})
	}

	writtenB := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	writtenC := time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
	f.store.setOrganizationsWithValues(
		orgencryption.OrganizationValues{OrganizationID: "org_enc_values_b", Values: 3, LastWrittenAt: writtenB},
		orgencryption.OrganizationValues{OrganizationID: "org_enc_values_c", Values: 1, LastWrittenAt: writtenC},
	)

	return writtenB, writtenC
}

// requireSameInstant checks that an RFC 3339 view names want, in whatever
// time zone the database reported it.
func requireSameInstant(t *testing.T, want time.Time, got *string) {
	t.Helper()

	require.NotNil(t, got)
	parsed, err := time.Parse(time.RFC3339, *got)
	require.NoError(t, err)
	require.True(t, want.Equal(parsed), "want %s, got %s", want.Format(time.RFC3339), *got)
}

func transitionRowIDs(rows []*gen.AdminOrganizationEncryptionTransitionRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.OrganizationID)
	}

	return ids
}

func TestOrganizationEncryption_ListKeepsOrganizationsWithValues(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	writtenB, writtenC := f.seedOrganizationsWithValues(ctx, t)

	list, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: nil, Values: "present", Sort: "last_written", Limit: 200, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, []string{"org_enc_values_c", "org_enc_values_b"}, transitionRowIDs(list.Organizations), "only organizations with values, most recently written first")
	require.Equal(t, int64(1), list.Organizations[0].ValueCount)
	requireSameInstant(t, writtenC, list.Organizations[0].LastWrittenAt)
	require.Equal(t, int64(3), list.Organizations[1].ValueCount)
	requireSameInstant(t, writtenB, list.Organizations[1].LastWrittenAt)
	require.False(t, list.HasMore)

	require.Len(t, list.StateCounts, 1, "the counts cover only organizations with values")
	require.Equal(t, string(orgencryption.TransitionStatePlatformKey), list.StateCounts[0].State)
	require.Equal(t, int64(2), list.StateCounts[0].Organizations)
}

func TestOrganizationEncryption_ListSortsOrganizationsWithValuesOldestFirst(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	f.seedOrganizationsWithValues(ctx, t)

	list, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: nil, Values: "present", Sort: "oldest", Limit: 200, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, []string{"org_enc_values_b", "org_enc_values_c"}, transitionRowIDs(list.Organizations))
}

func TestOrganizationEncryption_ListAllPutsRecentlyWrittenOrganizationsFirst(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	f.seedOrganizationsWithValues(ctx, t)

	list, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: nil, Values: "all", Sort: "last_written", Limit: 200, Offset: 0})
	require.NoError(t, err)
	ids := transitionRowIDs(list.Organizations)
	require.GreaterOrEqual(t, len(ids), 3)
	require.Equal(t, []string{"org_enc_values_c", "org_enc_values_b"}, ids[:2], "organizations with values lead, most recently written first")
	require.Contains(t, ids[2:], "org_enc_values_a", "an organization without values follows")

	for _, row := range list.Organizations[2:] {
		require.Zero(t, row.ValueCount)
		require.Nil(t, row.LastWrittenAt)
	}

	var total int64
	for _, count := range list.StateCounts {
		total += count.Organizations
	}
	require.False(t, list.HasMore)
	require.Equal(t, int64(len(list.Organizations)), total, "the counts cover every organization the list shows")
}

func TestOrganizationEncryption_ListFiltersOrganizationsWithValuesByState(t *testing.T) {
	t.Parallel()

	ctx, f := newEncryptionFixture(t)
	f.seedOrganizationsWithValues(ctx, t)
	f.enroll(ctx, t, "org_enc_values_b")

	dualWrite := string(orgencryption.TransitionStateDualWrite)
	list, err := f.svc.ListOrganizationEncryptionTransitions(ctx, &gen.ListOrganizationEncryptionTransitionsPayload{UseCase: nil, State: &dualWrite, Values: "present", Sort: "last_written", Limit: 200, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, []string{"org_enc_values_b"}, transitionRowIDs(list.Organizations))

	counts := map[string]int64{}
	for _, count := range list.StateCounts {
		counts[count.State] = count.Organizations
	}
	require.Equal(t, map[string]int64{dualWrite: 1, string(orgencryption.TransitionStatePlatformKey): 1}, counts)
}
