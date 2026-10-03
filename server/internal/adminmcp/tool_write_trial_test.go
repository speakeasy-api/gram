package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func newTrialWriter(f proposalFixture) (*trialWriter, *writeTools) {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationExtendOrganizationTrial: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	writer := &trialWriter{store: f.store, audit: audit.NewLogger(), writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationExtendOrganizationTrial: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	return writer, tools
}

// seedTrial stores an enterprise trial ending at endsAt, truncated to the
// microsecond precision Postgres keeps.
func seedTrial(t *testing.T, f proposalFixture, organizationID string, endsAt time.Time, converted bool) time.Time {
	t.Helper()
	endsAt = endsAt.UTC().Truncate(time.Microsecond)
	convertedAt := pgtype.Timestamptz{}
	if converted {
		convertedAt = conv.ToPGTimestamptz(time.Now())
	}
	require.NoError(t, trialsrepo.New(f.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID, Tier: "enterprise", CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-24 * time.Hour)), EndsAt: conv.ToPGTimestamptz(endsAt), ConvertedAt: convertedAt, DemotedAt: pgtype.Timestamptz{},
	}))
	return endsAt
}

func trialEndsAt(t *testing.T, f proposalFixture, organizationID string) time.Time {
	t.Helper()
	trial, err := trialsrepo.New(f.db).GetTrial(t.Context(), organizationID)
	require.NoError(t, err)
	return trial.EndsAt.Time.UTC()
}

func approveTrial(t *testing.T, f proposalFixture, writer *trialWriter, proposalID string) {
	t.Helper()
	p, err := f.store.GetForOwner(t.Context(), uuid.MustParse(proposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
}

func TestTrialExtensionWriteApprovalAndExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_trial_write")
	writer, tools := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	neighbourEndsAt := seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), false)

	for _, bad := range []PrepareTrialExtensionInput{
		{OrganizationID: f.orgA, Days: 14},
		{OrganizationID: "synthetic-a", Days: 14, RetryKey: "slug"},
		{OrganizationID: f.orgA, Days: 0, RetryKey: "zero"},
		{OrganizationID: f.orgA, Days: -7, RetryKey: "negative"},
		{OrganizationID: f.orgA, Days: 366, RetryKey: "too-many"},
		{OrganizationID: f.orgA, Days: 1<<32 + 1, RetryKey: "wraps-to-one"},
	} {
		_, err := writer.prepare(ctx, bad)
		require.Error(t, err, "prepare must refuse input %q", bad.RetryKey)
	}

	input := PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 14, RetryKey: "trial-1"}
	prepared, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "https://staff.example.test"+Path+"/proposals/"+prepared.ProposalID, prepared.ApprovalURL)
	var preview trialPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, 14, preview.Days)
	require.Equal(t, trialTimestamp(endsAt), preview.EndsAt)
	require.Equal(t, trialTimestamp(endsAt.AddDate(0, 0, 14)), preview.NewEndsAt)
	require.Equal(t, trialSideEffects, preview.SideEffects)
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA), "prepare does not write")

	replay, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, prepared.ProposalID, replay.ProposalID)
	input.Days = 15
	_, err = writer.prepare(ctx, input)
	require.ErrorIs(t, err, ErrProposalConflict)

	// A second extension prepared from the same trial state, approved now,
	// and a third left pending. Both must go stale once the first executes.
	second, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 7, RetryKey: "second-extension"})
	require.NoError(t, err)
	approveTrial(t, f, writer, second.ProposalID)
	third, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 3, RetryKey: "third-extension"})
	require.NoError(t, err)

	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)

	id := uuid.MustParse(prepared.ProposalID)
	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	memory := testenv.NewMemoryCache()
	approval := newStaffProposalApproval(f.store, NewStaffOAuthAuthorization(postgresStaffClientStore{db: f.db}, postgresStaffAuthorizationStore{db: f.db}, memory, &fakeAdminVerifier{result: staff}, f.cipher, staffAudience), memory, writer.writes)
	approval.operations = map[WriteOperation]approvableOperation{OperationExtendOrganizationTrial: writer} //nolint:exhaustive // Only selected write operations are enabled by this test.
	handler := middleware.AdminOriginCheck(nil)(approval.Handler())
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, approvalRequest(http.MethodGet, id, nil, "browser-session"))
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "Synthetic A")
	require.Contains(t, page.Body.String(), "Extend the enterprise trial by 14 days")
	const display = "2 Jan 2006 15:04 UTC"
	require.Contains(t, page.Body.String(), "<td>Trial end date</td><td>"+endsAt.Format(display)+"</td><td>"+endsAt.AddDate(0, 0, 14).Format(display)+"</td>")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, approvalRequest(http.MethodPost, id, approvalForm(t, page.Body.String()), "browser-session"))
	require.Equal(t, http.StatusOK, accepted.Code)

	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	var receipt trialReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, trialReceipt{PreviousEndsAt: trialTimestamp(endsAt), EndsAt: trialTimestamp(endsAt.AddDate(0, 0, 14))}, receipt)
	require.Equal(t, endsAt.AddDate(0, 0, 14), trialEndsAt(t, f, f.orgA))
	require.Equal(t, neighbourEndsAt, trialEndsAt(t, f, f.orgB), "execution cannot affect a second tenant")
	trial, err := trialsrepo.New(f.db).GetTrial(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, trial.ConvertedAt.Valid)
	require.False(t, trial.DemotedAt.Valid)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface, "customer audit feeds and the outbox mask admin_mcp actors")
	require.NotNil(t, entry.ActingClientID)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))

	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, endsAt.AddDate(0, 0, 14), trialEndsAt(t, f, f.orgA), "a replay does not extend again")
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))

	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: second.ProposalID})
	require.ErrorIs(t, err, ErrStaleState, "an extension prepared before another one executed is stale")
	pending, err := f.store.GetForOwner(t.Context(), uuid.MustParse(third.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), pending.ID, f.owner.SubjectURN, pending.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	require.Equal(t, endsAt.AddDate(0, 0, 14), trialEndsAt(t, f, f.orgA))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))

	otherPrincipal, ok := ctx.Value(principalKey{}).(Principal)
	require.True(t, ok)
	otherPrincipal.Subject = "user:other-staff"
	_, err = tools.execute(context.WithValue(ctx, principalKey{}, otherPrincipal), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotFound)
}

