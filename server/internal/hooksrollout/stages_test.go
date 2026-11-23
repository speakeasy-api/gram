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
	defaultPin := &hooksrollout.Pin{Version: 40}
	stage := &hooksrollout.Stage{Version: 42, Percent: 25, State: hooksrollout.StageRunning}
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
	pins.Default = &hooksrollout.Pin{Version: 42}
	_, source = pins.Effective(inside)
	require.Equal(t, hooksrollout.SourceDefault, source, "a stage no newer than the default pin has no effect")

	pins.Override = &hooksrollout.Pin{Version: 39}
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
	require.ErrorIs(t, stages.Start(ctx, 40, operator), hooksrollout.ErrStageVersionBehind)
	require.NoError(t, stages.Start(ctx, 42, operator))

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

func TestStageStore_PauseResumeCancel(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	stages := hooksrollout.NewStageStore(conn)
	ctx := t.Context()

	require.ErrorIs(t, stages.Pause(ctx, "alert", operator), hooksrollout.ErrNoActiveStage)
	require.NoError(t, stages.Start(ctx, 42, operator))
	require.ErrorIs(t, stages.Resume(ctx, operator), hooksrollout.ErrStageNotPaused)

	require.NoError(t, stages.Pause(ctx, "delivery errors climbing", operator))
	current, err := stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StagePaused, current.State)
	require.Equal(t, "delivery errors climbing", current.Reason)

	advanced, err := stages.Advance(ctx, current.SetAt.Add(2*hooksrollout.StageSoak))
	require.NoError(t, err)
	require.Nil(t, advanced, "a paused rollout never advances")

	require.ErrorIs(t, stages.Pause(ctx, "again", operator), hooksrollout.ErrStageNotRunning)
	require.NoError(t, stages.Resume(ctx, operator))
	require.NoError(t, stages.Cancel(ctx, "bad release", operator))

	current, err = stages.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, hooksrollout.StageCancelled, current.State)
	require.False(t, current.Active())
	require.ErrorIs(t, stages.Cancel(ctx, "again", operator), hooksrollout.ErrNoActiveStage)
}

func TestStore_OrganizationPinsIncludesStage(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	require.NoError(t, hooksrollout.NewStageStore(conn).Start(t.Context(), 42, operator))

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
