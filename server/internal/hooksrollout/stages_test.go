package hooksrollout_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
)

func TestInStage_IsStableAndGrows(t *testing.T) {
	t.Parallel()

	cleared := 0
	for i := range 1000 {
		orgID := fmt.Sprintf("org-%d", i)
		require.False(t, hooksrollout.InStage(orgID, 0))
		require.True(t, hooksrollout.InStage(orgID, 100))
		if hooksrollout.InStage(orgID, 25) {
			cleared++
			require.True(t, hooksrollout.InStage(orgID, 26), "a cleared organization stays cleared as the share grows")
		}
	}
	require.InDelta(t, 250, cleared, 60)
}

func TestEffective_StageClearsBucketedOrganizations(t *testing.T) {
	t.Parallel()

	inside, outside := bucketedOrganizations(t, 25)
	defaultPin := &hooksrollout.Pin{Version: 40, SetBy: operator, SetAt: time.Time{}}
	stage := &hooksrollout.Stage{Version: 42, Percent: 25, State: hooksrollout.StageRunning, Reason: "", SetBy: operator, SetAt: time.Time{}}
	pins := hooksrollout.OrganizationPins{Default: defaultPin, Stage: stage}

	pin, source := pins.Effective(inside)
	require.Equal(t, hooksrollout.SourceStage, source)
	require.Equal(t, 42, pin.Version)

	pin, source = pins.Effective(outside)
	require.Equal(t, hooksrollout.SourceDefault, source)
	require.Equal(t, 40, pin.Version)

	stage.State = hooksrollout.StagePaused
	_, source = pins.Effective(inside)
	require.Equal(t, hooksrollout.SourceStage, source, "a paused rollout keeps the organizations it cleared")

	for _, state := range []hooksrollout.StageState{hooksrollout.StageCancelled, hooksrollout.StageCompleted} {
		stage.State = state
		_, source = pins.Effective(inside)
		require.Equal(t, hooksrollout.SourceDefault, source, state)
	}

	stage.State = hooksrollout.StageRunning
	pins.Default = &hooksrollout.Pin{Version: 42, SetBy: operator, SetAt: time.Time{}}
	_, source = pins.Effective(inside)
	require.Equal(t, hooksrollout.SourceDefault, source, "a stage no newer than the default pin has no effect")

	pins.Override = &hooksrollout.Pin{Version: 39, SetBy: operator, SetAt: time.Time{}}
	pin, source = pins.Effective(inside)
	require.Equal(t, hooksrollout.SourceOrganization, source)
	require.Equal(t, 39, pin.Version)
}

func TestStageStore_AdvancesThroughStagesAndMovesDefaultPin(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	pins := hooksrollout.NewStore(conn)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.NoError(t, pins.SetDefault(ctx, 40, operator))
	require.ErrorIs(t, stages.Start(ctx, 0, 40, operator), hooksrollout.ErrStageVersionBehind)
	require.NoError(t, stages.Start(ctx, 0, 42, operator))

	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageRunning, current.State)
	require.Equal(t, 0, current.Percent)

	advanced, err := stages.Advance(ctx, current.SetAt.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, advanced, "nothing moves before the soak ends")

	advanced, err = stages.Advance(ctx, current.SetAt.Add(hooksrollout.StageSoak))
	require.NoError(t, err)
	require.Equal(t, 25, advanced.Percent)
	require.Equal(t, hooksrollout.StageRunning, advanced.State)

	current, err = stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageWorkflowActor, current.SetBy)
	require.Equal(t, current, advanced, "Advance reports the stored stage")

	advanced, err = stages.Advance(ctx, current.SetAt.Add(hooksrollout.StageSoak))
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageCompleted, advanced.State)

	defaultPin, err := pins.DefaultPin(ctx)
	require.NoError(t, err)
	require.Equal(t, 42, defaultPin.Version)
	require.Equal(t, hooksrollout.StageWorkflowActor, defaultPin.SetBy)

	advanced, err = stages.Advance(ctx, time.Now().Add(48*time.Hour))
	require.NoError(t, err)
	require.Nil(t, advanced)
}

func TestStageStore_StartRefusesOlderThanActive(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.NoError(t, stages.Start(ctx, 0, 42, operator))
	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, stages.Start(ctx, current.Revision, 41, operator), hooksrollout.ErrStageVersionOlder)
	require.NoError(t, stages.Start(ctx, current.Revision, 43, operator))
}