func TestTrialExtensionStaleAfterDemotionOrConversion(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_trial_stale")
	writer, tools := newTrialWriter(f)
	ctx := writeContext(t, f)
	demotedEndsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	convertedEndsAt := seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), false)

	demoted, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 14, RetryKey: "demoted"})
	require.NoError(t, err)
	approveTrial(t, f, writer, demoted.ProposalID)
	demotedPending, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 7, RetryKey: "demoted-pending"})
	require.NoError(t, err)
	converted, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgB, Days: 14, RetryKey: "converted"})
	require.NoError(t, err)
	approveTrial(t, f, writer, converted.ProposalID)

	rows, err := trialsrepo.New(f.db).DemoteTrialFixture(t.Context(), f.orgA)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	rows, err = trialsrepo.New(f.db).MarkTrialConverted(t.Context(), f.orgB)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: demoted.ProposalID})
	require.ErrorIs(t, err, ErrStaleState, "a demoted trial is not extended")
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: converted.ProposalID})
	require.ErrorIs(t, err, ErrStaleState, "a converted trial is not extended")
	pending, err := f.store.GetForOwner(t.Context(), uuid.MustParse(demotedPending.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), pending.ID, f.owner.SubjectURN, pending.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)

	require.Equal(t, demotedEndsAt, trialEndsAt(t, f, f.orgA))
	require.Equal(t, convertedEndsAt, trialEndsAt(t, f, f.orgB))
	for _, id := range []string{demoted.ProposalID, converted.ProposalID} {
		closed, err := f.store.GetForOwner(t.Context(), uuid.MustParse(id), f.owner)
		require.NoError(t, err)
		require.Equal(t, ProposalInvalidated, closed.Status)
	}
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
}

