package triggers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func (f identityFixture) resolve(t *testing.T, assistantID, triggerID uuid.UUID) assistantidentity.Resolution {
	t.Helper()
	result, err := testIdentityService.Resolve(t.Context(), f.db, "org-trigger-test", f.projectID, assistantID, triggerID)
	require.NoError(t, err)
	return result
}

func (f identityFixture) bound(t *testing.T, triggerID uuid.UUID) bool {
	t.Helper()
	_, err := identityrepo.New(f.db).GetTriggerBinding(t.Context(), identityrepo.GetTriggerBindingParams{ProjectID: f.projectID, TriggerID: triggerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

func (f identityFixture) retarget(t *testing.T, item triggerrepo.TriggerInstance, assistantID uuid.UUID) {
	t.Helper()
	_, err := f.app.Update(t.Context(), triggers.UpdateParams{ID: item.ID, ProjectID: f.projectID, DefinitionSlug: item.DefinitionSlug, Name: item.Name, EnvironmentID: item.EnvironmentID, TargetKind: item.TargetKind, TargetRef: assistantID.String(), TargetDisplay: item.TargetDisplay, Config: map[string]any{}, Status: item.Status})
	require.NoError(t, err)
}

func TestRootTriggerBindsRetargetsAndWithdraws(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	item, err := f.app.Create(t.Context(), f.createParams())
	require.NoError(t, err)
	first := f.resolve(t, f.assistantID, item.ID)
	require.Equal(t, assistantidentity.Active, first.State)

	next := f.createAssistant(t, true)
	f.retarget(t, item, next)
	moved := f.resolve(t, next, item.ID)
	require.Equal(t, assistantidentity.Active, moved.State)
	require.Equal(t, first.Identity.Subject, moved.Identity.Subject)
	require.NotEqual(t, first.Identity.AgentID, moved.Identity.AgentID)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, *first.Identity), assistantidentity.ErrInvalidIdentity)

	legacy := f.createAssistant(t, false)
	f.retarget(t, item, legacy)
	require.False(t, f.bound(t, item.ID))
	require.Equal(t, assistantidentity.NeverConfigured, f.resolve(t, legacy, item.ID).State)

	f.retarget(t, item, next)
	require.True(t, f.bound(t, item.ID))
	require.NoError(t, f.app.Delete(t.Context(), f.projectID, item.ID))
	require.False(t, f.bound(t, item.ID))
	require.Equal(t, assistantidentity.Unavailable, f.resolve(t, next, item.ID).State)

	for _, tc := range []struct {
		action audit.Action
		want   int64
	}{
		{action: audit.ActionWorkloadIssuerCreate, want: 1},
		{action: audit.ActionWorkloadAdmissionAdmit, want: 3},
		{action: audit.ActionWorkloadAdmissionWithdraw, want: 3},
	} {
		count, err := audittest.AuditLogCountByAction(t.Context(), f.db, tc.action)
		require.NoError(t, err)
		require.Equal(t, tc.want, count, tc.action)
		latest, err := audittest.LatestAuditLogByAction(t.Context(), f.db, tc.action)
		require.NoError(t, err)
		require.Equal(t, agentownership.SystemActor.ID, latest.ActorID, "unauthenticated trigger changes are attributed to the system")
	}
}

func TestRootTriggerPauseKeepsIdentity(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	params := f.createParams()
	params.Status = triggers.StatusPaused
	item, err := f.app.Create(t.Context(), params)
	require.NoError(t, err)
	paused := f.resolve(t, f.assistantID, item.ID)
	require.Equal(t, assistantidentity.Active, paused.State)
	_, err = f.app.SetStatus(t.Context(), f.projectID, item.ID, triggers.StatusActive)
	require.NoError(t, err)
	require.Equal(t, paused, f.resolve(t, f.assistantID, item.ID))
}

func TestRootTriggerCreateCompensationWithdrawsIdentity(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	params := f.createParams()
	params.DefinitionSlug = triggers.DefinitionSlugCron
	params.Config = map[string]any{"schedule": "0 * * * *"}
	var triggerID uuid.UUID
	_, err := f.app.Create(t.Context(), params, func(_ context.Context, _ pgx.Tx, item triggerrepo.TriggerInstance) error {
		triggerID = item.ID
		return nil
	})
	require.Error(t, err, "no Temporal client forces post-commit schedule compensation")
	require.NotEqual(t, uuid.Nil, triggerID)
	require.False(t, f.bound(t, triggerID))
}

func TestContinuationWakeIsNeverBound(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	var wakeID uuid.UUID
	_, err := f.app.CreateWakeInstance(t.Context(), triggers.CreateWakeInstanceParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, Name: "Follow up", AssistantID: f.assistantID, TargetDisplay: "Assistant", FireAt: time.Now().Add(time.Hour), Note: nil, CorrelationID: "thread-identity"}, func(ctx context.Context, tx pgx.Tx, item triggerrepo.TriggerInstance) error {
		wakeID = item.ID
		return testIdentityService.BindRootTrigger(ctx, tx, item.ProjectID, item.ID)
	})
	require.Error(t, err, "no Temporal client forces workflow compensation")
	require.NotEqual(t, uuid.Nil, wakeID)
	require.False(t, f.bound(t, wakeID))
}

