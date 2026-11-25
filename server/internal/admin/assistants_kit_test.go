package admin

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// identifiedAssistant creates an assistant with a usable dedicated agent.
func (o *assistantMigrationOrg) identifiedAssistant(identities *assistantidentity.Service, name, owner string) uuid.UUID {
	o.t.Helper()
	id := o.legacyAssistant(name, owner)
	tx, err := o.db.Begin(o.t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; provisioning uses generated SQLc methods.
	require.NoError(o.t, err)
	defer func() { _ = tx.Rollback(o.t.Context()) }()
	require.NoError(o.t, identities.Provision(o.t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: o.id, ProjectID: o.project, AssistantID: id, ActorUserID: owner, Staff: nil}))
	require.NoError(o.t, tx.Commit(o.t.Context()))
	return id
}

// sharedIssuer gives the project the shared user-session issuer Kit mints
// runtime tokens from.
func (o *assistantMigrationOrg) sharedIssuer() {
	o.t.Helper()
	_, err := usersessionsrepo.New(o.db).CreateUserSessionIssuer(o.t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: o.project, OrganizationID: pgtype.Text{String: o.id, Valid: true}, Slug: "shared-" + uuid.NewString()[:8],
		AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		AuthorizationServerMode: pgtype.Text{String: "shared", Valid: true}, UseAuthenticationHost: pgtype.Bool{},
	})
	require.NoError(o.t, err)
}

// runtime seeds one live per-thread legacy runtime of the assistant.
func (o *assistantMigrationOrg) runtime(assistantID uuid.UUID, state string, warmUntil time.Time) uuid.UUID {
	o.t.Helper()
	id := uuid.New()
	now := time.Now()
	require.NoError(o.t, assistantrepo.New(o.db).CreateAssistantRuntime(o.t.Context(), assistantrepo.CreateAssistantRuntimeParams{
		ID: id, AssistantThreadID: uuid.New(), AssistantID: assistantID, ProjectID: o.project, Backend: "fly",
		BackendMetadataJson: []byte("{}"), State: state, WarmUntil: pgtype.Timestamptz{Time: warmUntil, Valid: true},
		LastHeartbeatAt: pgtype.Timestamptz{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		EndedAt: pgtype.Timestamptz{}, DeletedAt: pgtype.Timestamptz{},
	}))
	return id
}

// pendingEvent queues an unprocessed event on a thread of the assistant.
func (o *assistantMigrationOrg) pendingEvent(assistantID uuid.UUID) {
	o.t.Helper()
	chatID, err := chatrepo.New(o.db).UpsertChat(o.t.Context(), chatrepo.UpsertChatParams{
		ID: uuid.New(), ProjectID: o.project, OrganizationID: o.id,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{}, Title: pgtype.Text{String: "Pending", Valid: true},
	})
	require.NoError(o.t, err)
	queries := assistantrepo.New(o.db)
	threadID, err := queries.UpsertAssistantThread(o.t.Context(), assistantrepo.UpsertAssistantThreadParams{
		AssistantID: assistantID, ProjectID: o.project, CorrelationID: "corr-" + uuid.NewString(), ChatID: chatID, SourceKind: "cron", SourceRefJson: []byte("{}"),
	})
	require.NoError(o.t, err)
	_, err = queries.InsertAssistantThreadEvent(o.t.Context(), assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID: threadID, AssistantID: assistantID, ProjectID: o.project, TriggerInstanceID: uuid.NullUUID{},
		EventID: "event-" + uuid.NewString(), CorrelationID: "corr-" + uuid.NewString(), Status: "pending",
		NormalizedPayloadJson: []byte("{}"), SourcePayloadJson: []byte("{}"),
	})
	require.NoError(o.t, err)
}

func (o *assistantMigrationOrg) runtimeVersion(assistantID uuid.UUID) int16 {
	o.t.Helper()
	row, err := assistantrepo.New(o.db).GetAssistant(o.t.Context(), assistantrepo.GetAssistantParams{ProjectID: o.project, AssistantID: assistantID})
	require.NoError(o.t, err)
	return row.RuntimeVersion
}

func kitOutcomes(run *gen.AdminAssistantKitMigrationRun) map[string]string {
	outcomes := make(map[string]string, len(run.Results))
	for _, result := range run.Results {
		outcomes[result.AssistantID] = result.Outcome
	}
	return outcomes
}

