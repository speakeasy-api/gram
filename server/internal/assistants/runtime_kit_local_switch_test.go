package assistants

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type kitLocalMissingImageEngine struct{ *fakeKitLocalEngine }

func (e *kitLocalMissingImageEngine) ImageID(context.Context, string) (string, error) {
	return "", errLocalImageNotFound
}

func TestKitLocalSwitchEmptyRequiresImageReadiness(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	result, err := k.PrepareRuntimeSwitch(t.Context(), a, nil, 3)
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.True(t, result.DestinationReady)
	require.NotNil(t, result.Release)
	result.Release()
	require.Empty(t, engine.calls)

	_, err = k.PrepareRuntimeSwitch(t.Context(), a, nil, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)

	k.engine = &kitLocalMissingImageEngine{engine}
	require.ErrorIs(t, k.RuntimeReadyForAdmission(t.Context()), errRuntimeSwitchUnavailable)
	result, err = k.PrepareRuntimeSwitch(t.Context(), a, nil, 3)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	require.False(t, result.Settled)
	require.Empty(t, engine.calls)
}

func TestKitLocalSwitchSettlesRunningThreadContainers(t *testing.T) {
	t.Parallel()
	k, _, r, p := kitLocalFixture(t)
	r.State = runtimeStateActive
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON

	threadA, threadB := uuid.New(), uuid.New()
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadA}))
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadB}))

	result, err := k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.ElementsMatch(t, []uuid.UUID{threadA, threadB}, p.closedThreads)

	// Refuses switch when a running thread session fails to settle.
	p.closeErr = errors.New("session busy")
	result, err = k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchBusy)
	require.False(t, result.Settled)
}

func TestKitLocalSwitchRejectsForeignLabelsAndStartingState(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON
	threadID := uuid.New()
	require.NoError(t, k.RunTurn(t.Context(), r, runTurnRequest{ThreadID: threadID}))

	r.State = runtimeStateStarting
	_, err = k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchBusy)

	r.State = runtimeStateStopped
	name, _ := kitLocalThreadNames(r, threadID)
	engine.containers[name].Labels["gram.ai/assistant-id"] = uuid.NewString()
	_, err = k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
}