func TestTrialExtensionRefusesTrialsThatAreNotRunning(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_trial_not_running")
	writer, _ := newTrialWriter(f)
	ctx := writeContext(t, f)

	_, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 14, RetryKey: "no-trial"})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	seedTrial(t, f, f.orgA, time.Now().Add(-time.Hour), false)
	_, err = writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 14, RetryKey: "expired"})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), true)
	_, err = writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgB, Days: 14, RetryKey: "converted"})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	_, err = writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: "org_missing_" + uuid.NewString(), Days: 14, RetryKey: "missing"})
	require.Error(t, err)
}

// Concurrent executes of one approved proposal extend the trial once.
func TestTrialExtensionConcurrentExecutesWriteOnce(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_trial_concurrent")
	writer, tools := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	prepared, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 30, RetryKey: "concurrent"})
	require.NoError(t, err)
	approveTrial(t, f, writer, prepared.ProposalID)

	type outcome struct {
		out ProposalOutput
		err error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			out, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			results <- outcome{out: out, err: err}
		}()
	}
	fresh := 0
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		require.Equal(t, string(ProposalSucceeded), got.out.Status)
		if !got.out.Replay {
			fresh++
		}
	}
	require.Equal(t, 1, fresh, "exactly one execute writes; the other replays the receipt")
	require.Equal(t, endsAt.AddDate(0, 0, 30), trialEndsAt(t, f, f.orgA))
	require.Equal(t, 1, countWriteEvents(t, f.db, uuid.MustParse(prepared.ProposalID), "executed"))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
}

func TestTrialExtensionAuditFailureAndTamperedDays(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_trial_rollback")
	writer, tools := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)

	// Stored days outside the bounds can only come from direct database
	// access. Execution refuses them even with approval bypassed.
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: Exercise the transaction-bound state reader with a synthetic database.
	require.NoError(t, err)
	state, _, _, err := writer.readState(t.Context(), tx, f.orgA)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	expected, err := json.Marshal(state)
	require.NoError(t, err)
	for key, arguments := range map[string]string{"zero": `{"days":0}`, "too-many": `{"days":366}`, "negative": `{"days":-30}`, "missing": `{}`} {
		input := featureProposal(f.orgA, "tampered-"+key, true)
		input.Operation = OperationExtendOrganizationTrial
		input.Arguments = json.RawMessage(arguments)
		input.ExpectedState = expected
		p, _, err := f.store.Create(t.Context(), f.owner, input, time.Now())
		require.NoError(t, err)
		_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, allowProposalBrowser)
		require.NoError(t, err)
		_, err = tools.execute(ctx, ProposalIDInput{ProposalID: p.ID.String()})
		require.ErrorIs(t, err, ErrProposalInvalidated, key)
	}
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA))

	prepared, err := writer.prepare(ctx, PrepareTrialExtensionInput{OrganizationID: f.orgA, Days: 14, RetryKey: "rollback"})
	require.NoError(t, err)
	approveTrial(t, f, writer, prepared.ProposalID)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationEnterpriseTrialExtended))
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA), "the extension rolls back with the failed audit")
	id := uuid.MustParse(prepared.ProposalID)
	still, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, still.Status, "a failed attempt does not consume the approval")
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "execute_refused"))
	require.Zero(t, countWriteEvents(t, f.db, id, "executed"))
}

func TestTrialViewRequiresMatchingTarget(t *testing.T) {
	t.Parallel()
	writer := &trialWriter{}
	endsAt := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	preview, err := json.Marshal(trialPreview{OrganizationID: "org_b", Name: "B", Slug: "b", Days: 1, EndsAt: trialTimestamp(endsAt), NewEndsAt: trialTimestamp(endsAt.AddDate(0, 0, 1))})
	require.NoError(t, err)

	_, err = writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_a"}, Preview: preview})
	require.ErrorIs(t, err, ErrProposalInvalidated, "a preview naming another organisation is never shown")

	view, err := writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_b"}, Preview: preview})
	require.NoError(t, err)
	require.Equal(t, "Extend the enterprise trial by 1 day", view.Summary)
	require.Equal(t, []proposalViewChange{{Setting: "Trial end date", Before: "1 Oct 2026 09:30 UTC", After: "2 Oct 2026 09:30 UTC"}}, view.Changes)
}