func TestMigrateOrganizationAssistantsToKit(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "staff@example.invalid", Name: "Staff Member", HD: testAdminHD})
	past, recent, future := time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), time.Now().Add(time.Hour)

	org := newAssistantMigrationOrg(t, db, "assistant-kit-migration")
	org.sharedIssuer()
	owner := org.member(true)
	idle := org.identifiedAssistant(svc.assistantIdentities, "Idle", owner)
	idleStopped := org.runtime(idle, "stopped", past)
	idleExpired := org.runtime(idle, "active", past)
	fresh := org.identifiedAssistant(svc.assistantIdentities, "Fresh", owner)
	warm := org.identifiedAssistant(svc.assistantIdentities, "Warm", owner)
	warmRuntime := org.runtime(warm, "active", future)
	expiring := org.identifiedAssistant(svc.assistantIdentities, "Expiring", owner)
	org.runtime(expiring, "active", recent)
	working := org.identifiedAssistant(svc.assistantIdentities, "Working", owner)
	org.pendingEvent(working)
	legacy := org.legacyAssistant("Legacy", owner)

	off := org.identifiedAssistant(svc.assistantIdentities, "Reasoning Off", owner)
	offRuntime := org.runtime(off, "active", past)
	queries := assistantrepo.New(db)
	_, err := queries.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{ProjectID: org.project, AssistantID: off, ReasoningEffort: pgtype.Text{String: "none", Valid: true}})
	require.NoError(t, err)
	offBefore, err := queries.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: org.project, ID: offRuntime})
	require.NoError(t, err)

	other := newAssistantMigrationOrg(t, db, "assistant-kit-migration-other")
	other.sharedIssuer()
	untouched := other.identifiedAssistant(svc.assistantIdentities, "Other organization", other.member(true))

	first, err := svc.MigrateOrganizationAssistantsToKit(ctx, &gen.MigrateOrganizationAssistantsToKitPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		idle.String():     string(assistants.KitSwitchSwitched),
		fresh.String():    string(assistants.KitSwitchSwitched),
		warm.String():     string(assistants.KitSwitchBusy),
		expiring.String(): string(assistants.KitSwitchBusy),
		working.String():  string(assistants.KitSwitchBusy),
		legacy.String():   string(assistants.KitSwitchNeedsIdentity),
		off.String():      string(assistants.KitSwitchIneligible),
	}, kitOutcomes(first))
	require.Equal(t, int64(2), first.Switched)
	require.Equal(t, int64(3), first.Busy)
	require.Equal(t, int64(1), first.NeedsIdentity)
	require.EqualValues(t, 1, first.Ineligible)
	require.Zero(t, first.Failed, "unsupported reasoning is an eligibility refusal, not a retryable failure")

	require.Equal(t, int16(3), org.runtimeVersion(idle))
	require.Equal(t, int16(3), org.runtimeVersion(fresh))
	require.Equal(t, int16(2), org.runtimeVersion(warm))
	require.Equal(t, int16(2), org.runtimeVersion(working))
	require.Equal(t, int16(2), org.runtimeVersion(legacy))
	require.Equal(t, int16(2), org.runtimeVersion(off))
	offAfter, err := queries.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: org.project, ID: offRuntime})
	require.NoError(t, err)
	require.Equal(t, offBefore, offAfter)
	for _, id := range []uuid.UUID{idleStopped, idleExpired} {
		row, err := queries.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: org.project, ID: id})
		require.NoError(t, err)
		require.Equal(t, "switch_retiring", row.State, "the janitor stops retired runtimes")
		require.True(t, row.Ended)
	}
	warmRow, err := queries.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ProjectID: org.project, ID: warmRuntime})
	require.NoError(t, err)
	require.Equal(t, "active", warmRow.State, "a runtime inside its warm window is left alone")

	switched, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), db, audit.ActionAssistantRuntimeSwitch, idle.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", switched.ActorID, "the staff member is the audit actor")
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAssistantRuntimeSwitch)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, int64(2))

	again, err := svc.MigrateOrganizationAssistantsToKit(ctx, &gen.MigrateOrganizationAssistantsToKitPayload{OrganizationID: org.id, AssistantIds: []string{idle.String(), fresh.String()}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{idle.String(): string(assistants.KitSwitchAlreadyKit), fresh.String(): string(assistants.KitSwitchAlreadyKit)}, kitOutcomes(again))
	require.Equal(t, int64(2), again.AlreadyKit)

	none, err := svc.MigrateOrganizationAssistantsToKit(ctx, &gen.MigrateOrganizationAssistantsToKitPayload{OrganizationID: org.id, AssistantIds: []string{}})
	require.NoError(t, err)
	require.Empty(t, none.Results, "an empty selection moves nothing")
	require.Equal(t, int16(2), org.runtimeVersion(warm))

	require.Equal(t, int16(2), other.runtimeVersion(untouched), "another organization is untouched")

	_, err = svc.MigrateOrganizationAssistantsToKit(ctx, &gen.MigrateOrganizationAssistantsToKitPayload{OrganizationID: org.id, AssistantIds: []string{untouched.String()}})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code, "an assistant of another organization cannot be selected")
	require.Equal(t, int16(2), other.runtimeVersion(untouched))
}

func TestMigrateOrganizationAssistantsToKitIneligibleWithoutSharedIssuer(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "", Name: "", HD: testAdminHD})

	org := newAssistantMigrationOrg(t, db, "assistant-kit-ineligible")
	assistant := org.identifiedAssistant(svc.assistantIdentities, "No issuer", org.member(true))

	run, err := svc.MigrateOrganizationAssistantsToKit(ctx, &gen.MigrateOrganizationAssistantsToKitPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, map[string]string{assistant.String(): string(assistants.KitSwitchIneligible)}, kitOutcomes(run))
	require.Equal(t, int16(2), org.runtimeVersion(assistant))
}