func TestRootTriggerManagementToleratesUserWorkloadEdits(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	item, err := f.app.Create(ctx, f.createParams())
	require.NoError(t, err)
	resolved := f.resolve(t, f.assistantID, item.ID)
	require.Equal(t, assistantidentity.Active, resolved.State)

	// A user withdraws the issuer through the workload identity surface.
	workloads := workloadrepo.New(f.db)
	_, err = workloads.SoftDeleteWorkloadIssuer(ctx, workloadrepo.SoftDeleteWorkloadIssuerParams{OrganizationID: "org-trigger-test", ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ID: resolved.Identity.IssuerID})
	require.NoError(t, err)
	_, err = workloads.SoftDeleteWorkloadAdmissionsByIssuer(ctx, workloadrepo.SoftDeleteWorkloadAdmissionsByIssuerParams{OrganizationID: "org-trigger-test", WorkloadIssuerID: resolved.Identity.IssuerID})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Unavailable, f.resolve(t, f.assistantID, item.ID).State)

	_, err = f.app.SetStatus(ctx, f.projectID, item.ID, triggers.StatusPaused)
	require.NoError(t, err)
	next := f.createAssistant(t, true)
	f.retarget(t, item, next)
	require.Equal(t, assistantidentity.Active, f.resolve(t, next, item.ID).State)
	require.NoError(t, f.app.Delete(ctx, f.projectID, item.ID))
}

func TestWakeCapturesRequesterOrOwnerAtCreation(t *testing.T) {
	t.Parallel()
	for _, requester := range []string{"requester", ""} {
		t.Run("requester="+requester, func(t *testing.T) {
			t.Parallel()
			f := newIdentityFixture(t)
			ctx := t.Context()
			if requester != "" {
				ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "org-trigger-test", UserID: requester})
			}
			_, err := f.app.CreateWakeInstance(ctx, triggers.CreateWakeInstanceParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, AssistantID: f.assistantID, Name: "Wake", TargetDisplay: "Assistant", FireAt: time.Now().Add(time.Hour), CorrelationID: "thread"}, func(_ context.Context, _ pgx.Tx, item triggerrepo.TriggerInstance) error {
				var config struct {
					Requester string `json:"requester_user_id"`
					Version   int    `json:"identity_version"`
				}
				require.NoError(t, json.Unmarshal(item.ConfigJson, &config))
				expected := requester
				if expected == "" {
					expected = "trigger-owner"
				}
				require.Equal(t, expected, config.Requester)
				require.Equal(t, 1, config.Version)
				return errors.New("captured wake identity")
			})
			require.ErrorContains(t, err, "captured wake identity")
		})
	}
}

func (f identityFixture) provisionTx(t *testing.T, tx pgx.Tx, assistantID uuid.UUID) error {
	t.Helper()
	if err := testIdentityService.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, AssistantID: assistantID, ActorUserID: "trigger-owner"}); err != nil {
		return fmt.Errorf("provision assistant identity: %w", err)
	}
	return nil
}

func TestTriggerCreateDuringUpgradeIsBound(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	legacy := f.createAssistant(t, false)
	params := f.createParams()
	params.TargetRef = legacy.String()

	upgrade := testenv.BeginTx(t, ctx, f.db)
	require.NoError(t, f.provisionTx(t, upgrade, legacy))
	var item triggerrepo.TriggerInstance
	var group errgroup.Group
	group.Go(func() error {
		var err error
		item, err = f.app.Create(ctx, params)
		if err != nil {
			return fmt.Errorf("create trigger during upgrade: %w", err)
		}
		return nil
	})
	testenv.WaitForQueryBlockedBy(t, ctx, f.db, testenv.BackendPID(upgrade), "%ShareLockAssistant :one%")
	require.NoError(t, upgrade.Commit(ctx))
	require.NoError(t, group.Wait())
	require.True(t, f.bound(t, item.ID))
	require.Equal(t, assistantidentity.Active, f.resolve(t, legacy, item.ID).State)
}

func TestUpgradeDuringTriggerCreateBindsIt(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	legacy := f.createAssistant(t, false)
	params := f.createParams()
	params.TargetRef = legacy.String()

	holding := make(chan uint32, 1)
	release := make(chan struct{})
	t.Cleanup(sync.OnceFunc(func() { close(release) }))
	createDone := make(chan error, 1)
	var item triggerrepo.TriggerInstance
	go func() {
		var err error
		item, err = f.app.Create(ctx, params, func(_ context.Context, tx pgx.Tx, _ triggerrepo.TriggerInstance) error {
			holding <- testenv.BackendPID(tx)
			<-release
			return nil
		})
		createDone <- err
	}()
	var creator uint32
	select {
	case creator = <-holding:
	case err := <-createDone:
		require.FailNow(t, "create finished before holding its lock", "error: %v", err)
	}

	var group errgroup.Group
	group.Go(func() error {
		tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the upgrade uses the identity service.
		if err != nil {
			return fmt.Errorf("begin upgrade: %w", err)
		}
		defer o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) })
		if err := f.provisionTx(t, tx, legacy); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit upgrade: %w", err)
		}
		return nil
	})
	testenv.WaitForQueryBlockedBy(t, ctx, f.db, creator, "%name: LockAssistant :one%")
	release <- struct{}{}
	require.NoError(t, <-createDone)
	require.NoError(t, group.Wait())
	require.True(t, f.bound(t, item.ID))
	require.Equal(t, assistantidentity.Active, f.resolve(t, legacy, item.ID).State)
}