func TestStageStore_PauseResumeCancel(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.ErrorIs(t, stages.Pause(ctx, 0, "alert", operator), hooksrollout.ErrNoActiveStage)
	require.NoError(t, stages.Start(ctx, 0, 42, operator))
	require.ErrorIs(t, stages.Resume(ctx, revision(t, stages), operator), hooksrollout.ErrStageNotPaused)

	require.NoError(t, stages.Pause(ctx, revision(t, stages), "delivery errors climbing", operator))
	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StagePaused, current.State)
	require.Equal(t, "delivery errors climbing", current.Reason)

	advanced, err := stages.Advance(ctx, current.SetAt.Add(2*hooksrollout.StageSoak))
	require.NoError(t, err)
	require.Nil(t, advanced, "a paused rollout never advances")

	require.ErrorIs(t, stages.Pause(ctx, revision(t, stages), "again", operator), hooksrollout.ErrStageNotRunning)
	require.NoError(t, stages.Resume(ctx, revision(t, stages), operator))
	require.NoError(t, stages.Cancel(ctx, revision(t, stages), "bad release", operator))

	current, err = stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageCancelled, current.State)
	require.False(t, current.Active())
	require.ErrorIs(t, stages.Cancel(ctx, revision(t, stages), "again", operator), hooksrollout.ErrNoActiveStage)
}

func TestStageStore_RejectsStaleRevision(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.NoError(t, stages.Start(ctx, 0, 42, operator))
	shown := revision(t, stages)
	require.ErrorIs(t, stages.Start(ctx, 0, 43, operator), hooksrollout.ErrStageChanged, "starting over a rollout the operator wasn't shown")
	require.NoError(t, stages.Start(ctx, shown, 43, operator), "another operator replaces the rollout")

	require.ErrorIs(t, stages.Cancel(ctx, shown, "cancel 42", operator), hooksrollout.ErrStageChanged)
	require.ErrorIs(t, stages.Pause(ctx, shown, "pause 42", operator), hooksrollout.ErrStageChanged)
	require.ErrorIs(t, stages.Resume(ctx, shown, operator), hooksrollout.ErrStageChanged)

	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, 43, current.Version)
	require.Equal(t, hooksrollout.StageRunning, current.State)
}

func TestStageStore_CompletionKeepsNewerDefaultPin(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	pins := hooksrollout.NewStore(conn)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.NoError(t, stages.Start(ctx, 0, 20, operator))
	require.NoError(t, pins.SetDefault(ctx, 30, operator))

	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageCancelled, current.State, "moving the default past the rollout supersedes it")

	for range 3 {
		_, err := stages.Advance(ctx, time.Now().Add(72*time.Hour))
		require.NoError(t, err)
	}

	defaultPin, err := pins.DefaultPin(ctx)
	require.NoError(t, err)
	require.Equal(t, 30, defaultPin.Version)
}

func TestStageStore_DefaultBelowRolloutKeepsItRunning(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	pins := hooksrollout.NewStore(conn)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.NoError(t, stages.Start(ctx, 0, 30, operator))
	require.NoError(t, pins.SetDefault(ctx, 20, operator))

	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageRunning, current.State)
	require.Equal(t, 30, current.Version)
}

func revision(t *testing.T, stages *hooksrollout.StageStore) int64 {
	t.Helper()
	current, err := stages.Current(t.Context())
	require.NoError(t, err)
	return current.CurrentRevision()
}

func TestStore_OrganizationPinsIncludesStage(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	require.NoError(t, hooksrollout.NewStageStore(conn).Start(t.Context(), 0, 42, operator))

	pins, err := hooksrollout.NewStore(conn).OrganizationPins(t.Context(), createOrganization(t, conn))
	require.NoError(t, err)
	require.NotNil(t, pins.Stage)
	require.Equal(t, 42, pins.Stage.Version)
}

func bucketedOrganizations(t *testing.T, percent int) (inside, outside string) {
	t.Helper()
	for i := 0; inside == "" || outside == ""; i++ {
		orgID := fmt.Sprintf("org-%d", i)
		if hooksrollout.InStage(orgID, percent) {
			inside = orgID
		} else {
			outside = orgID
		}
	}
	return inside, outside
}
